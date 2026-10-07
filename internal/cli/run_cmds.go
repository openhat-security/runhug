package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/gcp"
	"github.com/adamsiwiec1/runhug/internal/jobs"
	"github.com/adamsiwiec1/runhug/internal/store"
)

// runSession carries chat target + optional registry model for slash commands.
type runSession struct {
	Target      EndpointTarget
	Model       store.Model // zero if --base-url only / unknown registry entry
	Stream      bool
	Tools       bool   // local agent tools (list/read/write/edit/bash)
	Plan        bool   // plan mode: read-only tools
	Perm        string // ask | allow | deny
	PermAlways  map[string]bool
	PermNever   map[string]bool
	RegistryKey string
	Cleanup     func()
	Cwd         string   // workspace root for tools
	PathFocus   string   // last path the user named (dump fallback)
	PathRoots   []string // all paths the user named; writes must stay under one of them
	Out         io.Writer
	Err         io.Writer

	PersistID string
	Chat      []chatMsg
	Agent     []agentMsg

	MetricsOn     bool // compact strip above the prompt
	ChatFull      bool // chat owns the terminal (alt screen)
	altScreen     bool // currently in alt screen
	TTY           io.Writer
	Transcript    *lineBuf
	fullW, fullH  int
	fullPromptRow int
	fullBannerRow int
	fullCwdRow    int
	fullModelRow  int
	fullInstRow   int
	metricsMu     sync.Mutex
	metricsCache  *hostMetrics
	metricsStop   chan struct{}
	LastClip      string // last error or copied selection (for /copy)
	Title         string
	undo          []chatSnap
	redo          []chatSnap
	Ctx           tokenUsage
	CtxLimit      int
}

type chatSnap struct {
	Chat  []chatMsg
	Agent []agentMsg
	Title string
}

func (s *runSession) out() io.Writer {
	if s != nil && s.Out != nil {
		return s.Out
	}
	return os.Stdout
}

func (s *runSession) errw() io.Writer {
	if s != nil && s.Err != nil {
		return s.Err
	}
	return os.Stderr
}

func (s *runSession) tty() io.Writer {
	if s != nil && s.TTY != nil {
		return s.TTY
	}
	if s != nil && s.Out != nil {
		if c, ok := s.Out.(*captureWriter); ok && c.w != nil {
			return c.w
		}
		return s.Out
	}
	return os.Stdout
}

func (s *runSession) setChatFull(on bool) {
	if s == nil {
		return
	}
	if on {
		s.drawFullChrome()
		return
	}
	s.leaveAltScreen()
	if !s.MetricsOn {
		s.stopMetricsPump()
	}
	printRunStatusStrip(s.tty(), s)
	s.replayTranscript(s.tty())
	fmt.Fprintln(s.tty(), dim("chat minimized  ·  /full takes over the terminal"))
	fmt.Fprintln(s.tty())
}

func (s *runSession) leaveAltScreen() {
	if s == nil || !s.altScreen {
		return
	}
	fmt.Fprint(s.tty(), "\033[r\033[?25h\033[?1049l")
	s.altScreen = false
	s.ChatFull = false
}

