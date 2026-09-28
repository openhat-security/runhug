package bridge

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adamsiwiec1/runhug/internal/hparams"
)

func TestTranslateRequestTextAndSystem(t *testing.T) {
	in := `{
		"model": "claude-3-5-sonnet-20241022",
		"max_tokens": 128,
		"system": "You are helpful.",
		"messages": [{"role":"user","content":"Hello"}]
	}`
	out, req, err := TranslateRequest([]byte(in), "org/my-model")
	if err != nil {
		t.Fatal(err)
	}
	if req.MaxTokens != 128 {
		t.Fatalf("max_tokens %d", req.MaxTokens)
	}
	var o openaiReq
	if err := json.Unmarshal(out, &o); err != nil {
		t.Fatal(err)
	}
	if o.Model != "org/my-model" {
		t.Fatalf("model rewrite: %q", o.Model)
	}
	if len(o.Messages) != 2 {
		t.Fatalf("msgs %+v", o.Messages)
	}
	if o.Messages[0].Role != "system" || o.Messages[0].Content != "You are helpful." {
		t.Fatalf("system %+v", o.Messages[0])
	}
	if o.Messages[1].Role != "user" || o.Messages[1].Content != "Hello" {
		t.Fatalf("user %+v", o.Messages[1])
	}
}

func TestTranslateRequestToolsAndToolResult(t *testing.T) {
	in := `{
		"model": "m",
		"max_tokens": 64,
		"tools": [{
			"name": "get_weather",
			"description": "weather",
			"input_schema": {"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}
		}],
		"tool_choice": {"type":"auto"},
		"messages": [
			{"role":"user","content":"Weather in Paris?"},
			{"role":"assistant","content":[
				{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"city":"Paris"}}
			]},
			{"role":"user","content":[
				{"type":"tool_result","tool_use_id":"toolu_1","content":"{\"temp\":18}"}
			]}
		]
	}`
	out, _, err := TranslateRequest([]byte(in), "")
	if err != nil {
		t.Fatal(err)
	}
	var o openaiReq
	if err := json.Unmarshal(out, &o); err != nil {
		t.Fatal(err)
	}
	if len(o.Tools) != 1 || o.Tools[0].Function.Name != "get_weather" {
		t.Fatalf("tools %+v", o.Tools)
	}
	if o.ToolChoice != "auto" {
		t.Fatalf("tool_choice %#v", o.ToolChoice)
	}
	// user, assistant+tool_calls, tool
	if len(o.Messages) != 3 {
		t.Fatalf("want 3 msgs, got %d: %+v", len(o.Messages), o.Messages)
	}
	asst := o.Messages[1]
	if len(asst.ToolCalls) != 1 || asst.ToolCalls[0].Function.Name != "get_weather" {
		t.Fatalf("assistant tool_calls %+v", asst.ToolCalls)
	}
	if !strings.Contains(asst.ToolCalls[0].Function.Arguments, "Paris") {
		t.Fatalf("args %q", asst.ToolCalls[0].Function.Arguments)
	}
	tool := o.Messages[2]
	if tool.Role != "tool" || tool.ToolCallID != "toolu_1" {
		t.Fatalf("tool msg %+v", tool)
	}
}

func TestTranslateRequestToolChoiceAnyAndNamed(t *testing.T) {
	in := `{"model":"m","max_tokens":1,"tool_choice":{"type":"any"},"messages":[{"role":"user","content":"x"}]}`
	out, _, err := TranslateRequest([]byte(in), "")
	if err != nil {
		t.Fatal(err)
	}
	var o openaiReq
	_ = json.Unmarshal(out, &o)
	if o.ToolChoice != "required" {
		t.Fatalf("any → required, got %#v", o.ToolChoice)
	}

	in2 := `{"model":"m","max_tokens":1,"tool_choice":{"type":"tool","name":"foo"},"messages":[{"role":"user","content":"x"}]}`
	out2, _, err := TranslateRequest([]byte(in2), "")
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(out2, &o)
	m, ok := o.ToolChoice.(map[string]any)
	if !ok {
		t.Fatalf("%T", o.ToolChoice)
	}
	fn := m["function"].(map[string]any)
	if fn["name"] != "foo" {
		t.Fatalf("%v", m)
	}
}

