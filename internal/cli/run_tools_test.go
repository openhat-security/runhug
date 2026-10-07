package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveWorkspacePath(t *testing.T) {
	cwd := t.TempDir()
	abs, err := resolveWorkspacePath(cwd, "a/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(abs, cwd) {
		t.Fatalf("%s", abs)
	}
	if _, err := resolveWorkspacePath(cwd, "../escape"); err == nil {
		t.Fatal("expected escape error")
	}
	abs2, err := resolveWorkspacePath(cwd, filepath.Join(cwd, "a/b.txt"))
	if err != nil || abs2 != abs {
		t.Fatalf("abs normalize: %s %v", abs2, err)
	}
	if err := os.MkdirAll(filepath.Join(cwd, "cmd/c2edux/agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveWorkspacePath(cwd, "/c2edux/agent/client.go")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cwd, "cmd/c2edux/agent/client.go")
	if got != want {
		t.Fatalf("leading-slash rebase: %s want %s", got, want)
	}
	if !looksLikeDirectList("ls") || !looksLikeDirectList("ls -la") || looksLikeDirectList("ls; rm -rf /") {
		t.Fatal("direct list detector")
	}
	if looksLikeCodingAsk("ls") || looksLikeCodingAsk("do not continue earlier writes") {
		t.Fatal("false coding ask")
	}
	if !looksLikeWriteContinue("continue") || looksLikeWriteContinue("ls") {
		t.Fatal("write-continue detector")
	}
	var buf strings.Builder
	printTurnFileSummary(&buf, []fileOp{{Op: "wrote", Path: "a.go"}, {Op: "appended", Path: "a.go"}, {Op: "edited", Path: "b.go"}})
	if !strings.Contains(buf.String(), "a.go") || !strings.Contains(buf.String(), "b.go") {
		t.Fatalf("%s", buf.String())
	}
}

func TestEnforcePathFocusAndNoOverwrite(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, "cmd/c2edux/agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := enforcePathFocus("cmd", cwd, "write_file", `{"path":"internal/c2edux/agent.go","content":"x"}`); err == nil {
		t.Fatal("expected focus rejection")
	}
	if err := enforcePathFocus("cmd", cwd, "write_file", `{"path":"cmd/main.go","content":"x"}`); err == nil {
		t.Fatal("expected cmd/main.go rejection when subpackages exist")
	}
	if err := enforcePathFocus("cmd", cwd, "write_file", `{"path":"cmd/c2edux/agent/main.go","content":"x"}`); err != nil {
		t.Fatal(err)
	}
	_, err := runAgentTool(cwd, "write_file", `{"path":"cmd/c2edux/agent/main.go","content":"package main\n"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runAgentTool(cwd, "write_file", `{"path":"cmd/c2edux/agent/main.go","content":"package main\n\nfunc main() {}\n"}`)
	if err == nil || !strings.Contains(err.Error(), "edit_file") {
		t.Fatalf("expected overwrite refusal, got %v", err)
	}
	_, err = runAgentTool(cwd, "edit_file", `{"path":"cmd/c2edux/agent/main.go","old_string":"package main\n","new_string":"package main\n\nfunc main() {}\n"}`)
	if err != nil {
		t.Fatal(err)
	}
	glued := "'" + filepath.Join(cwd, "internal") + "''" + filepath.Join(cwd, "cmd") + "'"
	if got := pathMention("continue " + glued); got != "cmd" && !strings.HasSuffix(got, "cmd") {
		t.Fatalf("glued drag-drop path: %q", got)
	}
	ask := filepath.Join(cwd, "cmd") + " finish this and the integration with '" + filepath.Join(cwd, "internal/c2edux") + "'"
	roots := pathMentions(ask)
	if len(roots) < 2 {
		t.Fatalf("expected cmd + internal/c2edux, got %q", roots)
	}
	if !looksLikeCodingAsk(ask) || !looksLikeAdviceOnly("## Next Steps\n\n- [ ] Add tests\n\nI'm happy to help adapt this.\n\n```go\nx\n```\n\n```go\ny\n```\n") {
		t.Fatal("coding/advice detectors")
	}
	if err := enforcePathFocusAny([]string{"cmd", "internal/c2edux"}, cwd, "write_file", `{"path":"internal/c2edux/agent.go","content":"x"}`); err != nil {
		t.Fatal(err)
	}
}

func TestRunAgentToolsCRUD(t *testing.T) {
	cwd := t.TempDir()
	out, err := runAgentTool(cwd, "write_file", `{"path":"hello.txt","content":"hi\n"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "wrote") {
		t.Fatal(out)
	}
	raw, err := os.ReadFile(filepath.Join(cwd, "hello.txt"))
	if err != nil || string(raw) != "hi\n" {
		t.Fatalf("%q %v", raw, err)
	}
	out, err = runAgentTool(cwd, "edit_file", `{"path":"hello.txt","old_string":"hi","new_string":"yo"}`)
	if err != nil {
		t.Fatal(err)
	}
	_ = out
	raw, _ = os.ReadFile(filepath.Join(cwd, "hello.txt"))
	if string(raw) != "yo\n" {
		t.Fatalf("%q", raw)
	}
	listed, err := runAgentTool(cwd, "list_dir", `{"path":"."}`)
	if err != nil || !strings.Contains(listed, "hello.txt") {
		t.Fatalf("%q %v", listed, err)
	}
	read, err := runAgentTool(cwd, "read_file", `{"path":"hello.txt"}`)
	if err != nil || read != "yo\n" {
		t.Fatalf("%q %v", read, err)
	}
	dirList, err := runAgentTool(cwd, "read_file", `{"path":"."}`)
	if err != nil || !strings.Contains(dirList, "hello.txt") || !strings.HasPrefix(dirList, dirListPrefix) {
		t.Fatalf("dir read %q %v", dirList, err)
	}
	listed2 := map[string]bool{}
	if msg, ok := skipRepeatDirExplore(cwd, "list_dir", `{"path":"."}`, listed2); ok || msg != "" {
		t.Fatal("first list should run")
	}
	if msg, ok := skipRepeatDirExplore(cwd, "read_file", `{"path":"."}`, listed2); !ok || !strings.Contains(msg, "already listed") {
		t.Fatalf("expected skip, got %q %v", msg, ok)
	}
	cut := `{"path":"partial.go","content":"package main\n\nfunc main() {\n  fmt.Println(\"hi`
	out, err = runAgentTool(cwd, "write_file", cut)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "truncated") {
		t.Fatalf("%s", out)
	}
	raw, _ = os.ReadFile(filepath.Join(cwd, "partial.go"))
	if !strings.Contains(string(raw), "package main") {
		t.Fatalf("%q", raw)
	}
	out, err = runAgentTool(cwd, "append_file", `{"path":"partial.go","content":"\n}\n"}`)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(cwd, "partial.go"))
	if !strings.Contains(string(raw), "}\n") {
		t.Fatalf("append %q %s", raw, out)
	}
}

