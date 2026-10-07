package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	toolMaxReadBytes = 120_000
	toolMaxOutBytes  = 48_000
	toolBashTimeout  = 60 * time.Second
	dirListPrefix    = "directory (read a filename from this list, not the folder):\n"
)

type openaiToolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

func agentToolDefs() []openaiToolDef {
	must := func(name, desc, params string) openaiToolDef {
		var t openaiToolDef
		t.Type = "function"
		t.Function.Name = name
		t.Function.Description = desc
		t.Function.Parameters = json.RawMessage(params)
		return t
	}
	return []openaiToolDef{
		must("list_dir", "List files and directories at path (relative to workspace).",
			`{"type":"object","properties":{"path":{"type":"string","description":"Directory path (default .)"}},"required":[]}`),
		must("read_file", "Read a UTF-8 text file. Path must be a file (e.g. foo.go), not a directory. A directory path is listed instead — then read a filename from that list.",
			`{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer","description":"1-based start line"},"limit":{"type":"integer","description":"max lines"}},"required":["path"]}`),
		must("write_file", "Create a new file only (refuses to overwrite). Keep content under 1500 characters; use append_file for the rest. Paths relative to workspace cwd.",
			`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`),
		must("append_file", "Append text to a file (creates it if needed). Use after write_file when the file is large; keep content under 1500 characters per call.",
			`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`),
		must("edit_file", "Edit an existing file by replacing an exact old_string with new_string (first match). Prefer this over write_file when the file already exists.",
			`{"type":"object","properties":{"path":{"type":"string"},"old_string":{"type":"string"},"new_string":{"type":"string"}},"required":["path","old_string","new_string"]}`),
		must("glob", "Find files under the workspace matching a glob pattern.",
			`{"type":"object","properties":{"pattern":{"type":"string"}},"required":["pattern"]}`),
		must("bash", "Run a shell command in the workspace directory. Prefer specialized tools for file edits.",
			`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`),
	}
}

func resolveWorkspacePath(cwd, p string) (string, error) {
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	p, err = normalizeToolPath(cwd, p)
	if err != nil {
		return "", err
	}
	if p == "" {
		p = "."
	}
	var abs string
	if filepath.IsAbs(p) {
		abs, err = filepath.Abs(p)
	} else {
		abs, err = filepath.Abs(filepath.Join(cwd, p))
	}
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(cwd, abs)
	if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
		return "", fmt.Errorf("path %q escapes workspace %s", p, cwd)
	}
	return abs, nil
}

func listDirAt(cwd, path string) (string, error) {
	abs, err := resolveWorkspacePath(cwd, path)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", fmt.Errorf("path %q is a file — use read_file", path)
	}
	ents, err := os.ReadDir(abs)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, e := range ents {
		suffix := ""
		if e.IsDir() {
			suffix = "/"
		}
		fmt.Fprintf(&b, "%s%s\n", e.Name(), suffix)
	}
	return truncateToolOut(b.String()), nil
}

func toolArgPath(argsJSON string) string {
	var args map[string]any
	_ = json.Unmarshal([]byte(argsJSON), &args)
	p, _ := args["path"].(string)
	p = strings.TrimSpace(p)
	if p == "" {
		return "."
	}
	return p
}

// skipRepeatDirExplore skips a second list_dir/read_file of the same directory in one round.
func skipRepeatDirExplore(cwd, name, argsJSON string, listed map[string]bool) (string, bool) {
	if listed == nil || (name != "list_dir" && name != "read_file") {
		return "", false
	}
	abs, err := resolveWorkspacePath(cwd, toolArgPath(argsJSON))
	if err != nil {
		return "", false
	}
	st, err := os.Stat(abs)
	if err != nil || !st.IsDir() {
		return "", false
	}
	if listed[abs] {
		return "already listed this directory this round — read a file inside it (e.g. foo.go)", true
	}
	listed[abs] = true
	return "", false
}