func printRunStatusStrip(w io.Writer, s *runSession) {
	backend := "unknown"
	detail := ""
	if s.Model.HFRepo != "" {
		backend = s.Model.Kind()
		switch s.Model.Kind() {
		case store.BackendGCP:
			detail = s.Model.PodID
			if detail == "" {
				detail = s.Model.EndpointID
			}
		case store.BackendLocal:
			detail = s.Model.Runtime
		default:
			detail = s.Model.EndpointID
		}
	} else if strings.HasPrefix(s.Target.Source, "gcp") {
		backend = "gcp"
	} else if strings.HasPrefix(s.Target.Source, "runpod") {
		backend = "runpod"
	} else if strings.HasPrefix(s.Target.Source, "local") {
		backend = "local"
	}
	port := ""
	if u := s.Target.BaseURL; u != "" {
		if p := localPortFromBaseURL(u); p > 0 {
			port = fmt.Sprintf(":%d", p)
		}
	}
	fmt.Fprintf(w, "%s  %s", dim("status"), colorBackend(padRight(backend, 8), backend))
	if detail != "" {
		fmt.Fprintf(w, "  %s", cyan(truncateRunes(detail, 36)))
	}
	if port != "" {
		fmt.Fprintf(w, "  tunnel %s", dim(port))
	}
	tools := "tools off"
	if s.Tools {
		tools = "tools on"
	}
	mode := "agent"
	if s != nil && s.Plan {
		mode = "plan"
	}
	perm := "ask"
	if s != nil && s.Perm != "" {
		perm = s.Perm
	}
	fmt.Fprintf(w, "  %s  %s  %s  %s\n", dim("("+tools+")"), dim(mode), dim("perm "+perm), dim("type / for commands · Tab complete · ↑ history · Ctrl-C cancel/clear"))
	if s != nil {
		fmt.Fprintln(w, "  "+formatContextMeter(s))
	}
	if s != nil && s.MetricsOn {
		if m := s.cachedMetrics(); m != nil {
			printMetricsMini(w, *m)
		} else {
			fmt.Fprintln(w, dim("metrics  fetching…  /metrics off to hide"))
		}
	}
	fmt.Fprintln(w)
}

