package bridge

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/adamsiwiec1/runhug/internal/hparams"
)

// --- Anthropic request shapes (subset) ---

type anthropicReq struct {
	Model         string          `json:"model"`
	Messages      []anthropicMsg  `json:"messages"`
	System        json.RawMessage `json:"system,omitempty"`
	MaxTokens     int             `json:"max_tokens"`
	Temperature   *float64        `json:"temperature,omitempty"`
	TopP          *float64        `json:"top_p,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	Stream        bool            `json:"stream,omitempty"`
	Tools         []anthropicTool `json:"tools,omitempty"`
	ToolChoice    json.RawMessage `json:"tool_choice,omitempty"`
	Metadata      *struct {
		UserID string `json:"user_id,omitempty"`
	} `json:"metadata,omitempty"`
}

type anthropicMsg struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// --- OpenAI request shapes ---

type openaiReq struct {
	Model       string         `json:"model"`
	Messages    []openaiMsg    `json:"messages"`
	MaxTokens   int            `json:"max_tokens,omitempty"`
	Temperature *float64       `json:"temperature,omitempty"`
	TopP        *float64       `json:"top_p,omitempty"`
	Stop        []string       `json:"stop,omitempty"`
	Stream      bool           `json:"stream,omitempty"`
	Tools       []openaiTool   `json:"tools,omitempty"`
	ToolChoice  any            `json:"tool_choice,omitempty"`
	User        string         `json:"user,omitempty"`
}

type openaiMsg struct {
	Role       string           `json:"role"`
	Content    any              `json:"content,omitempty"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
}

type openaiTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type openaiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// --- Anthropic / OpenAI response shapes ---

type anthropicResp struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	Role         string             `json:"role"`
	Model        string             `json:"model"`
	Content      []anthropicBlock   `json:"content"`
	StopReason   string             `json:"stop_reason"`
	StopSequence *string            `json:"stop_sequence"`
	Usage        anthropicUsage     `json:"usage"`
}

type anthropicBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type openaiResp struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role             string           `json:"role"`
			Content          *string          `json:"content"`
			Reasoning        string           `json:"reasoning"`
			ReasoningContent string           `json:"reasoning_content"`
			ToolCalls        []openaiToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// TranslateRequest converts an Anthropic /v1/messages body to OpenAI chat completions.
// defaultModel overrides an empty or client-default model id when set.
func TranslateRequest(body []byte, defaultModel string) ([]byte, anthropicReq, error) {
	var req anthropicReq
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, req, fmt.Errorf("decode anthropic request: %w", err)
	}
	out, err := anthropicToOpenAI(req, defaultModel)
	if err != nil {
		return nil, req, err
	}
	raw, err := json.Marshal(out)
	return raw, req, err
}

func anthropicToOpenAI(req anthropicReq, defaultModel string) (openaiReq, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" || isClaudeish(model) {
		if dm := strings.TrimSpace(defaultModel); dm != "" {
			model = dm
		}
	}
	out := openaiReq{
		Model:       model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stop:        req.StopSequences,
		Stream:      req.Stream,
	}
	if req.Metadata != nil {
		out.User = req.Metadata.UserID
	}

	toolNames := map[string]string{} // tool_use id → name (from prior assistant turns)

	if sys := systemText(req.System); sys != "" {
		out.Messages = append(out.Messages, openaiMsg{Role: "system", Content: sys})
	}

	for _, m := range req.Messages {
		msgs, err := convertMessage(m, toolNames)
		if err != nil {
			return out, err
		}
		out.Messages = append(out.Messages, msgs...)
	}

	for _, t := range req.Tools {
		ot := openaiTool{Type: "function"}
		ot.Function.Name = t.Name
		ot.Function.Description = t.Description
		schema := t.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		ot.Function.Parameters = schema
		out.Tools = append(out.Tools, ot)
	}
	if len(req.ToolChoice) > 0 && string(req.ToolChoice) != "null" {
		out.ToolChoice = mapToolChoice(req.ToolChoice)
	}
	return out, nil
}

// FillOpenAISampling sets temperature/top_p/max_tokens on an OpenAI chat body
// only when the client left them unset.
func FillOpenAISampling(body []byte, s *hparams.Sampling) []byte {
	if s == nil || s.Empty() || len(body) == 0 {
		return body
	}
	var req openaiReq
	if err := json.Unmarshal(body, &req); err != nil {
		return body
	}
	if req.Temperature == nil && s.Temperature != nil {
		req.Temperature = s.Temperature
	}
	if req.TopP == nil && s.TopP != nil {
		req.TopP = s.TopP
	}
	if req.MaxTokens == 0 && s.MaxTokens != nil {
		req.MaxTokens = *s.MaxTokens
	}
	out, err := json.Marshal(req)
	if err != nil {
		return body
	}
	return out
}