// normalizeToolPath cleans model/user paths: file://, abs→rel, glued drag-drops.
func normalizeToolPath(cwd, p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", nil
	}
	p = strings.TrimPrefix(p, "file://")
	// Drag-dropping two paths can glue quotes: 'a''b' — keep the last segment.
	if strings.Count(p, "'") >= 2 || strings.Count(p, `"`) >= 2 {
		if parts := quotedPathParts(p); len(parts) > 0 {
			p = parts[len(parts)-1]
		}
	}
	p = strings.Trim(p, "`\"'")
	p = filepath.Clean(p)
	if cwd != "" {
		if absCwd, err := filepath.Abs(cwd); err == nil {
			absCwd = filepath.Clean(absCwd)
			if filepath.IsAbs(p) {
				if rel, err := filepath.Rel(absCwd, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					p = rel
				} else {
					p = preferWorkspaceRel(absCwd, strings.TrimPrefix(p, string(filepath.Separator)))
				}
			}
		}
	}
	return p, nil
}

// preferWorkspaceRel maps model paths like /c2edux/agent/client.go onto cmd/ or internal/.
func preferWorkspaceRel(cwd, rel string) string {
	rel = filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(rel), "/"))
	if rel == "" || rel == "." {
		return rel
	}
	candidates := []string{rel, "cmd/" + rel, "internal/" + rel}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(cwd, filepath.FromSlash(c))); err == nil {
			return filepath.FromSlash(c)
		}
	}
	for _, c := range candidates {
		dir := filepath.Join(cwd, filepath.FromSlash(filepath.Dir(c)))
		if st, err := os.Stat(dir); err == nil && st.IsDir() && filepath.Dir(c) != "." {
			return filepath.FromSlash(c)
		}
	}
	return filepath.FromSlash(rel)
}

type fileOp struct {
	Op   string
	Path string
}

func fileOpFromTool(name, argsJSON string, err error) (fileOp, bool) {
	if err != nil {
		return fileOp{}, false
	}
	var op string
	switch name {
	case "write_file":
		op = "wrote"
	case "append_file":
		op = "appended"
	case "edit_file":
		op = "edited"
	default:
		return fileOp{}, false
	}
	path := toolPathFromArgs(name, argsJSON)
	if path == "" {
		return fileOp{}, false
	}
	return fileOp{Op: op, Path: path}, true
}

func printTurnFileSummary(w io.Writer, ops []fileOp) {
	if w == nil || len(ops) == 0 {
		return
	}
	seen := map[string]fileOp{}
	var order []string
	for _, op := range ops {
		k := op.Path
		if _, ok := seen[k]; !ok {
			order = append(order, k)
		}
		seen[k] = op
	}
	fmt.Fprintln(w, dim("files this turn"))
	for _, k := range order {
		op := seen[k]
		fmt.Fprintf(w, "  %s  %s\n", dim(op.Op), op.Path)
	}
}

func looksLikeDirectList(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if strings.ContainsAny(s, ";|&`$()<>") {
		return false
	}
	lower := strings.ToLower(s)
	switch lower {
	case "ls", "ll", "dir", "pwd", "ls -l", "ls -la", "ls -al", "ls -alh", "ls -lh":
		return true
	}
	fields := strings.Fields(lower)
	if len(fields) == 0 {
		return false
	}
	if fields[0] != "ls" && fields[0] != "dir" {
		return false
	}
	for _, f := range fields[1:] {
		if strings.HasPrefix(f, "-") {
			continue
		}
		if strings.Contains(f, "..") {
			return false
		}
	}
	return true
}

func runDirectList(cwd, s string) (name, args, out string, err error) {
	lower := strings.ToLower(strings.TrimSpace(s))
	if lower == "pwd" {
		name, args = "bash", `{"command":"pwd"}`
		return name, args, cwd, nil
	}
	path := "."
	for _, f := range strings.Fields(strings.TrimSpace(s)) {
		if strings.HasPrefix(f, "-") {
			continue
		}
		lf := strings.ToLower(f)
		if lf == "ls" || lf == "ll" || lf == "dir" {
			continue
		}
		path = f
		break
	}
	raw, _ := json.Marshal(map[string]string{"path": path})
	name, args = "list_dir", string(raw)
	out, err = runAgentTool(cwd, name, args)
	return name, args, out, err
}