// handleRunSlash returns (handled, exit, error).
// handled=false means the line is a normal user chat message.
// clearHistory clears conversation state (chat + agent messages).
func handleRunSlash(s *runSession, line string, clearHistory func()) (handled, exit bool, err error) {
	if !strings.HasPrefix(line, "/") {
		return false, false, nil
	}
	fields := strings.Fields(line)
	cmd := strings.ToLower(fields[0])
	switch cmd {
	case "/exit", "/quit":
		return true, true, nil
	case "/copy":
		text := strings.TrimSpace(s.LastClip)
		if text == "" {
			text = lastAssistantText(s)
		}
		if text == "" {
			fmt.Fprintln(s.out(), dim("nothing to copy"))
			return true, false, nil
		}
		if err := copyToClipboard(text); err != nil {
			return true, false, err
		}
		fmt.Fprintln(s.out(), dim("copied "+strconv.Itoa(len(text))+" chars"))
		return true, false, nil
	case "/help":
		printRunHelp(s.out(), s)
		return true, false, nil
	case "/clear":
		if clearHistory != nil {
			clearHistory()
		}
		s.PathFocus = ""
		s.PathRoots = nil
		_ = saveRunPersist(s)
		fmt.Fprintln(s.out(), dim("history cleared"))
		return true, false, nil
	case "/sessions":
		printSessionList(s.out(), s)
		return true, false, nil
	case "/new":
		s.Chat = nil
		s.Agent = nil
		s.undo = nil
		s.redo = nil
		s.PathFocus = ""
		s.PathRoots = nil
		s.PersistID = newSessionID()
		_ = saveRunPersist(s)
		printKV(s.out(), "session", s.PersistID)
		return true, false, nil
	case "/resume", "/session":
		id := ""
		if len(fields) > 1 {
			id = fields[1]
		}
		var p *persistedChat
		var err error
		if id == "" {
			p = latestPersistedChat(s.Cwd)
			if p == nil {
				return true, false, fmt.Errorf("no sessions to resume")
			}
		} else {
			p, err = loadPersistedChat(id)
			if err != nil {
				return true, false, err
			}
		}
		applyPersistedChat(s, p)
		fmt.Fprintf(s.out(), "%s resumed %s (%d messages)\n", dim("session"), cyan(s.PersistID), len(s.Agent)+len(s.Chat))
		printSessionHistory(s.out(), s)
		return true, false, nil
	case "/model":
		if len(fields) == 1 {
			printKV(s.out(), "model", bold(s.Target.Model))
			return true, false, nil
		}
		s.Target.Model = strings.TrimSpace(strings.Join(fields[1:], " "))
		printKV(s.out(), "model", bold(s.Target.Model))
		return true, false, nil
	case "/plan":
		s.Plan = true
		saveRunModeSettings(s)
		printKV(s.out(), "mode", bold("plan")+"  "+dim("read-only · /agent to implement"))
		return true, false, nil
	case "/agent":
		s.Plan = false
		saveRunModeSettings(s)
		printKV(s.out(), "mode", bold("agent")+"  "+dim("tools · perm "+normalizeRunPerm(s.Perm)))
		return true, false, nil
	case "/perm":
		if len(fields) == 1 {
			printKV(s.out(), "perm", normalizeRunPerm(s.Perm))
			return true, false, nil
		}
		p := normalizeRunPerm(fields[1])
		if p != "ask" && p != "allow" && p != "deny" {
			return true, false, fmt.Errorf("usage: /perm ask|allow|deny")
		}
		s.Perm = p
		saveRunModeSettings(s)
		printKV(s.out(), "perm", bold(s.Perm))
		return true, false, nil
	case "/settings":
		return true, false, runSettingsMenu(s)
	case "/status":
		return true, false, runSlashStatus(s)
	case "/metrics":
		return handleMetricsSlash(s, fields[1:])
	case "/full", "/fullscreen":
		s.setChatFull(true)
		return true, false, nil
	case "/mini", "/minimize":
		s.setChatFull(false)
		return true, false, nil
	case "/logs":
		n := 80
		if len(fields) > 1 {
			if v, e := strconv.Atoi(fields[1]); e == nil && v > 0 {
				n = v
			}
		}
		return true, false, runSlashLogs(s, n)
	case "/tools":
		if len(fields) == 1 {
			printRunToolsHelp(s.out(), s)
			return true, false, nil
		}
		switch strings.ToLower(fields[1]) {
		case "on", "true", "1":
			s.Tools = true
		case "off", "false", "0":
			s.Tools = false
		default:
			return true, false, fmt.Errorf("usage: /tools on|off")
		}
		printRunToolsHelp(s.out(), s)
		return true, false, nil
	case "/cwd":
		if len(fields) == 1 {
			printKV(s.out(), "cwd", s.Cwd)
			return true, false, nil
		}
		path := strings.TrimSpace(strings.Join(fields[1:], " "))
		cand := path
		if !filepath.IsAbs(cand) {
			cand = filepath.Join(s.Cwd, cand)
		}
		abs, err := filepath.Abs(cand)
		if err != nil {
			return true, false, err
		}
		st, err := os.Stat(abs)
		if err != nil || !st.IsDir() {
			return true, false, fmt.Errorf("not a directory: %s", path)
		}
		s.Cwd = abs
		printKV(s.out(), "cwd", s.Cwd)
		return true, false, nil
	case "/undo":
		if err := undoRunTurn(s); err != nil {
			return true, false, err
		}
		fmt.Fprintln(s.out(), dim("undid last turn  ·  /redo to restore"))
		_ = saveRunPersist(s)
		return true, false, nil
	case "/redo":
		if err := redoRunTurn(s); err != nil {
			return true, false, err
		}
		fmt.Fprintln(s.out(), dim("redid turn"))
		_ = saveRunPersist(s)
		return true, false, nil
	case "/compact":
		if err := compactRunSession(s); err != nil {
			return true, false, err
		}
		_ = saveRunPersist(s)
		return true, false, nil
	case "/context":
		_, err := handleContextSlash(s, fields)
		return true, false, err
	case "/gpu":
		return true, false, handleGPUSlash(s, fields)
	case "/export":
		path := ""
		if len(fields) > 1 {
			path = strings.Join(fields[1:], " ")
		}
		outPath, err := exportRunSession(s, path)
		if err != nil {
			return true, false, err
		}
		printKV(s.out(), "export", cyan(outPath))
		return true, false, nil
	case "/rename":
		if len(fields) < 2 {
			printKV(s.out(), "title", sessionTitle(s))
			return true, false, nil
		}
		s.Title = strings.TrimSpace(strings.Join(fields[1:], " "))
		_ = saveRunPersist(s)
		printKV(s.out(), "title", s.Title)
		return true, false, nil
	default:
		fmt.Fprintf(s.errw(), "%s unknown command %s — try /help\n", yellow("⚠"), cmd)
		return true, false, nil
	}
}

