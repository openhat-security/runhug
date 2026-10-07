package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type replyStream struct {
	w         io.Writer
	inThink   bool
	tagHold   string
	thinkOn   bool
	answerOn  bool
	content   strings.Builder
	reasoning strings.Builder
	skipEmpty bool
}

func (s *replyStream) addReasoning(piece string) {
	piece = normalizeStreamPiece(piece)
	if piece == "" {
		return
	}
	s.reasoning.WriteString(piece)
	s.ensureThink()
	fmt.Fprint(s.w, dim(piece))
	_ = syncWriter(s.w)
}

func (s *replyStream) ensureThink() {
	if s.thinkOn {
		return
	}
	clearThinkingOn(s.w)
	fmt.Fprint(s.w, dim("thinking> "))
	s.thinkOn = true
	_ = syncWriter(s.w)
}

func (s *replyStream) ensureAnswer() {
	if s.answerOn {
		return
	}
	if s.thinkOn {
		fmt.Fprintln(s.w)
	} else {
		clearThinkingOn(s.w)
	}
	fmt.Fprint(s.w, cyan("assistant> "))
	s.answerOn = true
	_ = syncWriter(s.w)
}

func (s *replyStream) addAnswer(piece string) {
	piece = normalizeStreamPiece(piece)
	if piece == "" {
		return
	}
	s.content.WriteString(piece)
	s.ensureAnswer()
	fmt.Fprint(s.w, piece)
	_ = syncWriter(s.w)
}

func (s *replyStream) addContent(piece string) {
	piece = normalizeStreamPiece(piece)
	if piece == "" && s.tagHold == "" {
		return
	}
	s.tagHold += piece
	for {
		if s.inThink {
			i := strings.Index(s.tagHold, "</think>")
			if i < 0 {
				keep := holdTagPrefix(s.tagHold, "</think>")
				emit := s.tagHold[:len(s.tagHold)-len(keep)]
				s.tagHold = keep
				s.addReasoning(emit)
				return
			}
			s.addReasoning(s.tagHold[:i])
			s.tagHold = s.tagHold[i+len("</think>"):]
			s.inThink = false
			continue
		}
		i := strings.Index(s.tagHold, "<think>")
		if i < 0 {
			keep := holdTagPrefix(s.tagHold, "<think>")
			emit := s.tagHold[:len(s.tagHold)-len(keep)]
			s.tagHold = keep
			s.addAnswer(emit)
			return
		}
		s.addAnswer(s.tagHold[:i])
		s.tagHold = s.tagHold[i+len("<think>"):]
		s.inThink = true
	}
}

func (s *replyStream) closeTags() {
	if s.tagHold == "" {
		return
	}
	if s.inThink {
		s.addReasoning(s.tagHold)
	} else {
		s.addAnswer(s.tagHold)
	}
	s.tagHold = ""
}

func (s *replyStream) finish() {
	s.closeTags()
	if !s.thinkOn && !s.answerOn {
		if s.skipEmpty {
			clearThinkingOn(s.w)
			return
		}
		clearThinkingOn(s.w)
		fmt.Fprint(s.w, cyan("assistant> "))
		fmt.Fprint(s.w, dim("(empty)"))
		s.answerOn = true
	}
	if s.thinkOn || s.answerOn {
		fmt.Fprintln(s.w)
	}
}

func (s *replyStream) answerText() string {
	s.closeTags()
	out := strings.TrimSpace(s.content.String())
	if out == "" {
		out = strings.TrimSpace(s.reasoning.String())
	}
	return out
}

func holdTagPrefix(s, tag string) string {
	max := len(tag) - 1
	if max > len(s) {
		max = len(s)
	}
	for n := max; n > 0; n-- {
		if strings.HasSuffix(s, tag[:n]) {
			return s[len(s)-n:]
		}
	}
	return ""
}

func jsonStringish(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj struct {
		Content string `json:"content"`
		Text    string `json:"text"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		if obj.Content != "" {
			return obj.Content
		}
		return obj.Text
	}
	return ""
}

type sseChoiceDelta struct {
	Content          json.RawMessage `json:"content"`
	Reasoning        json.RawMessage `json:"reasoning"`
	ReasoningContent json.RawMessage `json:"reasoning_content"`
	ToolCalls        []sseToolDelta  `json:"tool_calls"`
}

type sseToolDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func applyReasoningDelta(rs *replyStream, d sseChoiceDelta) {
	if v := jsonStringish(d.ReasoningContent); v != "" {
		rs.addReasoning(v)
	}
	if v := jsonStringish(d.Reasoning); v != "" {
		rs.addReasoning(v)
	}
	if v := jsonStringish(d.Content); v != "" {
		rs.addContent(v)
	}
}

func readSSEChat(r io.Reader, w io.Writer) (string, tokenUsage, error) {
	if w == nil {
		w = io.Discard
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 2<<20)
	rs := &replyStream{w: w}
	var usage tokenUsage
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta sseChoiceDelta `json:"delta"`
			} `json:"choices"`
			Usage json.RawMessage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if u := parseTokenUsage(chunk.Usage); u.ok() {
			usage = u
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		applyReasoningDelta(rs, chunk.Choices[0].Delta)
	}
	rs.finish()
	if err := sc.Err(); err != nil {
		return rs.answerText(), usage, err
	}
	return rs.answerText(), usage, nil
}

func readSSEAgent(r io.Reader, w io.Writer) (agentMsg, string, tokenUsage, error) {
	if w == nil {
		w = io.Discard
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 2<<20)
	rs := &replyStream{w: w}
	calls := map[int]*agentToolCall{}
	finish := ""
	var usage tokenUsage
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta        sseChoiceDelta `json:"delta"`
				FinishReason string         `json:"finish_reason"`
			} `json:"choices"`
			Usage json.RawMessage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if u := parseTokenUsage(chunk.Usage); u.ok() {
			usage = u
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		ch := chunk.Choices[0]
		if ch.FinishReason != "" {
			finish = ch.FinishReason
		}
		applyReasoningDelta(rs, ch.Delta)
		for _, tc := range ch.Delta.ToolCalls {
			acc, ok := calls[tc.Index]
			if !ok {
				acc = &agentToolCall{Type: "function"}
				calls[tc.Index] = acc
			}
			if tc.ID != "" {
				acc.ID = tc.ID
			}
			if tc.Type != "" {
				acc.Type = tc.Type
			}
			if tc.Function.Name != "" {
				acc.Function.Name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				acc.Function.Arguments += tc.Function.Arguments
			}
		}
	}
	out := agentMsg{Role: "assistant", Content: rs.answerText()}
	if len(calls) > 0 {
		rs.skipEmpty = true
		max := 0
		for i := range calls {
			if i > max {
				max = i
			}
		}
		out.ToolCalls = make([]agentToolCall, 0, len(calls))
		for i := 0; i <= max; i++ {
			acc := calls[i]
			if acc == nil {
				continue
			}
			if acc.Type == "" {
				acc.Type = "function"
			}
			if acc.ID == "" {
				acc.ID = fmt.Sprintf("call_%d", i+1)
			}
			out.ToolCalls = append(out.ToolCalls, *acc)
		}
	}
	rs.finish()
	if err := sc.Err(); err != nil {
		return out, finish, usage, err
	}
	return out, finish, usage, nil
}