func quotedPathParts(text string) []string {
	var out []string
	for _, quote := range []byte{'\'', '"'} {
		s := text
		for {
			i := strings.IndexByte(s, quote)
			if i < 0 {
				break
			}
			s = s[i+1:]
			j := strings.IndexByte(s, quote)
			if j < 0 {
				break
			}
			part := strings.TrimSpace(s[:j])
			s = s[j+1:]
			if part != "" && (strings.Contains(part, "/") || strings.Contains(part, string(filepath.Separator)) || strings.Contains(part, ".")) {
				out = append(out, part)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return out
}

func toolPathFromArgs(name, argsJSON string) string {
	var args map[string]any
	_ = json.Unmarshal([]byte(argsJSON), &args)
	if args != nil {
		if p, _ := args["path"].(string); strings.TrimSpace(p) != "" {
			return p
		}
		if p, _ := args["pattern"].(string); name == "glob" && strings.TrimSpace(p) != "" {
			return p
		}
	}
	if name == "write_file" || name == "append_file" || name == "edit_file" || name == "read_file" {
		if p, _, ok := salvageWriteArgs(argsJSON); ok {
			return p
		}
		if p, ok := jsonStringField(argsJSON, "path"); ok {
			return p
		}
	}
	return ""
}

func enforcePathFocusAny(roots []string, cwd, name, argsJSON string) error {
	if len(roots) == 0 {
		return nil
	}
	var last error
	for _, root := range roots {
		err := enforcePathFocus(root, cwd, name, argsJSON)
		if err == nil {
			return nil
		}
		last = err
	}
	return last
}

// enforcePathFocus rejects writes/edits outside the directory/file the user named.
func enforcePathFocus(focus, cwd, name, argsJSON string) error {
	switch name {
	case "write_file", "append_file", "edit_file":
	default:
		return nil
	}
	focus = strings.TrimSpace(focus)
	if focus == "" {
		return nil
	}
	path := toolPathFromArgs(name, argsJSON)
	if path == "" {
		return nil
	}
	focusNorm, err := normalizeToolPath(cwd, focus)
	if err != nil {
		return nil
	}
	pathNorm, err := normalizeToolPath(cwd, path)
	if err != nil {
		return err
	}
	focusAbs, err := resolveWorkspacePath(cwd, focusNorm)
	if err != nil {
		// Focus may be a not-yet-created file path; compare relatively.
		focusAbs = filepath.Join(cwd, focusNorm)
	}
	pathAbs, err := resolveWorkspacePath(cwd, pathNorm)
	if err != nil {
		return err
	}
	if st, err := os.Stat(focusAbs); err == nil && st.IsDir() {
		rel, err := filepath.Rel(focusAbs, pathAbs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("path %q is outside user focus %q — write under that directory", pathNorm, focusNorm)
		}
		// Focus is a package root with subpackages (e.g. cmd/) — do not create
		// files directly in that root (cmd/main.go). Require a nested path.
		if name == "write_file" && rejectFocusRootFile(focusAbs, pathAbs, rel) {
			hint := focusNestHint(focusAbs, focusNorm)
			return fmt.Errorf("path %q sits in focus root %q — write inside an existing subpackage%s", pathNorm, focusNorm, hint)
		}
		return nil
	}
	// Focus is a file (or not created yet): require the same file.
	if filepath.Clean(pathAbs) != filepath.Clean(focusAbs) {
		// Allow writing a file inside focus when focus looks like a directory path.
		if !strings.Contains(filepath.Base(focusNorm), ".") {
			rel, err := filepath.Rel(focusAbs, pathAbs)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				if name == "write_file" && rejectFocusRootFile(focusAbs, pathAbs, rel) {
					hint := focusNestHint(focusAbs, focusNorm)
					return fmt.Errorf("path %q sits in focus root %q — write inside an existing subpackage%s", pathNorm, focusNorm, hint)
				}
				return nil
			}
		}
		return fmt.Errorf("path %q is outside user focus %q — edit that path", pathNorm, focusNorm)
	}
	return nil
}

// rejectFocusRootFile is true when path is a direct child file of focus and focus
// already has subdirectories (so the real packages live one level down).
func rejectFocusRootFile(focusAbs, pathAbs, rel string) bool {
	if rel == "." || rel == "" {
		return true
	}
	if strings.Contains(rel, string(filepath.Separator)) {
		return false // nested: cmd/c2edux/agent/main.go
	}
	// Direct child file (cmd/main.go). Allow only if that file already exists
	// (edit/append path) or focus has no subdirs yet.
	if st, err := os.Stat(pathAbs); err == nil && st.Mode().IsRegular() {
		return false
	}
	ents, err := os.ReadDir(focusAbs)
	if err != nil {
		return false
	}
	hasSubdir := false
	for _, e := range ents {
		if e.IsDir() {
			hasSubdir = true
			break
		}
	}
	return hasSubdir
}

func focusNestHint(focusAbs, focusNorm string) string {
	ents, err := os.ReadDir(focusAbs)
	if err != nil {
		return ""
	}
	var dirs []string
	for _, e := range ents {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, filepath.Join(focusNorm, e.Name()))
			if len(dirs) >= 3 {
				break
			}
		}
	}
	if len(dirs) == 0 {
		return ""
	}
	return " (try " + strings.Join(dirs, ", ") + ", …)"
}