func printRunHelp(w io.Writer, s *runSession) {
	fmt.Fprintln(w, bold("Chat commands"))
	for _, x := range runSlashCatalog() {
		label := x.Cmd
		if x.Args != "" {
			label += " " + x.Args
		}
		fmt.Fprintf(w, "  %s  %s\n", cyan(padRight(label, 32)), dim(x.Desc))
	}
	fmt.Fprintln(w)
	printRunToolsHelp(w, s)
	fmt.Fprintln(w, dim("Keys: Tab complete · type @path then Tab · ↑/↓ history · wheel / PgUp PgDn scroll (full) · drag to copy · /copy last error · Ctrl-V paste · Ctrl-C cancel / clear · Ctrl-D / /exit quit"))
	fmt.Fprintln(w, dim("Layout: /full takes over the terminal · /mini restores scrollback"))
	fmt.Fprintln(w)
}

func printRunToolsHelp(w io.Writer, s *runSession) {
	state := "off"
	cwd := ""
	if s != nil {
		if s.Tools {
			state = "on"
		}
		cwd = s.Cwd
	}
	fmt.Fprintln(w, bold("Agent tools"))
	fmt.Fprintf(w, "  %s  %s\n", cyan(padRight("status", 12)), bold(state))
	if cwd != "" {
		fmt.Fprintf(w, "  %s  %s\n", cyan(padRight("cwd", 12)), cwd)
	}
	fmt.Fprintln(w, dim("  When on, the model may call these on this machine (paths stay inside cwd):"))
	type row struct{ name, desc string }
	for _, r := range []row{
		{"list_dir", "list files in a folder"},
		{"read_file", "read a text file"},
		{"write_file", "create or overwrite a file (small chunks)"},
		{"append_file", "append more text to a file"},
		{"edit_file", "replace an exact string in a file"},
		{"glob", "find files by glob pattern"},
		{"bash", "run a shell command in cwd"},
	} {
		fmt.Fprintf(w, "  %s  %s\n", cyan(padRight(r.name, 12)), dim(r.desc))
	}
	fmt.Fprintln(w, dim("  /tools on  allow calls · /tools off  plain chat, no file/shell access"))
	fmt.Fprintln(w)
}

func runSlashStatus(s *runSession) error {
	w := s.out()
	heading(w, "Status")
	printKV(w, "base_url", cyan(s.Target.BaseURL))
	printKV(w, "model", bold(s.Target.Model))
	printKV(w, "source", s.Target.Source)
	mode := "agent"
	if s.Plan {
		mode = "plan"
	}
	printKV(w, "mode", mode)
	printKV(w, "perm", normalizeRunPerm(s.Perm))
	printKV(w, "context", formatContextMeter(s))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := probeOpenAIModels(ctx, s.Target.BaseURL, s.Target.APIKey); err != nil {
		printKV(w, "openai", yellow("unreachable — "+err.Error()))
	} else {
		printKV(w, "openai", green("ok"))
	}
	switch s.Model.Kind() {
	case store.BackendGCP:
		if s.Model.PodID == "" {
			break
		}
		client := gcp.NewClient()
		st, err := client.DescribeInstance(ctx, s.Model.EndpointID, s.Model.EndpointType, s.Model.PodID)
		if err != nil {
			printKV(w, "instance", yellow(err.Error()))
		} else {
			printKV(w, "instance", fmt.Sprintf("%s  %s", bold(st.Name), cyan(st.Status)))
		}
	case store.BackendLocal:
		printKV(w, "runtime", dash(s.Model.Runtime))
	case store.BackendRunpod:
		env := config.Load()
		if env.RunpodAPIKey != "" && s.Model.EndpointID != "" {
			h, err := jobs.Health(env.RunpodAPIKey, s.Model.EndpointID)
			if err != nil {
				printKV(w, "workers", yellow(err.Error()))
			} else if h != nil && h.Workers != nil {
				wr := h.Workers
				printKV(w, "workers", fmt.Sprintf("ready=%s running=%s initializing=%s",
					itoaPtr(wr.Ready), itoaPtr(wr.Running), itoaPtr(wr.Initializing)))
			}
		}
	}
	fmt.Fprintln(w)
	return nil
}

