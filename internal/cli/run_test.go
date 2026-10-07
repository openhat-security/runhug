package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adamsiwiec1/runhug/internal/store"
)

func TestNormalizeStreamPiece(t *testing.T) {
	if got := normalizeStreamPiece("a\rb\rc"); got != "abc" {
		t.Fatalf("%q", got)
	}
}

func TestReadSSEChatFirstTokenClearsThinking(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":""}}]}`,
		`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
		`data: {"choices":[{"delta":{"content":"!\r world"}}]}`,
		`data: [DONE]`,
		"",
	}, "\n")
	var buf bytes.Buffer
	// Pretend thinking was shown on the same writer.
	buf.WriteString("thinking…")
	got, _, err := readSSEChat(strings.NewReader(body), &buf)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Hello! world" {
		t.Fatalf("reply %q", got)
	}
	out := buf.String()
	if !strings.Contains(out, "assistant>") || !strings.Contains(out, "Hello! world") {
		t.Fatalf("output:\n%s", out)
	}
	// Carriage return clear should have been emitted before assistant>
	if !strings.Contains(out, "\r\033[K") {
		t.Fatalf("expected clear sequence in %q", out)
	}
}

func TestReadSSEChatEmpty(t *testing.T) {
	body := "data: [DONE]\n"
	var buf bytes.Buffer
	got, _, err := readSSEChat(strings.NewReader(body), &buf)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("%q", got)
	}
	if !strings.Contains(buf.String(), "(empty)") {
		t.Fatalf("%s", buf.String())
	}
}

func TestReadSSEChatStreamsReasoning(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"reasoning_content":"Let me "}}]}`,
		`data: {"choices":[{"delta":{"reasoning":"think."}}]}`,
		`data: {"choices":[{"delta":{"content":"Done."}}]}`,
		`data: [DONE]`,
		"",
	}, "\n")
	var buf bytes.Buffer
	buf.WriteString("thinking…")
	got, _, err := readSSEChat(strings.NewReader(body), &buf)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Done." {
		t.Fatalf("reply %q", got)
	}
	out := buf.String()
	if !strings.Contains(out, "thinking>") || !strings.Contains(out, "Let me think.") {
		t.Fatalf("expected streamed thinking:\n%s", out)
	}
	if !strings.Contains(out, "assistant>") || !strings.Contains(out, "Done.") {
		t.Fatalf("expected answer:\n%s", out)
	}
}