func runAgentTool(cwd, name, argsJSON string) (string, error) {
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		if name == "write_file" || name == "append_file" {
			path, content, ok := salvageWriteArgs(argsJSON)
			if ok {
				return applyFileWrite(cwd, name, path, content, true)
			}
		}
		return "", fmt.Errorf("invalid tool args: %w", err)
	}
	str := func(k string) string {
		v, _ := args[k].(string)
		return v
	}
	intish := func(k string) int {
		switch v := args[k].(type) {
		case float64:
			return int(v)
		case int:
			return v
		default:
			return 0
		}
	}

	switch name {
	case "list_dir":
		path := str("path")
		if path == "" {
			path = "."
		}
		return listDirAt(cwd, path)

	case "read_file":
		path := str("path")
		abs, err := resolveWorkspacePath(cwd, path)
		if err != nil {
			return "", err
		}
		if st, err := os.Stat(abs); err == nil && st.IsDir() {
			listing, err := listDirAt(cwd, path)
			if err != nil {
				return "", err
			}
			return dirListPrefix + listing, nil
		}
		raw, err := os.ReadFile(abs)
		if err != nil {
			return "", err
		}
		if len(raw) > toolMaxReadBytes {
			raw = raw[:toolMaxReadBytes]
		}
		text := string(raw)
		off, lim := intish("offset"), intish("limit")
		if off > 0 || lim > 0 {
			lines := strings.Split(text, "\n")
			start := 0
			if off > 0 {
				start = off - 1
			}
			if start > len(lines) {
				start = len(lines)
			}
			end := len(lines)
			if lim > 0 && start+lim < end {
				end = start + lim
			}
			text = strings.Join(lines[start:end], "\n")
		}
		return truncateToolOut(text), nil

	case "write_file":
		return applyFileWrite(cwd, name, str("path"), str("content"), false)

	case "append_file":
		return applyFileWrite(cwd, name, str("path"), str("content"), false)

	case "edit_file":
		abs, err := resolveWorkspacePath(cwd, str("path"))
		if err != nil {
			return "", err
		}
		raw, err := os.ReadFile(abs)
		if err != nil {
			return "", err
		}
		oldS, newS := str("old_string"), str("new_string")
		if oldS == "" {
			return "", fmt.Errorf("old_string required")
		}
		text := string(raw)
		if !strings.Contains(text, oldS) {
			return "", fmt.Errorf("old_string not found in %s", abs)
		}
		updated := strings.Replace(text, oldS, newS, 1)
		if err := os.WriteFile(abs, []byte(updated), 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("edited %s", abs), nil

	case "glob":
		pattern := str("pattern")
		if pattern == "" {
			return "", fmt.Errorf("pattern required")
		}
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(cwd, pattern)
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return "", err
		}
		var kept []string
		for _, m := range matches {
			if _, err := resolveWorkspacePath(cwd, m); err != nil {
				continue
			}
			rel, err := filepath.Rel(cwd, m)
			if err != nil {
				kept = append(kept, m)
			} else {
				kept = append(kept, rel)
			}
			if len(kept) >= 200 {
				break
			}
		}
		if len(kept) == 0 {
			return "(no matches)", nil
		}
		return truncateToolOut(strings.Join(kept, "\n")), nil

	case "bash":
		cmd := str("command")
		if strings.TrimSpace(cmd) == "" {
			return "", fmt.Errorf("command required")
		}
		ctx, cancel := context.WithTimeout(context.Background(), toolBashTimeout)
		defer cancel()
		c := exec.CommandContext(ctx, "bash", "-lc", cmd)
		c.Dir = cwd
		out, err := c.CombinedOutput()
		text := truncateToolOut(string(out))
		if err != nil {
			if text == "" {
				return "", err
			}
			return text + "\n(exit error: " + err.Error() + ")", nil
		}
		if text == "" {
			return "(no output)", nil
		}
		return text, nil

	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

func truncateToolOut(s string) string {
	if len(s) <= toolMaxOutBytes {
		return s
	}
	return s[:toolMaxOutBytes] + "\n…(truncated)"
}

const toolDisplayMaxLines = 40

func printToolCall(w io.Writer, name, argsJSON string) {
	fmt.Fprintf(w, "%s %s  %s\n", cyan("→"), bold(name), dim(summarizeToolArgs(name, argsJSON)))
}

func summarizeToolArgs(name, argsJSON string) string {
	var args map[string]any
	_ = json.Unmarshal([]byte(argsJSON), &args)
	str := func(k string) string {
		v, _ := args[k].(string)
		return strings.TrimSpace(v)
	}
	switch name {
	case "write_file", "read_file", "edit_file", "append_file":
		if p := str("path"); p != "" {
			return p
		}
		if path, _, ok := salvageWriteArgs(argsJSON); ok {
			return path
		}
	case "list_dir":
		p := str("path")
		if p == "" {
			return "."
		}
		return p
	case "glob":
		return str("pattern")
	case "bash":
		return str("command")
	}
	return truncateRunes(compactJSON(argsJSON), 64)
}

func compactJSON(s string) string {
	var buf bytes.Buffer
	if json.Compact(&buf, []byte(s)) == nil {
		return buf.String()
	}
	return strings.Join(strings.Fields(s), " ")
}

func printToolResult(w io.Writer, name, result string, err error) {
	if err != nil {
		fmt.Fprintf(w, "  %s %s\n", yellow("✗"), err)
		return
	}
	if name == "read_file" {
		if rest, ok := strings.CutPrefix(result, dirListPrefix); ok {
			fmt.Fprintf(w, "  %s %s\n", dim("dir"), dim("not a file — listing"))
			printToolResult(w, "list_dir", rest, nil)
			return
		}
	}
	switch name {
	case "list_dir", "glob":
		names := splitToolLines(result)
		if len(names) == 0 {
			fmt.Fprintf(w, "  %s %s\n", green("✓"), dim("(empty)"))
			return
		}
		for _, line := range formatLSColumns(names, metricsTermWidth()) {
			fmt.Fprintln(w, line)
		}
	case "write_file", "edit_file", "append_file":
		fmt.Fprintf(w, "  %s %s\n", green("✓"), dim(result))
	default:
		printToolBlock(w, result)
	}
}

func splitToolLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && line != "(no matches)" {
			out = append(out, line)
		}
	}
	return out
}

