package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	agentMaxRounds        = 12
	agentMaxToolsPerRound = 8
	agentSystemPrompt     = `You are runhug, a local coding assistant in the user's workspace.
Obey the latest user message. Do not continue earlier file work unless they asked to continue or finish that work.
You implement changes with tools. You do not write plans, blueprints, checklists, "next steps", or ask the user to paste file contents.
On coding requests: call list_dir/read_file first, then edit_file/write_file/append_file. Never invent paths or file contents.
read_file takes a FILE (pkg/foo.go). Do not read_file a directory. Do not list every package — list one folder, read one or two files, then edit.
Paths are relative to the workspace cwd — never start with a leading /. Stay under paths the user named.
If a file already exists, prefer edit_file (exact old_string → new_string) or append_file — write_file refuses overwrites.
Keep each write_file / append_file content under 1500 characters. For large files: write_file the first chunk, then append_file until done.
Tool arguments MUST be valid JSON (escape quotes, backslashes, and newlines). At most 8 tool calls per round.
After tools, summarize every file you wrote or edited and what changed. For list/ls requests, only list — do not write files.`

	agentPlanAddendum = `

You are in PLAN MODE. Use only list_dir, read_file, and glob. Do not write, edit, append, or run bash.
Read the code, then reply with a short plan. Tell the user to type /agent when they want you to implement it.`
)

