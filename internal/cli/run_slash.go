package cli

import (
	"os"
	"path/filepath"
	"strings"
)

// runSlashSpec is one slash command shown in /help and `/` autocomplete.
type runSlashSpec struct {
	Cmd  string
	Args string // optional hint, e.g. "[n]"
	Desc string
}

func runSlashCatalog() []runSlashSpec {
	return []runSlashSpec{
		{Cmd: "/help", Desc: "show commands"},
		{Cmd: "/copy", Desc: "copy last error (or last reply) to the clipboard"},
		{Cmd: "/status", Desc: "backend + readiness"},
		{Cmd: "/metrics", Args: "[on|off|mini|full]", Desc: "GPU/CPU/RAM snapshot, strip, or live"},
		{Cmd: "/full", Desc: "return to fullscreen chat (default)"},
		{Cmd: "/mini", Desc: "leave fullscreen, restore scrollback"},
		{Cmd: "/logs", Args: "[n]", Desc: "tail guest logs (GCP)"},
		{Cmd: "/model", Args: "[name]", Desc: "show or set served model"},
		{Cmd: "/plan", Desc: "read-only plan mode (no writes)"},
		{Cmd: "/agent", Desc: "implement mode (tools + permissions)"},
		{Cmd: "/perm", Args: "[ask|allow|deny]", Desc: "mutating tool permissions"},
		{Cmd: "/settings", Desc: "mode, permissions, model, GPU"},
		{Cmd: "/tools", Args: "[on|off]", Desc: "local file/shell tools (and what they do)"},
		{Cmd: "/cwd", Args: "[path]", Desc: "show or set workspace root"},
		{Cmd: "/clear", Desc: "clear conversation history"},
		{Cmd: "/sessions", Desc: "list saved chats"},
		{Cmd: "/resume", Args: "[id]", Desc: "resume a saved session"},
		{Cmd: "/new", Desc: "start a new session"},
		{Cmd: "/undo", Desc: "drop the last user turn (OpenCode-style)"},
		{Cmd: "/redo", Desc: "restore an undone turn"},
		{Cmd: "/compact", Desc: "summarize history to free context"},
		{Cmd: "/context", Args: "[up|n]", Desc: "show usage, or raise llama.cpp -c (GCP restarts the container)"},
		{Cmd: "/gpu", Args: "[up|L4|A100]", Desc: "resize Spot GPU (stop/start)"},
		{Cmd: "/export", Args: "[file]", Desc: "write the transcript as markdown"},
		{Cmd: "/rename", Args: "[title]", Desc: "name this session"},
		{Cmd: "/exit", Desc: "leave chat (Ctrl-D also exits)"},
	}
}

func filterSlashCatalog(prefix string) []runSlashSpec {
	prefix = trimSlashPrefix(prefix)
	var out []runSlashSpec
	for _, s := range runSlashCatalog() {
		name := s.Cmd
		if prefix == "" || hasSlashPrefix(name, prefix) {
			out = append(out, s)
		}
	}
	return out
}

func trimSlashPrefix(s string) string {
	// Keep leading slash in matching via hasSlashPrefix.
	return s
}

