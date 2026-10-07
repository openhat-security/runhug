package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/adamsiwiec1/runhug/internal/config"
)

const persistMsgLimit = 400

type persistedChat struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Cwd        string     `json:"cwd"`
	Model      string     `json:"model"`
	BaseURL    string     `json:"base_url"`
	Tools      bool       `json:"tools"`
	Plan       bool       `json:"plan,omitempty"`
	Perm       string     `json:"perm,omitempty"`
	PermAlways []string   `json:"perm_always,omitempty"`
	PermNever  []string   `json:"perm_never,omitempty"`
	Updated    time.Time  `json:"updated"`
	Chat       []chatMsg  `json:"chat,omitempty"`
	Agent      []agentMsg `json:"agent,omitempty"`
}

func sessionsDir() (string, error) {
	d, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "sessions"), nil
}

func newSessionID() string {
	return time.Now().UTC().Format("20060102-150405") + "-" + fmt.Sprintf("%04x", time.Now().UnixNano()&0xffff)
}

func safeSessionID(id string) string {
	id = filepath.Base(strings.TrimSpace(id))
	if id == "" || id == "." || id == ".." {
		return ""
	}
	for _, r := range id {
		if r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		return ""
	}
	return id
}

func sessionPath(id string) (string, error) {
	id = safeSessionID(id)
	if id == "" {
		return "", fmt.Errorf("invalid session id")
	}
	dir, err := sessionsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, id+".json"), nil
}

func saveRunPersist(s *runSession) error {
	if s == nil {
		return nil
	}
	if len(s.Chat) == 0 && len(s.Agent) == 0 {
		return nil
	}
	if s.PersistID == "" {
		return nil
	}
	path, err := sessionPath(s.PersistID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	chat := s.Chat
	agent := s.Agent
	if len(chat) > persistMsgLimit {
		chat = chat[len(chat)-persistMsgLimit:]
	}
	if len(agent) > persistMsgLimit {
		agent = agent[len(agent)-persistMsgLimit:]
	}
	p := persistedChat{
		ID:         s.PersistID,
		Title:      sessionTitle(s),
		Cwd:        filepath.Clean(s.Cwd),
		Model:      s.Target.Model,
		BaseURL:    s.Target.BaseURL,
		Tools:      s.Tools,
		Plan:       s.Plan,
		Perm:       s.Perm,
		PermAlways: mapKeysTrue(s.PermAlways),
		PermNever:  mapKeysTrue(s.PermNever),
		Updated:    time.Now().UTC(),
		Chat:       chat,
		Agent:      agent,
	}
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func sessionTitle(s *runSession) string {
	if s != nil && strings.TrimSpace(s.Title) != "" {
		return strings.TrimSpace(s.Title)
	}
	for _, m := range s.Agent {
		if m.Role == "user" && strings.TrimSpace(m.Content) != "" {
			return truncateRunes(strings.Join(strings.Fields(m.Content), " "), 48)
		}
	}
	for _, m := range s.Chat {
		if m.Role == "user" && strings.TrimSpace(m.Content) != "" {
			return truncateRunes(strings.Join(strings.Fields(m.Content), " "), 48)
		}
	}
	return "new session"
}

func loadPersistedChat(id string) (*persistedChat, error) {
	path, err := sessionPath(id)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p persistedChat
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		p.ID = id
	}
	return &p, nil
}

func applyPersistedChat(s *runSession, p *persistedChat) {
	if s == nil || p == nil {
		return
	}
	s.PersistID = p.ID
	s.Title = p.Title
	s.Chat = append([]chatMsg{}, p.Chat...)
	s.Agent = append([]agentMsg{}, p.Agent...)
	s.Tools = p.Tools
	s.Plan = p.Plan
	if p.Perm != "" {
		s.Perm = normalizeRunPerm(p.Perm)
	}
	s.PermAlways = stringSet(p.PermAlways)
	s.PermNever = stringSet(p.PermNever)
	if len(s.Agent) == 0 && len(s.Chat) > 0 {
		s.Agent = chatAsAgent(s.Chat)
	}
	if len(s.Chat) == 0 && len(s.Agent) > 0 {
		s.Chat = agentAsChat(s.Agent)
	}
	if p.Cwd != "" {
		if st, err := os.Stat(p.Cwd); err == nil && st.IsDir() {
			s.Cwd = p.Cwd
		}
	}
}

func chatAsAgent(chat []chatMsg) []agentMsg {
	out := make([]agentMsg, 0, len(chat))
	for _, m := range chat {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		out = append(out, agentMsg{Role: m.Role, Content: m.Content})
	}
	return out
}

func agentAsChat(agent []agentMsg) []chatMsg {
	out := make([]chatMsg, 0, len(agent))
	for _, m := range agent {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		if m.Role == "assistant" && strings.TrimSpace(m.Content) == "" {
			continue
		}
		out = append(out, chatMsg{Role: m.Role, Content: m.Content})
	}
	return out
}

func sameCwd(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	aa, err1 := filepath.EvalSymlinks(a)
	bb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && aa == bb
}

func listPersistedChats() []persistedChat {
	dir, err := sessionsDir()
	if err != nil {
		return nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []persistedChat
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		p, err := loadPersistedChat(id)
		if err != nil {
			continue
		}
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Updated.After(out[j].Updated)
	})
	return out
}