func isClaudeish(model string) bool {
	m := strings.ToLower(model)
	return strings.Contains(m, "claude") || strings.HasPrefix(m, "anthropic")
}

func systemText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var b strings.Builder
		for _, bl := range blocks {
			if t, _ := bl["type"].(string); t == "text" {
				if tx, _ := bl["text"].(string); tx != "" {
					if b.Len() > 0 {
						b.WriteByte('\n')
					}
					b.WriteString(tx)
				}
			}
		}
		return b.String()
	}
	return ""
}

func convertMessage(m anthropicMsg, toolNames map[string]string) ([]openaiMsg, error) {
	role := m.Role
	content := m.Content

	// Plain string content
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return []openaiMsg{{Role: role, Content: s}}, nil
	}

	var blocks []map[string]any
	if err := json.Unmarshal(content, &blocks); err != nil {
		return nil, fmt.Errorf("message content: %w", err)
	}

	if role == "assistant" {
		var textParts []string
		var toolCalls []openaiToolCall
		var parts []any // multimodal leftover (rare on assistant)
		for _, bl := range blocks {
			typ, _ := bl["type"].(string)
			switch typ {
			case "text":
				if tx, _ := bl["text"].(string); tx != "" {
					textParts = append(textParts, tx)
				}
			case "tool_use":
				id, _ := bl["id"].(string)
				name, _ := bl["name"].(string)
				if id != "" && name != "" {
					toolNames[id] = name
				}
				input := bl["input"]
				args, _ := json.Marshal(input)
				if args == nil {
					args = []byte("{}")
				}
				tc := openaiToolCall{ID: id, Type: "function"}
				tc.Function.Name = name
				tc.Function.Arguments = string(args)
				toolCalls = append(toolCalls, tc)
			case "thinking", "redacted_thinking":
				// drop
			default:
				parts = append(parts, bl)
			}
		}
		msg := openaiMsg{Role: "assistant"}
		if len(toolCalls) > 0 {
			msg.ToolCalls = toolCalls
			if len(textParts) > 0 {
				joined := strings.Join(textParts, "\n")
				msg.Content = joined
			} else {
				msg.Content = nil
			}
			return []openaiMsg{msg}, nil
		}
		if len(parts) == 0 {
			return []openaiMsg{{Role: "assistant", Content: strings.Join(textParts, "\n")}}, nil
		}
		_ = parts
		return []openaiMsg{{Role: "assistant", Content: strings.Join(textParts, "\n")}}, nil
	}

	// user (and anything else): may mix text, images, tool_result
	var out []openaiMsg
	var textParts []string
	var imageParts []any
	for _, bl := range blocks {
		typ, _ := bl["type"].(string)
		switch typ {
		case "text":
			if tx, _ := bl["text"].(string); tx != "" {
				textParts = append(textParts, tx)
			}
		case "image":
			if p := imageToOpenAIPart(bl); p != nil {
				imageParts = append(imageParts, p)
			}
		case "tool_result":
			// Flush pending text/images before tool results.
			if len(textParts) > 0 || len(imageParts) > 0 {
				out = append(out, userContentMsg(textParts, imageParts))
				textParts, imageParts = nil, nil
			}
			id, _ := bl["tool_use_id"].(string)
			name := toolNames[id]
			contentStr := toolResultContent(bl["content"])
			if isErr, _ := bl["is_error"].(bool); isErr {
				contentStr = "[tool_error] " + contentStr
			}
			om := openaiMsg{Role: "tool", ToolCallID: id, Content: contentStr}
			if name != "" {
				om.Name = name
			}
			out = append(out, om)
		}
	}
	if len(textParts) > 0 || len(imageParts) > 0 {
		out = append(out, userContentMsg(textParts, imageParts))
	}
	if len(out) == 0 {
		out = append(out, openaiMsg{Role: role, Content: ""})
	}
	return out, nil
}

func userContentMsg(textParts []string, imageParts []any) openaiMsg {
	if len(imageParts) == 0 {
		return openaiMsg{Role: "user", Content: strings.Join(textParts, "\n")}
	}
	parts := make([]any, 0, len(textParts)+len(imageParts))
	for _, t := range textParts {
		parts = append(parts, map[string]any{"type": "text", "text": t})
	}
	parts = append(parts, imageParts...)
	return openaiMsg{Role: "user", Content: parts}
}