func TestTranslateResponseTextGolden(t *testing.T) {
	in := `{
		"id": "chatcmpl-abc",
		"model": "upstream-m",
		"choices": [{
			"index": 0,
			"finish_reason": "stop",
			"message": {"role":"assistant","content":"Hello world"}
		}],
		"usage": {"prompt_tokens": 5, "completion_tokens": 2}
	}`
	out, err := TranslateResponse([]byte(in), "served-model")
	if err != nil {
		t.Fatal(err)
	}
	var ar anthropicResp
	if err := json.Unmarshal(out, &ar); err != nil {
		t.Fatal(err)
	}
	if ar.Type != "message" || ar.Role != "assistant" {
		t.Fatalf("%+v", ar)
	}
	if ar.Model != "served-model" {
		t.Fatalf("model %q", ar.Model)
	}
	if ar.StopReason != "end_turn" {
		t.Fatalf("stop %q", ar.StopReason)
	}
	if len(ar.Content) != 1 || ar.Content[0].Type != "text" || ar.Content[0].Text != "Hello world" {
		t.Fatalf("content %+v", ar.Content)
	}
	if ar.Usage.InputTokens != 5 || ar.Usage.OutputTokens != 2 {
		t.Fatalf("usage %+v", ar.Usage)
	}
	if !strings.HasPrefix(ar.ID, "msg_") {
		t.Fatalf("id %q", ar.ID)
	}
}

func TestTranslateResponseToolUseGolden(t *testing.T) {
	in := `{
		"id": "chatcmpl-t",
		"choices": [{
			"finish_reason": "tool_calls",
			"message": {
				"role": "assistant",
				"content": null,
				"tool_calls": [{
					"id": "call_1",
					"type": "function",
					"function": {"name": "get_weather", "arguments": "{\"city\":\"Paris\"}"}
				}]
			}
		}]
	}`
	out, err := TranslateResponse([]byte(in), "m")
	if err != nil {
		t.Fatal(err)
	}
	var ar anthropicResp
	if err := json.Unmarshal(out, &ar); err != nil {
		t.Fatal(err)
	}
	if ar.StopReason != "tool_use" {
		t.Fatalf("stop %q", ar.StopReason)
	}
	if len(ar.Content) != 1 || ar.Content[0].Type != "tool_use" {
		t.Fatalf("%+v", ar.Content)
	}
	if ar.Content[0].Name != "get_weather" || ar.Content[0].ID != "call_1" {
		t.Fatalf("%+v", ar.Content[0])
	}
	var input map[string]any
	if err := json.Unmarshal(ar.Content[0].Input, &input); err != nil {
		t.Fatal(err)
	}
	if input["city"] != "Paris" {
		t.Fatalf("%v", input)
	}
}

func TestEstimateTokens(t *testing.T) {
	n := EstimateTokens(anthropicReq{
		System:   json.RawMessage(`"hi"`),
		Messages: []anthropicMsg{{Role: "user", Content: json.RawMessage(`"hello world"`)}},
	})
	if n < 1 {
		t.Fatal(n)
	}
}

func TestFillOpenAISampling(t *testing.T) {
	temp := 0.4
	s := &hparams.Sampling{Temperature: &temp}
	in := []byte(`{"model":"m","messages":[],"max_tokens":64}`)
	out := FillOpenAISampling(in, s)
	var o openaiReq
	if err := json.Unmarshal(out, &o); err != nil {
		t.Fatal(err)
	}
	if o.Temperature == nil || *o.Temperature != 0.4 {
		t.Fatalf("temp %+v", o.Temperature)
	}
	if o.MaxTokens != 64 {
		t.Fatalf("must not clobber max_tokens %d", o.MaxTokens)
	}
	clientTemp := 0.9
	withTemp := []byte(`{"model":"m","temperature":0.9}`)
	out2 := FillOpenAISampling(withTemp, s)
	var o2 openaiReq
	if err := json.Unmarshal(out2, &o2); err != nil {
		t.Fatal(err)
	}
	if o2.Temperature == nil || *o2.Temperature != clientTemp {
		t.Fatalf("client temp must win, got %v", o2.Temperature)
	}
}