type agentMsg struct {
	Role       string          `json:"role"`
	Content    string          `json:"content"`
	ToolCalls  []agentToolCall `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
}

// MarshalJSON always emits content. llama.cpp rejects non-assistant
// messages that omit the field (empty tool results used to drop it).
func (m agentMsg) MarshalJSON() ([]byte, error) {
	type wire struct {
		Role       string          `json:"role"`
		Content    string          `json:"content"`
		ToolCalls  []agentToolCall `json:"tool_calls,omitempty"`
		ToolCallID string          `json:"tool_call_id,omitempty"`
		Name       string          `json:"name,omitempty"`
	}
	w := wire{
		Role:       m.Role,
		Content:    m.Content,
		ToolCalls:  m.ToolCalls,
		ToolCallID: m.ToolCallID,
		Name:       m.Name,
	}
	if w.Role != "assistant" && w.Content == "" {
		w.Content = ""
	}
	return json.Marshal(w)
}

type agentToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type agentChatReq struct {
	Model         string          `json:"model"`
	Messages      []agentMsg      `json:"messages"`
	Tools         []openaiToolDef `json:"tools,omitempty"`
	ToolChoice    any             `json:"tool_choice,omitempty"`
	Stream        bool            `json:"stream"`
	MaxTokens     int             `json:"max_tokens,omitempty"`
	StreamOptions *streamOpts     `json:"stream_options,omitempty"`
}

type agentChatResp struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role      string          `json:"role"`
			Content   string          `json:"content"`
			ToolCalls []agentToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage json.RawMessage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func agentSystemMessage() agentMsg {
	return agentMsg{Role: "system", Content: agentSystemPrompt}
}

func agentSystemMessagePlan() agentMsg {
	return agentMsg{Role: "system", Content: agentSystemPrompt + agentPlanAddendum}
}

func agentPlanToolDefs() []openaiToolDef {
	var out []openaiToolDef
	for _, t := range agentToolDefs() {
		switch t.Function.Name {
		case "list_dir", "read_file", "glob":
			out = append(out, t)
		}
	}
	return out
}

// runAgentTurn sends user text through a tool-enabled completion loop.
func runAgentTurn(ctx context.Context, s *runSession, messages *[]agentMsg, userText string) error {
	w := s.out()
	ask := strings.TrimSpace(userText)
	if roots := pathMentions(ask); len(roots) > 0 && looksLikeCodingAsk(ask) {
		s.PathRoots = roots
		s.PathFocus = roots[len(roots)-1]
		userText = userText + "\n\n[runhug] Path focus: " + strings.Join(roots, ", ") +
			". Use tools immediately (list_dir/read_file, then edit_file/write_file). " +
			"No plans or checklists. If a focus path is a directory with subpackages, write inside those — not a new file in the focus root."
	} else if looksLikeCodingAsk(ask) {
		userText = userText + "\n\n[runhug] Use tools to inspect and edit the repo. Do not write plans or checklists."
	} else {
		s.PathRoots = nil
		s.PathFocus = ""
		userText = userText + "\n\n[runhug] New request. Do not resume earlier file edits. Answer only this message."
	}
	*messages = append(*messages, agentMsg{Role: "user", Content: userText})

	fmt.Fprint(w, dim("thinking…"))
	_ = syncWriter(w)

	if !s.Plan && looksLikeWriteContinue(ask) {
		if path := lastWritePath(*messages); path != "" {
			return dumpIncompleteWrite(ctx, s, messages, path)
		}
	}

	jsonDumpTried := false
	noToolsTried := false
	adviceNudges := 0
	var fileOps []fileOp
	for round := 0; round < agentMaxRounds; round++ {
		if err := ctx.Err(); err != nil {
			clearThinkingOn(w)
			return err
		}
		msg, finish, err := s.completeAgent(ctx, *messages, true, s.Plan, true, w)
		if err != nil && looksLikeToolJSONError(err) {
			if len(msg.ToolCalls) == 0 {
				msg2, finish2, err2 := s.completeAgent(ctx, *messages, true, s.Plan, false, w)
				if err2 == nil || len(msg2.ToolCalls) > 0 {
					msg, finish, err = msg2, finish2, err2
				} else {
					err = err2
				}
			}
		}
		if err != nil && looksLikeToolJSONError(err) && !jsonDumpTried && !s.Plan && looksLikeWriteContinue(ask) {
			if path := lastWritePath(*messages); path != "" {
				jsonDumpTried = true
				return dumpIncompleteWrite(ctx, s, messages, path)
			}
		}
		if err != nil && looksLikeToolJSONError(err) && !noToolsTried && !s.Plan {
			noToolsTried = true
			clearThinkingOn(w)
			fmt.Fprintln(w, dim("tool JSON invalid — answering without tools this turn"))
			fmt.Fprint(w, dim("thinking…"))
			_ = syncWriter(w)
			msg, finish, err = s.completeAgent(ctx, *messages, false, false, true, w)
		}
		if err != nil {
			if round == 0 && looksLikeToolsUnsupported(err) {
				clearThinkingOn(w)
				fmt.Fprintln(w, yellow("⚠")+"  "+dim("model/backend rejected tools — answering without tools this turn"))
				fmt.Fprint(w, dim("thinking…"))
				_ = syncWriter(w)
				msg, finish, err = s.completeAgent(ctx, *messages, false, false, true, w)
			}
			if err != nil {
				clearThinkingOn(w)
				if looksLikeToolJSONError(err) {
					return fmt.Errorf("this backend rejected the model's tool JSON. Try /tools off, or a smaller edit")
				}
				return err
			}
		}

		if len(msg.ToolCalls) > 0 {
			*messages = append(*messages, msg)
			listed := map[string]bool{}
			for i, tc := range msg.ToolCalls {
				name := tc.Function.Name
				args := tc.Function.Arguments
				printToolCall(w, name, args)
				var result string
				var err error
				if i >= agentMaxToolsPerRound {
					err = fmt.Errorf("skipped: max %d tools this round — read a file (*.go), do not list every package", agentMaxToolsPerRound)
					result = err.Error()
				} else if skip, ok := skipRepeatDirExplore(s.Cwd, name, args, listed); ok {
					result = skip
				} else if errF := enforcePathFocusAny(s.PathRoots, s.Cwd, name, args); errF != nil {
					err = errF
					result = errF.Error()
				} else if errP := s.decideToolPerm(name, args); errP != nil {
					err = errP
					result = errP.Error()
				} else {
					result, err = runAgentTool(s.Cwd, name, args)
				}
				if op, ok := fileOpFromTool(name, args, err); ok {
					fileOps = append(fileOps, op)
				}
				printToolResult(w, name, result, err)
				if err != nil {
					result = "error: " + err.Error()
				}
				if result == "" {
					result = "(empty)"
				}
				*messages = append(*messages, agentMsg{
					Role:       "tool",
					Content:    result,
					ToolCallID: tc.ID,
					Name:       name,
				})
			}
			fmt.Fprint(w, dim("thinking…"))
			_ = syncWriter(w)
			continue
		}

		if adviceNudges < 1 && !s.Plan && looksLikeCodingAsk(ask) && looksLikeAdviceOnly(msg.Content) {
			adviceNudges++
			clearThinkingOn(w)
			fmt.Fprintln(w, dim("skipping plan — requiring tools"))
			*messages = append(*messages, agentMsg{
				Role:    "user",
				Content: "Stop. Do not plan or write checklists. Call tools now: list_dir and read_file on the paths I named, then edit_file or write_file to implement.",
			})
			fmt.Fprint(w, dim("thinking…"))
			_ = syncWriter(w)
			continue
		}

		*messages = append(*messages, agentMsg{Role: "assistant", Content: msg.Content})
		_ = finish
		printTurnFileSummary(w, fileOps)
		printContextUsage(w, s)
		return nil
	}
	clearThinkingOn(w)
	return fmt.Errorf("tool loop exceeded %d rounds", agentMaxRounds)
}

func dumpIncompleteWrite(ctx context.Context, s *runSession, messages *[]agentMsg, path string) error {
	w := s.out()
	clearThinkingOn(w)
	fmt.Fprintln(w, dim("finishing "+path+" as text (this backend breaks on large tool JSON)"))
	dumpMsgs := buildFileDumpMessages(path, *messages)
	fmt.Fprint(w, dim("thinking…"))
	_ = syncWriter(w)
	dumpMsg, dumpFinish, dumpErr := s.completeAgent(ctx, dumpMsgs, false, false, true, w)
	if dumpErr != nil {
		clearThinkingOn(w)
		return dumpErr
	}
	if strings.TrimSpace(dumpMsg.Content) == "" {
		clearThinkingOn(w)
		return fmt.Errorf("model returned an empty file dump")
	}
	n, errW := applyDumpedFiles(s.Cwd, w, dumpMsg.Content, path)
	if errW != nil {
		clearThinkingOn(w)
		return errW
	}
	*messages = append(*messages, agentMsg{Role: "assistant", Content: dumpMsg.Content})
	_ = dumpFinish
	if n > 0 {
		printTurnFileSummary(w, []fileOp{{Op: "wrote", Path: path}})
	}
	return nil
}

func looksLikeCodingAsk(s string) bool {
	lower := strings.ToLower(userAskCore(s))
	for _, n := range []string{
		"implement", "integrate", "refactor", "create", "complete",
	} {
		if hasWord(lower, n) {
			return true
		}
	}
	for _, n := range []string{"finish", "fix", "edit", "write", "wire"} {
		if hasWord(lower, n) {
			return true
		}
	}
	if strings.Contains(lower, "add ") {
		return true
	}
	if strings.Contains(lower, ".go") || strings.Contains(lower, "/cmd") || strings.Contains(lower, "/internal") {
		return true
	}
	return pathMention(s) != ""
}

func looksLikeWriteContinue(s string) bool {
	lower := strings.ToLower(userAskCore(s))
	if hasWord(lower, "continue") || hasWord(lower, "finish") {
		return true
	}
	for _, n := range []string{"cut off", "truncated", "rest of the file", "complete the file", "finish it"} {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return false
}

func userAskCore(s string) string {
	if i := strings.Index(s, "[runhug]"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func hasWord(s, w string) bool {
	if s == "" || w == "" {
		return false
	}
	for {
		i := strings.Index(s, w)
		if i < 0 {
			return false
		}
		beforeOK := i == 0 || !isWordChar(s[i-1])
		after := i + len(w)
		afterOK := after >= len(s) || !isWordChar(s[after])
		if beforeOK && afterOK {
			return true
		}
		s = s[i+1:]
	}
}

func isWordChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

func looksLikeAdviceOnly(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	lower := strings.ToLower(s)
	for _, n := range []string{
		"next steps", "checklist", "i'd need to see", "please share",
		"integration blueprint", "to refine this further", "- [ ]",
		"i'm happy to help", "based on a typical",
	} {
		if strings.Contains(lower, n) {
			return true
		}
	}
	// Long markdown essay with fences and no sign of having read the tree.
	if len(s) > 600 && strings.Count(s, "```") >= 2 && strings.Count(s, "##") >= 2 {
		return true
	}
	return false
}