func hasSlashPrefix(cmd, typed string) bool {
	if typed == "" {
		return true
	}
	if len(typed) > len(cmd) {
		return false
	}
	for i := 0; i < len(typed); i++ {
		a, b := cmd[i], typed[i]
		if a >= 'A' && a <= 'Z' {
			a += 'a' - 'A'
		}
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}

func cwdArg(line string) (arg string, ok bool) {
	if !strings.HasPrefix(strings.ToLower(line), "/cwd") {
		return "", false
	}
	rest := line[4:]
	if rest == "" {
		return "", false
	}
	if rest[0] != ' ' && rest[0] != '\t' {
		return "", false
	}
	return rest[1:], true
}

func completeInputLine(cwd, line string) (completed string, ok bool) {
	if _, isCwd := cwdArg(line); isCwd {
		return completeCwdLine(cwd, line)
	}
	if _, _, okAt := atMention(line); okAt {
		return completeAtLine(cwd, line)
	}
	return completeSlashLine(line)
}

func atMention(line string) (query string, at int, ok bool) {
	at = strings.LastIndexByte(line, '@')
	if at < 0 {
		return "", -1, false
	}
	if at > 0 {
		prev := line[at-1]
		if prev != ' ' && prev != '\t' && prev != '\n' {
			return "", -1, false
		}
	}
	rest := line[at+1:]
	if strings.ContainsAny(rest, " \t") {
		return "", -1, false
	}
	return rest, at, true
}

func completeAtLine(cwd, line string) (string, bool) {
	query, at, ok := atMention(line)
	if !ok {
		return line, false
	}
	next, matches := workspaceEntryMatches(cwd, query, false)
	if len(matches) == 0 {
		return line, false
	}
	out := line[:at+1] + next
	return out, out != line
}

func completeCwdLine(cwd, line string) (string, bool) {
	arg, ok := cwdArg(line)
	if !ok {
		return line, false
	}
	next, matches := cwdDirMatches(cwd, arg)
	if len(matches) == 0 {
		return line, false
	}
	out := "/cwd " + next
	return out, out != line
}

func cwdDirMatches(cwd, arg string) (completed string, names []string) {
	return workspaceEntryMatches(cwd, arg, true)
}

func workspaceEntryMatches(cwd, arg string, dirsOnly bool) (completed string, names []string) {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	typed := arg
	dirPart := ""
	prefix := typed
	listAbs := cwd
	if typed == "" {
		prefix = ""
	} else if strings.HasSuffix(typed, "/") {
		dirPart = typed
		prefix = ""
		listAbs = cwdJoin(cwd, typed)
	} else {
		sep := strings.LastIndexAny(typed, `/\`)
		if sep >= 0 {
			dirPart = typed[:sep+1]
			prefix = typed[sep+1:]
			listAbs = cwdJoin(cwd, dirPart)
		} else {
			prefix = typed
			listAbs = cwd
		}
	}
	ents, err := os.ReadDir(listAbs)
	if err != nil {
		return typed, nil
	}
	low := strings.ToLower(prefix)
	for _, e := range ents {
		name := e.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}
		if e.IsDir() {
			if low == "" || strings.HasPrefix(strings.ToLower(name), low) {
				names = append(names, name+"/")
			}
			continue
		}
		if dirsOnly {
			continue
		}
		if low == "" || strings.HasPrefix(strings.ToLower(name), low) {
			names = append(names, name)
		}
	}
	if dirsOnly && (low == "" || strings.HasPrefix("..", low)) {
		names = append([]string{"../"}, names...)
	}
	if len(names) == 0 {
		return typed, nil
	}
	if len(names) == 1 {
		return dirPart + names[0], names
	}
	lcp := names[0]
	for _, n := range names[1:] {
		lcp = commonPrefixFold(lcp, n)
	}
	if lcp == "" {
		return typed, names
	}
	return dirPart + lcp, names
}

func cwdJoin(cwd, p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "." {
		return cwd
	}
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			if p == "~" || strings.HasPrefix(p, "~/") {
				p = home + p[1:]
			}
		}
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Clean(filepath.Join(cwd, p))
}

func commonPrefixFold(a, b string) string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			break
		}
		i++
	}
	return a[:i]
}

func completeSlashLine(line string) (completed string, ok bool) {
	line = trimRightSpace(line)
	if line == "" || line[0] != '/' {
		return line, false
	}
	// Only complete the command token (no args yet).
	for i := 1; i < len(line); i++ {
		if line[i] == ' ' {
			return line, false
		}
	}
	matches := filterSlashCatalog(line)
	if len(matches) == 0 {
		return line, false
	}
	if len(matches) == 1 {
		return matches[0].Cmd + " ", true
	}
	// Longest common prefix among matches.
	lcp := matches[0].Cmd
	for _, m := range matches[1:] {
		lcp = commonPrefix(lcp, m.Cmd)
	}
	if lcp == line {
		return line, false
	}
	return lcp, true
}

func commonPrefix(a, b string) string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return a[:i]
}

func trimRightSpace(s string) string {
	i := len(s)
	for i > 0 {
		c := s[i-1]
		if c != ' ' && c != '\t' {
			break
		}
		i--
	}
	return s[:i]
}