func TestExtractFileDumps(t *testing.T) {
	got := extractFileDumps("intro\nFILEPATH: cmd/c2edux/agent/main.go\n---\npackage main\n\nfunc main() {}\n---\n", "")
	if len(got) != 1 || got[0][0] != "cmd/c2edux/agent/main.go" || !strings.Contains(got[0][1], "package main") {
		t.Fatalf("%q", got)
	}
	got = extractFileDumps("```go hello.go\npackage main\n```\n", "")
	if len(got) != 1 || got[0][0] != "hello.go" {
		t.Fatalf("%q", got)
	}
	got = extractFileDumps("```\njust code\n```\n", "fallback.rs")
	if len(got) != 1 || got[0][0] != "fallback.rs" {
		t.Fatalf("%q", got)
	}
	got = extractFileDumps("package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n", "internal/c2edux/agent.go")
	if len(got) != 1 || got[0][0] != "internal/c2edux/agent.go" || !strings.Contains(got[0][1], "package main") {
		t.Fatalf("raw dump %q", got)
	}
	msgs := buildFileDumpMessages("internal/c2edux/agent.go", nil)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Content, "FILEPATH:") || !strings.Contains(msgs[0].Content, "internal/c2edux/agent.go") {
		t.Fatalf("%q", msgs)
	}
}

func TestLastWritePathPrefersLatestUserPath(t *testing.T) {
	wd, _ := os.Getwd()
	quoted := "'" + filepath.Join(wd, "cmd", "c2edux", "agent") + "'"
	var tc agentToolCall
	tc.Function.Name = "write_file"
	tc.Function.Arguments = `{"path":"internal/c2edux/agent.go","content":"x"}`
	msgs := []agentMsg{
		{Role: "assistant", ToolCalls: []agentToolCall{tc}},
		{Role: "user", Content: "continue writing the code " + quoted},
	}
	got := lastWritePath(msgs)
	if got != "cmd/c2edux/agent" && !strings.HasSuffix(got, "cmd/c2edux/agent") {
		t.Fatalf("got %q want cmd/c2edux/agent", got)
	}
}