func looksLikeToolJSONError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "parse tool call") {
		return true
	}
	if strings.Contains(s, "tool call arguments") {
		return true
	}
	if strings.Contains(s, "tool") && strings.Contains(s, "json") && (strings.Contains(s, "parse") || strings.Contains(s, "missing closing quote") || strings.Contains(s, "unexpected end")) {
		return true
	}
	if strings.Contains(s, "unexpected end of json") {
		return true
	}
	return false
}

func toolArgsTruncated(msg agentMsg) bool {
	if len(msg.ToolCalls) == 0 {
		return false
	}
	for _, tc := range msg.ToolCalls {
		raw := strings.TrimSpace(tc.Function.Arguments)
		if raw == "" || !json.Valid([]byte(raw)) {
			return true
		}
	}
	return false
}

func looksLikeToolsUnsupported(err error) bool {
	if err == nil || looksLikeToolJSONError(err) {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, needle := range []string{
		"does not support tools",
		"tools are not supported",
		"unknown field \"tools\"",
		"unknown field 'tools'",
		"tool_choice",
	} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func agentMessagesForAPI(in []agentMsg) []agentMsg {
	out := make([]agentMsg, len(in))
	copy(out, in)
	var tools []int
	for i, m := range out {
		if m.Role == "tool" {
			tools = append(tools, i)
		}
	}
	const keepFull = 10
	const maxRunes = 2000
	if len(tools) <= keepFull {
		return out
	}
	for _, i := range tools[:len(tools)-keepFull] {
		rs := []rune(out[i].Content)
		if len(rs) > maxRunes {
			out[i].Content = string(rs[:maxRunes]) + "\n…"
		}
	}
	return out
}

func (s *runSession) completeAgent(ctx context.Context, messages []agentMsg, withTools, plan, stream bool, w io.Writer) (agentMsg, string, error) {
	msg, finish, u, err := agentComplete(ctx, s.Target, messages, withTools, plan, stream, w)
	if s != nil {
		s.noteUsage(u)
	}
	return msg, finish, err
}

func agentComplete(ctx context.Context, target EndpointTarget, messages []agentMsg, withTools, plan, stream bool, w io.Writer) (agentMsg, string, tokenUsage, error) {
	base := strings.TrimRight(target.BaseURL, "/")
	msgs := make([]agentMsg, 0, len(messages)+1)
	hasSys := false
	for _, m := range messages {
		if m.Role == "system" {
			hasSys = true
			break
		}
	}
	if !hasSys {
		if plan {
			msgs = append(msgs, agentSystemMessagePlan())
		} else {
			msgs = append(msgs, agentSystemMessage())
		}
	}
	for _, m := range agentMessagesForAPI(messages) {
		if m.Role != "assistant" && m.Content == "" {
			m.Content = ""
		}
		msgs = append(msgs, m)
	}

	reqBody := agentChatReq{
		Model:     target.Model,
		Messages:  msgs,
		Stream:    stream,
		MaxTokens: 8192,
	}
	if stream {
		reqBody.StreamOptions = &streamOpts{IncludeUsage: true}
	}
	if withTools {
		if plan {
			reqBody.Tools = agentPlanToolDefs()
		} else {
			reqBody.Tools = agentToolDefs()
		}
		reqBody.ToolChoice = "auto"
	}
	if s := target.Sampling; s != nil {
		if s.MaxTokens != nil && *s.MaxTokens > 0 {
			reqBody.MaxTokens = *s.MaxTokens
			if reqBody.MaxTokens < 4096 {
				reqBody.MaxTokens = 4096
			}
		}
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return agentMsg{}, "", tokenUsage{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return agentMsg{}, "", tokenUsage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if target.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+target.APIKey)
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	res, err := client.Do(req)
	if err != nil {
		return agentMsg{}, "", tokenUsage{}, fmt.Errorf("chat request failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
		msg := strings.TrimSpace(string(body))
		if len(msg) > 400 {
			msg = msg[:400] + "…"
		}
		return agentMsg{}, "", tokenUsage{}, fmt.Errorf("HTTP %d from %s: %s", res.StatusCode, base, msg)
	}
	if w == nil {
		w = io.Discard
	}
	ct := res.Header.Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") {
		return readSSEAgent(res.Body, w)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return agentMsg{}, "", tokenUsage{}, err
	}
	var parsed agentChatResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return agentMsg{}, "", tokenUsage{}, fmt.Errorf("decode chat response: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return agentMsg{}, "", tokenUsage{}, fmt.Errorf("upstream: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return agentMsg{}, "", tokenUsage{}, fmt.Errorf("empty completion")
	}
	ch := parsed.Choices[0]
	rs := &replyStream{w: w, skipEmpty: len(ch.Message.ToolCalls) > 0}
	rs.addContent(ch.Message.Content)
	rs.finish()
	out := agentMsg{
		Role:      "assistant",
		Content:   rs.answerText(),
		ToolCalls: ch.Message.ToolCalls,
	}
	for i := range out.ToolCalls {
		if out.ToolCalls[i].Type == "" {
			out.ToolCalls[i].Type = "function"
		}
		if out.ToolCalls[i].ID == "" {
			out.ToolCalls[i].ID = fmt.Sprintf("call_%d", i+1)
		}
	}
	return out, ch.FinishReason, parseTokenUsage(parsed.Usage), nil
}

func syncWriter(w io.Writer) error {
	type syncer interface{ Sync() error }
	if s, ok := w.(syncer); ok {
		return s.Sync()
	}
	return nil
}