func TestReadSSEChatThinkTags(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"<thi"}}]}`,
		`data: {"choices":[{"delta":{"content":"nk>plan</th"}}]}`,
		`data: {"choices":[{"delta":{"content":"ink>ok"}}]}`,
		`data: [DONE]`,
		"",
	}, "\n")
	var buf bytes.Buffer
	got, _, err := readSSEChat(strings.NewReader(body), &buf)
	if err != nil {
		t.Fatal(err)
	}
	if got != "ok" {
		t.Fatalf("reply %q", got)
	}
	if !strings.Contains(buf.String(), "plan") {
		t.Fatalf("%s", buf.String())
	}
}

func TestReadSSEAgentToolCalls(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"reasoning_content":"need list"}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"list_dir","arguments":"{\"p"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ath\":\".\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
		"",
	}, "\n")
	var buf bytes.Buffer
	msg, finish, _, err := readSSEAgent(strings.NewReader(body), &buf)
	if err != nil {
		t.Fatal(err)
	}
	if finish != "tool_calls" || len(msg.ToolCalls) != 1 {
		t.Fatalf("%q %+v", finish, msg.ToolCalls)
	}
	if msg.ToolCalls[0].Function.Name != "list_dir" || !strings.Contains(msg.ToolCalls[0].Function.Arguments, "path") {
		t.Fatalf("%+v", msg.ToolCalls[0])
	}
	if !strings.Contains(buf.String(), "need list") {
		t.Fatalf("%s", buf.String())
	}
	if strings.Contains(buf.String(), "(empty)") {
		t.Fatalf("tool round should not print empty assistant: %s", buf.String())
	}
}

func TestAgentMsgJSONAlwaysHasContent(t *testing.T) {
	raw, err := json.Marshal(agentMsg{Role: "tool", ToolCallID: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"content"`) {
		t.Fatalf("tool msg missing content: %s", raw)
	}
	raw, err = json.Marshal(agentMsg{
		Role: "assistant",
		ToolCalls: []agentToolCall{{
			ID: "c1", Type: "function",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"content"`) {
		t.Fatalf("assistant tool-call msg missing content: %s", raw)
	}
}

func TestLooksLikeToolErrors(t *testing.T) {
	jsonErr := fmt.Errorf("HTTP 500 from http://127.0.0.1:8080/v1: Failed to parse tool call arguments as JSON: missing closing quote")
	if !looksLikeToolJSONError(jsonErr) {
		t.Fatal("expected json tool error")
	}
	if looksLikeToolsUnsupported(jsonErr) {
		t.Fatal("json parse 500 is not tools-unsupported")
	}
	if !looksLikeToolsUnsupported(fmt.Errorf("unknown field \"tools\"")) {
		t.Fatal("expected unsupported")
	}
	cut := agentToolCall{}
	cut.Function.Name = "write_file"
	cut.Function.Arguments = `{"path":"/tmp/x`
	if !toolArgsTruncated(agentMsg{ToolCalls: []agentToolCall{cut}}) {
		t.Fatal("expected truncated")
	}
	ok := agentToolCall{}
	ok.Function.Name = "write_file"
	ok.Function.Arguments = `{"path":"a.go","content":"hi"}`
	if toolArgsTruncated(agentMsg{ToolCalls: []agentToolCall{ok}}) {
		t.Fatal("complete args")
	}
}

func TestLooksLikeEndpointDownAndPopUser(t *testing.T) {
	if !looksLikeEndpointDown(fmt.Errorf(`Post "http://127.0.0.1:18080/v1/chat/completions": dial tcp 127.0.0.1:18080: connect: connection refused`)) {
		t.Fatal("refused")
	}
	if looksLikeEndpointDown(fmt.Errorf("HTTP 400 bad request")) {
		t.Fatal("400 is not down")
	}
	msgs := []agentMsg{
		{Role: "user", Content: "hi"},
		{Role: "user", Content: "ttest\n\n[runhug] extra"},
	}
	popLastUserTurn(&msgs, "ttest")
	if len(msgs) != 1 || msgs[0].Content != "hi" {
		t.Fatalf("%+v", msgs)
	}
}

func TestParseRunSlash(t *testing.T) {
	cmd, args, ok := parseRunSlash("/logs 120")
	if !ok || cmd != "/logs" || len(args) != 1 || args[0] != "120" {
		t.Fatalf("%q %v %v", cmd, args, ok)
	}
	if _, _, ok := parseRunSlash("hello"); ok {
		t.Fatal("expected false")
	}
}

func TestHandleRunSlashHelpClearModel(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("RUNHUG_CONFIG", "")
	t.Setenv("RVP_CONFIG", "")
	var out bytes.Buffer
	s := &runSession{
		Target: EndpointTarget{Model: "m1", BaseURL: "http://127.0.0.1:9/v1", Source: "local"},
		Model:  store.Model{HFRepo: "m1", Backend: store.BackendLocal, BaseURL: "http://127.0.0.1:9/v1", Runtime: "ollama"},
		Cwd:    t.TempDir(),
		Tools:  true,
		Out:    &out,
		Err:    &out,
	}
	cleared := false
	clear := func() { cleared = true }
	handled, exit, err := handleRunSlash(s, "/help", clear)
	if err != nil || !handled || exit {
		t.Fatalf("%v %v %v", handled, exit, err)
	}
	if !strings.Contains(out.String(), "/compact") || !strings.Contains(out.String(), "/context") || !strings.Contains(out.String(), "/gpu") {
		t.Fatalf("%s", out.String())
	}
	out.Reset()
	handled, exit, err = handleRunSlash(s, "/clear", clear)
	if err != nil || !handled || exit || !cleared {
		t.Fatalf("clear: %v %v %v cleared=%v", handled, exit, err, cleared)
	}
	out.Reset()
	handled, exit, err = handleRunSlash(s, "/model other", clear)
	if err != nil || !handled || exit || s.Target.Model != "other" {
		t.Fatalf("model: %v %v %v %q", handled, exit, err, s.Target.Model)
	}
	out.Reset()
	handled, exit, err = handleRunSlash(s, "/tools off", clear)
	if err != nil || !handled || exit || s.Tools {
		t.Fatalf("tools: %v %v %v tools=%v", handled, exit, err, s.Tools)
	}
	out.Reset()
	handled, exit, err = handleRunSlash(s, "/full", clear)
	if err != nil || !handled || exit || !s.ChatFull {
		t.Fatalf("full: %v %v %v full=%v", handled, exit, err, s.ChatFull)
	}
	handled, exit, err = handleRunSlash(s, "/mini", clear)
	if err != nil || !handled || exit || s.ChatFull {
		t.Fatalf("mini: %v %v %v full=%v", handled, exit, err, s.ChatFull)
	}
	handled, exit, err = handleRunSlash(s, "/plan", clear)
	if err != nil || !handled || exit || !s.Plan {
		t.Fatalf("plan: %v %v %v plan=%v", handled, exit, err, s.Plan)
	}
	handled, exit, err = handleRunSlash(s, "/agent", clear)
	if err != nil || !handled || exit || s.Plan {
		t.Fatalf("agent: %v %v %v plan=%v", handled, exit, err, s.Plan)
	}
	handled, exit, err = handleRunSlash(s, "/perm allow", clear)
	if err != nil || !handled || exit || s.Perm != permAllow {
		t.Fatalf("perm: %v %v %v %q", handled, exit, err, s.Perm)
	}
	handled, exit, err = handleRunSlash(s, "/exit", clear)
	if err != nil || !handled || !exit {
		t.Fatalf("exit: %v %v %v", handled, exit, err)
	}
}

func TestUndoRedoExportAtMention(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &runSession{
		Cwd:    dir,
		Target: EndpointTarget{Model: "m"},
		Agent: []agentMsg{
			{Role: "user", Content: "one"},
			{Role: "assistant", Content: "ok one"},
			{Role: "user", Content: "two"},
			{Role: "assistant", Content: "ok two"},
		},
		Out: io.Discard, Err: io.Discard,
	}
	s.pushUndo()
	s.Agent = append(s.Agent, agentMsg{Role: "user", Content: "three"}, agentMsg{Role: "assistant", Content: "ok three"})
	if err := undoRunTurn(s); err != nil {
		t.Fatal(err)
	}
	if last := s.Agent[len(s.Agent)-1].Content; last != "ok two" {
		t.Fatalf("undo got %+v", s.Agent)
	}
	if err := redoRunTurn(s); err != nil {
		t.Fatal(err)
	}
	if last := s.Agent[len(s.Agent)-1].Content; last != "ok three" {
		t.Fatalf("redo got %+v", s.Agent)
	}
	path, err := exportRunSession(s, "out.md")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), "## user") || !strings.Contains(string(raw), "three") {
		t.Fatalf("%s %v", raw, err)
	}
	got, ok := completeInputLine(dir, "see @hel")
	if !ok || got != "see @hello.go" {
		t.Fatalf("at complete %q %v", got, ok)
	}
	if mentions := pathMentions("look at @internal/cli/run.go"); len(mentions) == 0 || mentions[len(mentions)-1] != "internal/cli/run.go" {
		t.Fatalf("%v", mentions)
	}
	dump := serializeForCompact(s)
	if !strings.Contains(dump, "[user]") || !strings.Contains(dump, "three") {
		t.Fatalf("%s", dump)
	}
}

func TestPrintRunStatusStrip(t *testing.T) {
	var buf bytes.Buffer
	printRunStatusStrip(&buf, &runSession{
		Target: EndpointTarget{BaseURL: "http://127.0.0.1:8080/v1", Source: "gcp x"},
		Model: store.Model{
			HFRepo: "org/m", Backend: store.BackendGCP, PodID: "runhug-org-m",
			EndpointID: "proj", EndpointType: "us-central1-a",
		},
	})
	s := buf.String()
	for _, want := range []string{"status", "gcp", "runhug-org-m", ":8080", "tools", "agent", "perm", "Ctrl-C", "ctx"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in %s", want, s)
		}
	}
}

func TestDecideToolPermAndPlanTools(t *testing.T) {
	s := &runSession{Perm: permAllow}
	if err := s.decideToolPerm("write_file", `{"path":"a.go"}`); err != nil {
		t.Fatal(err)
	}
	if err := s.decideToolPerm("read_file", `{"path":"a.go"}`); err != nil {
		t.Fatal(err)
	}
	s.Perm = permDeny
	if err := s.decideToolPerm("bash", `{"command":"true"}`); err == nil {
		t.Fatal("expected deny")
	}
	s.Perm = permAllow
	s.Plan = true
	if err := s.decideToolPerm("edit_file", `{"path":"a.go","old_string":"a","new_string":"b"}`); err == nil {
		t.Fatal("expected plan block")
	}
	names := map[string]bool{}
	for _, tdef := range agentPlanToolDefs() {
		names[tdef.Function.Name] = true
	}
	if !names["list_dir"] || !names["read_file"] || !names["glob"] || names["bash"] || names["write_file"] {
		t.Fatalf("%v", names)
	}
}

func TestWithTurnCancelOnSIGINT(t *testing.T) {
	started := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- withTurnCancel(context.Background(), func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("turn never started")
	}
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		if !errors.Is(err, errTurnCanceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not cancel")
	}
}