func runSlashLogs(s *runSession, lines int) error {
	w := s.out()
	heading(w, "Logs")
	if s.Model.Kind() != store.BackendGCP || s.Model.PodID == "" {
		fmt.Fprintln(w, dim("guest logs require a GCP registry model — use the provider dashboard for Runpod/local"))
		fmt.Fprintln(w)
		return nil
	}
	out, err := gcpGuest(s, gcp.GuestLogsCommand(lines))
	if err != nil {
		return err
	}
	fmt.Fprintln(w, out)
	fmt.Fprintln(w)
	return nil
}

func gcpGuest(s *runSession, command string) (string, error) {
	m := s.Model
	if m.EndpointID == "" || m.EndpointType == "" || m.PodID == "" {
		return "", fmt.Errorf("gcp registry entry incomplete (project/zone/instance)")
	}
	client := gcp.NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return client.GuestExec(ctx, gcp.GuestOpts{
		Project:  m.EndpointID,
		Zone:     m.EndpointType,
		Instance: m.PodID,
		Command:  command,
		Timeout:  55 * time.Second,
	})
}

// parseRunSlash is exported for tests: returns command name and args.
func parseRunSlash(line string) (cmd string, args []string, ok bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "/") {
		return "", nil, false
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", nil, false
	}
	return strings.ToLower(fields[0]), fields[1:], true
}

func lastAssistantText(s *runSession) string {
	if s == nil {
		return ""
	}
	for i := len(s.Agent) - 1; i >= 0; i-- {
		if s.Agent[i].Role == "assistant" && strings.TrimSpace(s.Agent[i].Content) != "" {
			return s.Agent[i].Content
		}
	}
	for i := len(s.Chat) - 1; i >= 0; i-- {
		if s.Chat[i].Role == "assistant" && strings.TrimSpace(s.Chat[i].Content) != "" {
			return s.Chat[i].Content
		}
	}
	return ""
}

func (s *runSession) noteError(err error) {
	if s == nil || err == nil {
		return
	}
	msg := err.Error()
	s.LastClip = msg
	fmt.Fprintln(s.out(), FormatError(msg))
	if copyErr := copyToClipboard(msg); copyErr == nil {
		fmt.Fprintln(s.out(), dim("copied error to clipboard  ·  /copy"))
	} else {
		fmt.Fprintln(s.out(), dim("/copy to retry clipboard"))
	}
}

func looksLikeEndpointDown(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, n := range []string{
		"connection refused",
		"connection reset",
		"broken pipe",
		"no route to host",
		"network is unreachable",
	} {
		if strings.Contains(s, n) {
			return true
		}
	}
	if strings.Contains(s, "i/o timeout") && strings.Contains(s, "127.0.0.1") {
		return true
	}
	if strings.Contains(s, "http ") && (strings.Contains(s, "502") || strings.Contains(s, "503") || strings.Contains(s, "504")) {
		return true
	}
	return false
}

func popLastUserTurn(msgs *[]agentMsg, line string) {
	if msgs == nil || len(*msgs) == 0 {
		return
	}
	line = strings.TrimSpace(line)
	for i := len(*msgs) - 1; i >= 0; i-- {
		if (*msgs)[i].Role != "user" {
			continue
		}
		if line == "" || strings.HasPrefix((*msgs)[i].Content, line) {
			*msgs = (*msgs)[:i]
		}
		return
	}
}

func cloneChat(in []chatMsg) []chatMsg {
	if len(in) == 0 {
		return nil
	}
	out := make([]chatMsg, len(in))
	copy(out, in)
	return out
}

func cloneAgent(in []agentMsg) []agentMsg {
	if len(in) == 0 {
		return nil
	}
	out := make([]agentMsg, len(in))
	copy(out, in)
	return out
}

func (s *runSession) snapshot() chatSnap {
	if s == nil {
		return chatSnap{}
	}
	return chatSnap{Chat: cloneChat(s.Chat), Agent: cloneAgent(s.Agent), Title: s.Title}
}