func printToolBlock(w io.Writer, result string) {
	result = strings.TrimRight(result, "\n")
	if result == "" || result == "(no output)" {
		fmt.Fprintf(w, "  %s %s\n", green("✓"), dim("(no output)"))
		return
	}
	lines := strings.Split(result, "\n")
	clipped := false
	if len(lines) > toolDisplayMaxLines {
		lines = lines[:toolDisplayMaxLines]
		clipped = true
	}
	fmt.Fprintf(w, "  %s\n", green("✓"))
	for _, line := range lines {
		fmt.Fprintf(w, "    %s\n", line)
	}
	if clipped {
		fmt.Fprintln(w, dim("    …"))
	}
}

func applyFileWrite(cwd, name, path, content string, truncated bool) (string, error) {
	abs, err := resolveWorkspacePath(cwd, path)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(abs); err == nil && st.IsDir() {
		return "", fmt.Errorf("path %q is a directory — give a file path inside it", path)
	}
	if strings.HasSuffix(strings.TrimSpace(path), "/") || strings.HasSuffix(abs, string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is a directory — give a file path inside it", path)
	}
	// Prefer edit_file for existing files unless this is a truncated salvage/append.
	if name == "write_file" && !truncated {
		if st, err := os.Stat(abs); err == nil && st.Mode().IsRegular() && st.Size() > 0 {
			return "", fmt.Errorf("file exists (%s) — use edit_file to change it, or append_file to add on; write_file refused to overwrite", abs)
		}
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	if name == "append_file" {
		f, err := os.OpenFile(abs, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return "", err
		}
		n, err := f.WriteString(content)
		_ = f.Close()
		if err != nil {
			return "", err
		}
		msg := fmt.Sprintf("appended %s (%d bytes)", abs, n)
		if truncated {
			msg += ". JSON was truncated — keep using append_file with the remaining text in chunks under 1500 characters."
		}
		return msg, nil
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return "", err
	}
	msg := fmt.Sprintf("wrote %s (%d bytes)", abs, len(content))
	if truncated {
		msg += ". JSON was truncated — finish the file with append_file using the remaining text in chunks under 1500 characters."
	}
	return msg, nil
}

func salvageWriteArgs(raw string) (path, content string, ok bool) {
	path, okPath := jsonStringField(raw, "path")
	content, okContent := jsonStringField(raw, "content")
	if !okPath || path == "" {
		return "", "", false
	}
	_ = okContent
	return path, content, true
}

func jsonStringField(raw, key string) (string, bool) {
	needle := `"` + key + `"`
	i := strings.Index(raw, needle)
	if i < 0 {
		return "", false
	}
	rest := strings.TrimSpace(raw[i+len(needle):])
	if !strings.HasPrefix(rest, ":") {
		return "", false
	}
	rest = strings.TrimSpace(rest[1:])
	if !strings.HasPrefix(rest, `"`) {
		return "", false
	}
	return unquoteJSONString(rest[1:]), true
}

func unquoteJSONString(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			break
		}
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		if i+1 >= len(s) {
			break
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case '"', '\\', '/':
			b.WriteByte(s[i])
		case 'u':
			if i+4 >= len(s) {
				break
			}
			i += 4
			b.WriteByte('?')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func lastWritePath(msgs []agentMsg) string {
	// Prefer a path the user just named (so "finish cmd/…" isn't overridden by an older write_file).
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			if p := pathMention(msgs[i].Content); p != "" {
				return p
			}
			break
		}
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		for _, tc := range msgs[i].ToolCalls {
			switch tc.Function.Name {
			case "write_file", "append_file":
				if p, _, ok := salvageWriteArgs(tc.Function.Arguments); ok && p != "" {
					return p
				}
				if p, ok := jsonStringField(tc.Function.Arguments, "path"); ok && p != "" {
					return p
				}
			}
		}
		if p := pathMention(msgs[i].Content); p != "" {
			return p
		}
	}
	return ""
}