func latestPersistedChat(cwd string) *persistedChat {
	all := listPersistedChats()
	pick := func(requireCwd bool) *persistedChat {
		for i := range all {
			if requireCwd && !sameCwd(all[i].Cwd, cwd) {
				continue
			}
			if len(all[i].Agent) == 0 && len(all[i].Chat) == 0 {
				continue
			}
			return &all[i]
		}
		return nil
	}
	if cwd != "" {
		if p := pick(true); p != nil {
			return p
		}
	}
	return pick(false)
}

func printSessionHistory(w io.Writer, s *runSession) {
	if s == nil || w == nil {
		return
	}
	msgs := s.Agent
	if len(msgs) == 0 {
		msgs = chatAsAgent(s.Chat)
	}
	if len(msgs) == 0 {
		return
	}
	fmt.Fprintln(w, dim("— conversation —"))
	for _, m := range msgs {
		switch m.Role {
		case "user":
			fmt.Fprintf(w, "%s %s\n", green("you>"), m.Content)
		case "assistant":
			for _, tc := range m.ToolCalls {
				printToolCall(w, tc.Function.Name, tc.Function.Arguments)
			}
			if t := strings.TrimSpace(m.Content); t != "" {
				fmt.Fprintf(w, "%s %s\n", cyan("assistant>"), t)
			}
		case "tool":
			body := strings.TrimSpace(m.Content)
			if body == "" || body == "(empty)" {
				continue
			}
			lines := strings.Split(body, "\n")
			const max = 12
			if len(lines) > max {
				body = strings.Join(lines[:max], "\n") + "\n…"
			}
			fmt.Fprintln(w, dim(body))
		}
	}
	fmt.Fprintln(w)
}

func printSessionList(w io.Writer, s *runSession) {
	all := listPersistedChats()
	if len(all) == 0 {
		fmt.Fprintln(w, dim("no sessions yet"))
		return
	}
	cur := ""
	if s != nil {
		cur = s.PersistID
	}
	fmt.Fprintln(w, bold("Sessions"))
	n := len(all)
	if n > 20 {
		n = 20
	}
	for _, p := range all[:n] {
		mark := " "
		if p.ID == cur {
			mark = "*"
		}
		when := p.Updated.Local().Format("01-02 15:04")
		title := p.Title
		if title == "" {
			title = "new session"
		}
		fmt.Fprintf(w, "  %s %s  %s  %s\n", cyan(mark), dim(when), p.ID, title)
	}
	fmt.Fprintln(w, dim("  /resume <id>  ·  /new"))
}

func mapKeysTrue(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func stringSet(keys []string) map[string]bool {
	if len(keys) == 0 {
		return nil
	}
	out := make(map[string]bool, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k != "" {
			out[k] = true
		}
	}
	return out
}