func (s *runSession) restore(sn chatSnap) {
	if s == nil {
		return
	}
	s.Chat = cloneChat(sn.Chat)
	s.Agent = cloneAgent(sn.Agent)
	s.Title = sn.Title
}

func (s *runSession) pushUndo() {
	if s == nil {
		return
	}
	s.undo = append(s.undo, s.snapshot())
	if len(s.undo) > 8 {
		s.undo = s.undo[len(s.undo)-8:]
	}
	s.redo = nil
}

func lastUserIndex(agent []agentMsg, chat []chatMsg) int {
	for i := len(agent) - 1; i >= 0; i-- {
		if agent[i].Role == "user" && !strings.HasPrefix(agent[i].Content, "[runhug]") {
			return i
		}
	}
	for i := len(chat) - 1; i >= 0; i-- {
		if chat[i].Role == "user" {
			return i
		}
	}
	return -1
}

func undoRunTurn(s *runSession) error {
	if s == nil {
		return fmt.Errorf("no session")
	}
	if len(s.undo) > 0 {
		cur := s.snapshot()
		sn := s.undo[len(s.undo)-1]
		s.undo = s.undo[:len(s.undo)-1]
		s.redo = append(s.redo, cur)
		s.restore(sn)
		return nil
	}
	if lastUserIndex(s.Agent, s.Chat) < 0 {
		return fmt.Errorf("nothing to undo")
	}
	cur := s.snapshot()
	if len(s.Agent) > 0 {
		i := lastUserIndex(s.Agent, nil)
		if i < 0 {
			return fmt.Errorf("nothing to undo")
		}
		s.Agent = s.Agent[:i]
		s.Chat = agentAsChat(s.Agent)
	} else {
		i := lastUserIndex(nil, s.Chat)
		if i < 0 {
			return fmt.Errorf("nothing to undo")
		}
		s.Chat = s.Chat[:i]
	}
	s.redo = append(s.redo, cur)
	return nil
}

func redoRunTurn(s *runSession) error {
	if s == nil || len(s.redo) == 0 {
		return fmt.Errorf("nothing to redo")
	}
	cur := s.snapshot()
	sn := s.redo[len(s.redo)-1]
	s.redo = s.redo[:len(s.redo)-1]
	s.undo = append(s.undo, cur)
	s.restore(sn)
	return nil
}

func serializeForCompact(s *runSession) string {
	var b strings.Builder
	msgs := s.Agent
	if len(msgs) == 0 {
		for _, m := range s.Chat {
			msgs = append(msgs, agentMsg{Role: m.Role, Content: m.Content})
		}
	}
	for _, m := range msgs {
		role := m.Role
		if role == "" {
			continue
		}
		body := m.Content
		if m.Role == "tool" || m.Role == "assistant" {
			rs := []rune(body)
			if len(rs) > 800 {
				body = string(rs[:800]) + "…"
			}
		}
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			var names []string
			for _, tc := range m.ToolCalls {
				names = append(names, tc.Function.Name)
			}
			body = strings.TrimSpace(body + "\ntools: " + strings.Join(names, ", "))
		}
		fmt.Fprintf(&b, "[%s] %s\n", role, body)
		if b.Len() > 24_000 {
			b.WriteString("\n…\n")
			break
		}
	}
	return b.String()
}

func compactRunSession(s *runSession) error {
	if s == nil {
		return fmt.Errorf("no session")
	}
	dump := strings.TrimSpace(serializeForCompact(s))
	if dump == "" {
		return fmt.Errorf("nothing to compact")
	}
	s.pushUndo()
	fmt.Fprint(s.out(), dim("compacting…"))
	_ = syncWriter(s.out())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	req := []agentMsg{
		{Role: "system", Content: "Summarize this coding chat so work can continue in a small context window. Keep file paths, decisions, bugs, and next steps. No tools. Under 40 lines."},
		{Role: "user", Content: dump},
	}
	msg, _, err := s.completeAgent(ctx, req, false, false, true, s.out())
	if err != nil {
		s.restore(s.undo[len(s.undo)-1])
		s.undo = s.undo[:len(s.undo)-1]
		return err
	}
	clearThinkingOn(s.out())
	sum := strings.TrimSpace(msg.Content)
	if sum == "" {
		s.restore(s.undo[len(s.undo)-1])
		s.undo = s.undo[:len(s.undo)-1]
		return fmt.Errorf("empty compact summary")
	}
	body := "[compacted session — /undo restores full history]\n" + sum
	s.Agent = []agentMsg{{Role: "user", Content: body}}
	s.Chat = []chatMsg{{Role: "user", Content: body}}
	fmt.Fprintln(s.out())
	fmt.Fprintln(s.out(), dim("compacted  ·  /undo restores the full transcript"))
	return nil
}