func lastWritePartial(msgs []agentMsg) (path, content string) {
	for i := len(msgs) - 1; i >= 0; i-- {
		for _, tc := range msgs[i].ToolCalls {
			switch tc.Function.Name {
			case "write_file", "append_file":
				p, c, ok := salvageWriteArgs(tc.Function.Arguments)
				if !ok {
					p, _ = jsonStringField(tc.Function.Arguments, "path")
					c, _ = jsonStringField(tc.Function.Arguments, "content")
				}
				if p != "" {
					return p, c
				}
			}
		}
	}
	return lastWritePath(msgs), ""
}

func pathMention(text string) string {
	all := pathMentions(text)
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1]
}

func pathMentions(text string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = tidyDumpPath(strings.TrimSpace(p))
		if p == "" || seen[p] {
			return
		}
		if !looksLikeRelPath(p) {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, p := range quotedPathParts(text) {
		add(p)
	}
	// Unquoted absolute / workspace-relative tokens (drag without quotes).
	for _, tok := range strings.Fields(text) {
		tok = strings.Trim(tok, "`\"',")
		if strings.HasPrefix(tok, "@") {
			add(strings.TrimPrefix(tok, "@"))
			continue
		}
		if strings.HasPrefix(tok, "/") || strings.HasPrefix(tok, "./") || strings.HasPrefix(tok, "cmd/") || strings.HasPrefix(tok, "internal/") {
			add(tok)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"FILEPATH:", "File:", "path:", "Path:"} {
			if strings.HasPrefix(line, prefix) {
				add(strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, prefix)), "`\"'"))
			}
		}
	}
	return out
}