func TestSummarizeToolArgs(t *testing.T) {
	got := summarizeToolArgs("write_file", `{"path":"hello_world.go","content":"package main\n\nfunc main() {}"}`)
	if got != "hello_world.go" {
		t.Fatalf("%q", got)
	}
	if summarizeToolArgs("list_dir", `{"path":"."}`) != "." {
		t.Fatal(summarizeToolArgs("list_dir", `{"path":"."}`))
	}
	if summarizeToolArgs("bash", `{"command":"ls -la"}`) != "ls -la" {
		t.Fatal(summarizeToolArgs("bash", `{"command":"ls -la"}`))
	}
}

func TestPrintToolResultListDirColumns(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf strings.Builder
	printToolCall(&buf, "list_dir", `{"path":"."}`)
	printToolResult(&buf, "list_dir", ".git/\nCHANGELOG.md\nhello_world.go\ninternal/\n", nil)
	out := buf.String()
	if strings.Contains(out, `"path"`) {
		t.Fatalf("raw json leaked:\n%s", out)
	}
	if !strings.Contains(out, "→") || !strings.Contains(out, "list_dir") {
		t.Fatalf("call line:\n%s", out)
	}
	if !strings.Contains(out, "hello_world.go") || !strings.Contains(out, ".git/") {
		t.Fatalf("grid:\n%s", out)
	}
	// Must not squash everything onto the check line.
	if strings.Contains(out, "✓ .git/") {
		t.Fatalf("squashed: %s", out)
	}
}

func TestCompleteSlashLine(t *testing.T) {
	got, ok := completeSlashLine("/he")
	if !ok || got != "/help " {
		t.Fatalf("%q %v", got, ok)
	}
	matches := filterSlashCatalog("/t")
	if len(matches) < 1 {
		t.Fatal(matches)
	}
}

func TestCompleteCwdLine(t *testing.T) {
	cwd := t.TempDir()
	for _, d := range []string{"alpha", "abyss", "beta"} {
		if err := os.Mkdir(filepath.Join(cwd, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(cwd, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, ok := completeInputLine(cwd, "/cwd al")
	if !ok || got != "/cwd alpha/" {
		t.Fatalf("unique %q %v", got, ok)
	}

	got, ok = completeInputLine(cwd, "/cwd a")
	if got != "/cwd a" {
		t.Fatalf("lcp %q %v", got, ok)
	}
	_, names := cwdDirMatches(cwd, "a")
	if len(names) != 2 {
		t.Fatalf("names %v", names)
	}

	hint := suggestionLine(cwd, "/cwd ")
	if !strings.Contains(hint, "../") || !strings.Contains(hint, "alpha/") || !strings.Contains(hint, "beta/") {
		t.Fatalf("hint %q", hint)
	}
	if strings.Contains(hint, "file.txt") {
		t.Fatalf("files should not appear: %q", hint)
	}
	grid := formatLSColumns([]string{"../", "bin/", "crypto-wallet/", "hfpacks/", "internal/", "packaging/"}, 40)
	if len(grid) < 2 {
		t.Fatalf("expected multiple rows, got %v", grid)
	}
	joined := strings.Join(grid, "\n")
	if !strings.Contains(joined, "bin/") || !strings.Contains(joined, "internal/") {
		t.Fatalf("grid:\n%s", joined)
	}
	for _, line := range grid {
		if displayWidth(line) > 40 {
			t.Fatalf("line wider than 40 (%d): %q", displayWidth(line), line)
		}
		if !strings.HasPrefix(line, "  ") {
			t.Fatalf("expected indent: %q", line)
		}
	}

	got, ok = completeInputLine(cwd, "/cwd")
	if !ok || got != "/cwd " {
		t.Fatalf("command %q %v", got, ok)
	}
}

func TestAgentToolDefs(t *testing.T) {
	defs := agentToolDefs()
	if len(defs) < 5 {
		t.Fatalf("%d", len(defs))
	}
	names := map[string]bool{}
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	for _, want := range []string{"list_dir", "read_file", "write_file", "append_file", "edit_file", "bash"} {
		if !names[want] {
			t.Fatalf("missing %s", want)
		}
	}
}