func exportRunSession(s *runSession, dest string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("no session")
	}
	dest = strings.TrimSpace(dest)
	if dest == "" {
		id := s.PersistID
		if id == "" {
			id = "chat"
		}
		dest = filepath.Join(s.Cwd, "runhug-"+id+".md")
	} else if !filepath.IsAbs(dest) {
		dest = filepath.Join(s.Cwd, dest)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", sessionTitle(s))
	fmt.Fprintf(&b, "- model: %s\n- session: %s\n- cwd: %s\n\n", s.Target.Model, s.PersistID, s.Cwd)
	msgs := s.Agent
	if len(msgs) == 0 {
		for _, m := range s.Chat {
			msgs = append(msgs, agentMsg{Role: m.Role, Content: m.Content})
		}
	}
	for _, m := range msgs {
		if m.Role == "tool" {
			fmt.Fprintf(&b, "### tool %s\n\n```\n%s\n```\n\n", m.Name, strings.TrimSpace(m.Content))
			continue
		}
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", m.Role, strings.TrimSpace(m.Content))
	}
	if err := os.WriteFile(dest, []byte(b.String()), 0o600); err != nil {
		return "", err
	}
	return dest, nil
}

func (s *runSession) canWake() bool {
	if s == nil {
		return false
	}
	switch s.Model.Kind() {
	case store.BackendGCP, store.BackendRunpod:
		return strings.TrimSpace(s.Model.HFRepo) != ""
	default:
		return false
	}
}

func (s *runSession) wakeEndpoint() error {
	if s == nil || !s.canWake() {
		return fmt.Errorf("no cloud instance to wake")
	}
	fmt.Fprintln(s.out())
	fmt.Fprintln(s.out(), yellow("backend unreachable")+"  "+dim("idle stop or tunnel died — waking instance…"))
	if s.Cleanup != nil {
		s.Cleanup()
		s.Cleanup = func() {}
	}
	h, err := EnsureReady(context.Background(), s.Model, EnsureOpts{
		ServeModel: s.Target.Model,
		Writer:     s.out(),
	})
	if err != nil {
		return err
	}
	s.Target = h.Target
	if h.Cleanup != nil {
		s.Cleanup = h.Cleanup
	}
	fmt.Fprintln(s.out(), green("✓")+"  "+dim("instance ready — retrying your message"))
	fmt.Fprintln(s.out())
	return nil
}

func (s *runSession) runTurnWithWake(tools bool, line string) error {
	run := func() error {
		return withTurnCancel(context.Background(), func(ctx context.Context) error {
			if tools {
				return runAgentTurn(ctx, s, &s.Agent, line)
			}
			reply, usage, err := chatCompletions(ctx, s.Target, s.Chat, s.Stream, s.out())
			if err != nil {
				return err
			}
			s.noteUsage(usage)
			s.Chat = append(s.Chat, chatMsg{Role: "assistant", Content: reply})
			printContextUsage(s.out(), s)
			return nil
		})
	}
	err := run()
	if errors.Is(err, errTurnCanceled) || !looksLikeEndpointDown(err) || !s.canWake() {
		return err
	}
	if tools {
		popLastUserTurn(&s.Agent, line)
	}
	if werr := s.wakeEndpoint(); werr != nil {
		return fmt.Errorf("%v\n%w", err, werr)
	}
	if !tools {
		fmt.Fprint(s.out(), dim("thinking…"))
		_ = syncWriter(s.out())
	}
	return run()
}