func tidyDumpPath(p string) string {
	p = strings.TrimSpace(p)
	if wd, err := os.Getwd(); err == nil {
		wd = strings.TrimRight(wd, string(os.PathSeparator)) + string(os.PathSeparator)
		if strings.HasPrefix(p, wd) {
			return strings.TrimPrefix(p, wd)
		}
	}
	return p
}

func looksLikeRelPath(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" || strings.ContainsAny(p, " \t\n") {
		return false
	}
	if strings.HasPrefix(p, "/") || strings.Contains(p, "/") || strings.Contains(p, ".") {
		return true
	}
	return false
}

func fileDumpPrompt(path string) string {
	hint := "the file you were writing"
	extra := ""
	if path != "" {
		hint = path
		if !strings.Contains(filepath.Base(path), ".") {
			extra = "\nFILEPATH must be a nested file under " + path + " (for example " + path + "/c2edux/agent/main.go), never " + path + "/main.go when subpackages exist.\n"
		}
	}
	return "Do not call tools. Do not use JSON. The previous write_file tool call was invalid.\n" +
		"Output the COMPLETE file for " + hint + " using this exact format and nothing else:\n" +
		extra +
		"FILEPATH: relative/path\n---\n<full file contents>\n---\n"
}

func buildFileDumpMessages(path string, prior []agentMsg) []agentMsg {
	p, partial := lastWritePartial(prior)
	if path == "" {
		path = p
	}
	var b strings.Builder
	b.WriteString(fileDumpPrompt(path))
	if strings.TrimSpace(partial) != "" {
		b.WriteString("\nYou already started this content (it was truncated). Re-emit the FULL file from the beginning, including what follows:\n---\n")
		b.WriteString(partial)
		if !strings.HasSuffix(partial, "\n") {
			b.WriteByte('\n')
		}
		b.WriteString("---\n")
	}
	return []agentMsg{{Role: "user", Content: b.String()}}
}

func extractFileDumps(text, fallbackPath string) [][2]string {
	text = stripDumpNoise(text)
	var out [][2]string
	s := text
	for {
		i := strings.Index(s, "FILEPATH:")
		if i < 0 {
			// also accept "File:" / "path:"
			alt := -1
			altKey := ""
			for _, key := range []string{"\nFile:", "\nPath:", "\npath:"} {
				if j := strings.Index(s, key); j >= 0 && (alt < 0 || j < alt) {
					alt = j
					altKey = key
				}
			}
			if alt < 0 {
				break
			}
			i = alt
			rest := s[i+len(altKey):]
			nl := strings.IndexByte(rest, '\n')
			if nl < 0 {
				break
			}
			path := strings.TrimSpace(rest[:nl])
			path = strings.Trim(path, "`\"'")
			body := rest[nl+1:]
			content, next := splitDumpBody(body)
			if path == "" {
				path = fallbackPath
			}
			if path != "" && strings.TrimSpace(content) != "" {
				out = append(out, [2]string{path, content})
			}
			s = next
			continue
		}
		rest := s[i+len("FILEPATH:"):]
		nl := strings.IndexByte(rest, '\n')
		if nl < 0 {
			break
		}
		path := strings.TrimSpace(rest[:nl])
		path = strings.Trim(path, "`\"'")
		body := rest[nl+1:]
		content, next := splitDumpBody(body)
		if path == "" {
			path = fallbackPath
		}
		if path != "" && strings.TrimSpace(content) != "" {
			out = append(out, [2]string{path, content})
		}
		s = next
	}
	if len(out) > 0 {
		return out
	}
	const fence = "```"
	t := text
	for {
		start := strings.Index(t, fence)
		if start < 0 {
			break
		}
		t = t[start+len(fence):]
		nl := strings.IndexByte(t, '\n')
		if nl < 0 {
			break
		}
		header := strings.TrimSpace(t[:nl])
		t = t[nl+1:]
		end := strings.Index(t, fence)
		if end < 0 {
			break
		}
		content := t[:end]
		t = t[end+len(fence):]
		path := fallbackPath
		fields := strings.Fields(header)
		if len(fields) >= 2 {
			path = fields[len(fields)-1]
		} else if len(fields) == 1 && (strings.Contains(fields[0], "/") || strings.Contains(fields[0], ".")) {
			path = fields[0]
		}
		path = strings.Trim(path, "`\"'")
		if path != "" && strings.TrimSpace(content) != "" {
			out = append(out, [2]string{path, content})
		}
	}
	if len(out) > 0 {
		return out
	}
	// Last resort: whole reply is the file body for a known path.
	if fallbackPath != "" && looksLikeSourceFile(text) {
		return [][2]string{{fallbackPath, strings.TrimSpace(text) + "\n"}}
	}
	return out
}

