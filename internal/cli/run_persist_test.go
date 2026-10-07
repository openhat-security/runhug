package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveLoadSession(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("RUNHUG_CONFIG", "")
	t.Setenv("RVP_CONFIG", "")
	s := &runSession{
		PersistID: "20261003-test-aaaa",
		Cwd:       t.TempDir(),
		Tools:     true,
		Target:    EndpointTarget{Model: "m", BaseURL: "http://127.0.0.1/v1"},
		Agent:     []agentMsg{{Role: "user", Content: "hello world"}},
	}
	if err := saveRunPersist(s); err != nil {
		t.Fatal(err)
	}
	p, err := loadPersistedChat(s.PersistID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "hello world" || len(p.Agent) != 1 || p.Agent[0].Content != "hello world" {
		t.Fatalf("%+v", p)
	}
	got := latestPersistedChat(s.Cwd)
	if got == nil || got.ID != s.PersistID {
		t.Fatalf("%+v", got)
	}
	s2 := &runSession{Target: EndpointTarget{Model: "other"}}
	applyPersistedChat(s2, p)
	if s2.PersistID != s.PersistID || len(s2.Agent) != 1 || !s2.Tools {
		t.Fatalf("%+v", s2)
	}

	empty := &runSession{PersistID: "20261003-test-bbbb", Cwd: s.Cwd, Target: s.Target}
	if err := saveRunPersist(empty); err != nil {
		t.Fatal(err)
	}
	got = latestPersistedChat(s.Cwd)
	if got == nil || got.ID != s.PersistID {
		t.Fatalf("empty session shadowed history: %+v", got)
	}

	tc := agentToolCall{ID: "c1", Type: "function"}
	tc.Function.Name = "list_dir"
	tc.Function.Arguments = `{"path":"."}`
	s.Agent = []agentMsg{
		{Role: "user", Content: "look around"},
		{Role: "assistant", ToolCalls: []agentToolCall{tc}},
		{Role: "tool", Content: "main.go\n", ToolCallID: "c1", Name: "list_dir"},
		{Role: "assistant", Content: "there is a main.go"},
	}
	if err := saveRunPersist(s); err != nil {
		t.Fatal(err)
	}
	p, err = loadPersistedChat(s.PersistID)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Agent) != 4 || len(p.Agent[1].ToolCalls) != 1 || p.Agent[1].ToolCalls[0].Function.Name != "list_dir" {
		t.Fatalf("tool_calls roundtrip: %+v", p.Agent)
	}
	var hist strings.Builder
	s3 := &runSession{Agent: p.Agent}
	printSessionHistory(&hist, s3)
	if !strings.Contains(hist.String(), "you>") || !strings.Contains(hist.String(), "look around") || !strings.Contains(hist.String(), "main.go") {
		t.Fatalf("history print:\n%s", hist.String())
	}
	if filepath.Base(p.ID+".json") == "" {
		t.Fatal("path")
	}
	if safeSessionID("ok-id_1") == "" {
		t.Fatal("ok id")
	}
	if safeSessionID("bad/id") != "id" {
		t.Fatalf("%q", safeSessionID("bad/id"))
	}
}