func imageToOpenAIPart(bl map[string]any) any {
	src, _ := bl["source"].(map[string]any)
	if src == nil {
		return nil
	}
	typ, _ := src["type"].(string)
	switch typ {
	case "base64":
		media, _ := src["media_type"].(string)
		data, _ := src["data"].(string)
		if media == "" || data == "" {
			return nil
		}
		return map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"url": "data:" + media + ";base64," + data,
			},
		}
	case "url":
		u, _ := src["url"].(string)
		if u == "" {
			return nil
		}
		return map[string]any{
			"type": "image_url",
			"image_url": map[string]any{"url": u},
		}
	}
	return nil
}

func toolResultContent(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(b)
	}
}

func mapToolChoice(raw json.RawMessage) any {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	typ, _ := obj["type"].(string)
	switch typ {
	case "auto":
		return "auto"
	case "none":
		return "none"
	case "any":
		return "required"
	case "tool":
		name, _ := obj["name"].(string)
		return map[string]any{
			"type":     "function",
			"function": map[string]string{"name": name},
		}
	}
	return "auto"
}

// TranslateResponse converts an OpenAI chat completion to Anthropic messages format.
func TranslateResponse(body []byte, anthropicModel string) ([]byte, error) {
	var or openaiResp
	if err := json.Unmarshal(body, &or); err != nil {
		return nil, fmt.Errorf("decode openai response: %w", err)
	}
	if or.Error != nil && or.Error.Message != "" {
		return anthropicErrorJSON("api_error", or.Error.Message), nil
	}
	ar := openaiToAnthropic(or, anthropicModel)
	return json.Marshal(ar)
}

func openaiToAnthropic(or openaiResp, anthropicModel string) anthropicResp {
	id := or.ID
	if id == "" {
		id = "msg_" + randomID(8)
	} else if !strings.HasPrefix(id, "msg_") {
		id = "msg_" + id
	}
	model := anthropicModel
	if model == "" {
		model = or.Model
	}
	ar := anthropicResp{
		ID:           id,
		Type:         "message",
		Role:         "assistant",
		Model:        model,
		Content:      []anthropicBlock{},
		StopReason:   "end_turn",
		StopSequence: nil,
	}
	if or.Usage != nil {
		ar.Usage = anthropicUsage{
			InputTokens:  or.Usage.PromptTokens,
			OutputTokens: or.Usage.CompletionTokens,
		}
	}
	if len(or.Choices) == 0 {
		return ar
	}
	ch := or.Choices[0]
	ar.StopReason = mapFinishReason(ch.FinishReason)
	text := ""
	if ch.Message.Content != nil {
		text = *ch.Message.Content
	}
	if text == "" {
		text = ch.Message.Reasoning
	}
	if text == "" {
		text = ch.Message.ReasoningContent
	}
	if text != "" {
		ar.Content = append(ar.Content, anthropicBlock{Type: "text", Text: text})
	}
	for _, tc := range ch.Message.ToolCalls {
		input := json.RawMessage([]byte("{}"))
		if tc.Function.Arguments != "" {
			if json.Valid([]byte(tc.Function.Arguments)) {
				input = json.RawMessage(tc.Function.Arguments)
			} else {
				escaped, _ := json.Marshal(tc.Function.Arguments)
				input = json.RawMessage(`{"raw":` + string(escaped) + `}`)
			}
		}
		tid := tc.ID
		if tid == "" {
			tid = "toolu_" + randomID(10)
		}
		ar.Content = append(ar.Content, anthropicBlock{
			Type:  "tool_use",
			ID:    tid,
			Name:  tc.Function.Name,
			Input: input,
		})
		if ar.StopReason == "end_turn" {
			ar.StopReason = "tool_use"
		}
	}
	if len(ar.Content) == 0 {
		ar.Content = append(ar.Content, anthropicBlock{Type: "text", Text: ""})
	}
	return ar
}

func mapFinishReason(fr string) string {
	switch fr {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	case "content_filter":
		return "refusal"
	default:
		if fr == "" {
			return "end_turn"
		}
		return "end_turn"
	}
}

func randomID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func anthropicErrorJSON(typ, msg string) []byte {
	b, _ := json.Marshal(map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    typ,
			"message": msg,
		},
	})
	return b
}

// EstimateTokens is a crude chars/4 stub for count_tokens.
func EstimateTokens(req anthropicReq) int {
	n := len(systemText(req.System))
	for _, m := range req.Messages {
		n += len(m.Content)
	}
	for _, t := range req.Tools {
		n += len(t.Name) + len(t.Description) + len(t.InputSchema)
	}
	tok := n / 4
	if tok < 1 {
		tok = 1
	}
	return tok
}