func splitDumpBody(body string) (content, rest string) {
	body = strings.TrimPrefix(body, "\r")
	trimmed := strings.TrimLeft(body, " \t\r\n")
	if strings.HasPrefix(trimmed, "---") {
		body = strings.TrimPrefix(trimmed, "---")
		body = strings.TrimPrefix(body, "\n")
		body = strings.TrimPrefix(body, "\r\n")
	}
	end := strings.Index(body, "\n---")
	if end >= 0 {
		return body[:end], body[end+4:]
	}
	return body, ""
}

func stripDumpNoise(text string) string {
	for {
		start := strings.Index(text, "<think>")
		if start < 0 {
			break
		}
		end := strings.Index(text[start:], "</think>")
		if end < 0 {
			text = text[:start]
			break
		}
		text = text[:start] + text[start+end+len("</think>"):]
	}
	return strings.TrimSpace(text)
}

func looksLikeSourceFile(text string) bool {
	t := strings.TrimSpace(text)
	if len(t) < 40 {
		return false
	}
	if strings.Contains(t, "FILEPATH:") || strings.Count(t, "```") >= 2 {
		return true
	}
	for _, needle := range []string{
		"package ", "fn main", "func main", "#!/", "import ", "from ", "export ",
		"#include", "use ", "pub fn", "fn ", "class ", "def ",
	} {
		if strings.Contains(t, needle) {
			return true
		}
	}
	return false
}

func applyDumpedFiles(cwd string, w io.Writer, text, fallbackPath string) (int, error) {
	dumps := extractFileDumps(text, fallbackPath)
	n := 0
	for _, d := range dumps {
		path, content := d[0], d[1]
		path, _ = normalizeToolPath(cwd, path)
		if fallbackPath != "" {
			if err := enforcePathFocus(fallbackPath, cwd, "write_file", `{"path":`+jsonQuote(path)+`}`); err != nil {
				printToolCall(w, "write_file", `{"path":`+jsonQuote(path)+`}`)
				printToolResult(w, "write_file", err.Error(), err)
				continue
			}
		}
		// Existing file: replace via write only from dump recovery (explicit full-file dump).
		abs, err := resolveWorkspacePath(cwd, path)
		if err != nil {
			return n, err
		}
		raw, _ := json.Marshal(map[string]string{"path": path})
		printToolCall(w, "write_file", string(raw))
		if st, err := os.Stat(abs); err == nil && st.IsDir() {
			err = fmt.Errorf("path %q is a directory — need a file path", path)
			printToolResult(w, "write_file", err.Error(), err)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return n, err
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			printToolResult(w, "write_file", err.Error(), err)
			return n, err
		}
		msg := fmt.Sprintf("wrote %s (%d bytes)", abs, len(content))
		printToolResult(w, "write_file", msg, nil)
		n++
	}
	return n, nil
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
