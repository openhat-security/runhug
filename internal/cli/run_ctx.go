package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/gcp"
	"github.com/adamsiwiec1/runhug/internal/store"
)

// Default llama.cpp -c from GCP startup (ImageConfig). Override with /context N.
const defaultContextLimit = 8192

type tokenUsage struct {
	Prompt     int `json:"prompt_tokens"`
	Completion int `json:"completion_tokens"`
	Total      int `json:"total_tokens"`
}

func (u tokenUsage) ok() bool {
	return u.Total > 0 || u.Prompt > 0 || u.Completion > 0
}

func (u tokenUsage) total() int {
	if u.Total > 0 {
		return u.Total
	}
	return u.Prompt + u.Completion
}

func parseTokenUsage(raw json.RawMessage) tokenUsage {
	if len(raw) == 0 || string(raw) == "null" {
		return tokenUsage{}
	}
	var u tokenUsage
	if json.Unmarshal(raw, &u) != nil {
		return tokenUsage{}
	}
	return u
}

func (s *runSession) noteUsage(u tokenUsage) {
	if s == nil || !u.ok() {
		return
	}
	s.Ctx = u
	if s.CtxLimit <= 0 {
		s.CtxLimit = defaultContextLimit
	}
}

func (s *runSession) ctxWindow() int {
	if s != nil && s.CtxLimit > 0 {
		return s.CtxLimit
	}
	return defaultContextLimit
}

func (s *runSession) ctxUsed() int {
	if s == nil {
		return 0
	}
	if s.Ctx.ok() {
		return s.Ctx.total()
	}
	return estimateSessionTokens(s)
}

func estimateSessionTokens(s *runSession) int {
	if s == nil {
		return 0
	}
	n := len(agentSystemPrompt) / 4
	src := s.Agent
	if len(src) == 0 {
		for _, m := range s.Chat {
			n += (len(m.Role) + len(m.Content) + 8) / 4
		}
		return n
	}
	for _, m := range src {
		n += (len(m.Role) + len(m.Content) + 8) / 4
		for _, tc := range m.ToolCalls {
			n += (len(tc.Function.Name) + len(tc.Function.Arguments) + 16) / 4
		}
	}
	return n
}

func formatTokenCount(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 10_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%dk", n/1000)
}

func contextPct(used, limit int) int {
	if limit <= 0 {
		return 0
	}
	p := used * 100 / limit
	if p > 100 {
		return 100
	}
	if p < 0 {
		return 0
	}
	return p
}

func contextBar(used, limit int, width int) string {
	if width < 4 {
		width = 4
	}
	pct := contextPct(used, limit)
	fill := (pct*width + 50) / 100
	if fill > width {
		fill = width
	}
	if pct > 0 && fill == 0 {
		fill = 1
	}
	bar := strings.Repeat("█", fill) + strings.Repeat("░", width-fill)
	switch {
	case pct >= 90:
		return red(bar)
	case pct >= 70:
		return yellow(bar)
	default:
		return cyan(bar)
	}
}

func formatContextMeter(s *runSession) string {
	used, limit := s.ctxUsed(), s.ctxWindow()
	pct := contextPct(used, limit)
	src := "est"
	if s != nil && s.Ctx.ok() {
		src = "api"
	}
	return fmt.Sprintf("%s  %s  %d%%  %s/%s  %s",
		dim("ctx"),
		contextBar(used, limit, 10),
		pct,
		formatTokenCount(used),
		formatTokenCount(limit),
		dim(src),
	)
}

func printContextUsage(w io.Writer, s *runSession) {
	if w == nil || s == nil {
		return
	}
	fmt.Fprintln(w, formatContextMeter(s))
	if s.Ctx.ok() && (s.Ctx.Prompt > 0 || s.Ctx.Completion > 0) {
		fmt.Fprintf(w, "  %s in %s  out %s\n",
			dim("tokens"),
			formatTokenCount(s.Ctx.Prompt),
			formatTokenCount(s.Ctx.Completion),
		)
	}
	if contextPct(s.ctxUsed(), s.ctxWindow()) >= 85 {
		fmt.Fprintln(w, dim("  context high — /context up  or  /compact"))
	}
}

var contextSteps = []int{8192, 16384, 32768, 65536, 131072}

func nextContextWindow(limit, used int) int {
	need := limit
	if used > need {
		need = used
	}
	for _, step := range contextSteps {
		if step > limit && step >= need {
			return step
		}
	}
	n := ((need + 1023) / 1024) * 1024
	if n <= limit {
		n = limit * 2
	}
	if n < 512 {
		n = 8192
	}
	return n
}

func parseContextArg(s *runSession, arg string) (int, error) {
	arg = strings.TrimSpace(strings.ToLower(arg))
	switch arg {
	case "", "show":
		return 0, nil
	case "up", "increase", "+", "more":
		return nextContextWindow(s.ctxWindow(), s.ctxUsed()), nil
	}
	var n int
	if _, err := fmt.Sscanf(arg, "%d", &n); err != nil || n < 512 {
		return 0, fmt.Errorf("usage: /context [up|8192|32768|65536]")
	}
	return n, nil
}

func handleContextSlash(s *runSession, fields []string) (handled bool, err error) {
	if s == nil {
		return true, fmt.Errorf("no session")
	}
	arg := ""
	if len(fields) > 1 {
		arg = fields[1]
	}
	n, err := parseContextArg(s, arg)
	if err != nil {
		return true, err
	}
	if n > 0 {
		prev := s.ctxWindow()
		s.CtxLimit = n
		printKV(s.out(), "context", fmt.Sprintf("%s → %s", formatTokenCount(prev), formatTokenCount(n)))
		if err := applyLiveContext(s, n); err != nil {
			return true, err
		}
	}
	printContextUsage(s.out(), s)
	return true, nil
}

func applyLiveContext(s *runSession, n int) error {
	if s == nil {
		return fmt.Errorf("no session")
	}
	if s.Model.Kind() != store.BackendGCP {
		fmt.Fprintln(s.out(), dim("meter updated — llama -c only restarts on a GCP Spot VM"))
		return nil
	}
	project := strings.TrimSpace(s.Model.EndpointID)
	zone := strings.TrimSpace(s.Model.EndpointType)
	name := strings.TrimSpace(s.Model.PodID)
	if project == "" || zone == "" || name == "" {
		return fmt.Errorf("GCP row needs project, zone, instance")
	}
	fmt.Fprintf(s.out(), "%s restarting llama.cpp with -c %d (GGUF reload)…\n", dim("context"), n)
	client := gcp.NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	out, err := client.GuestExec(ctx, gcp.GuestOpts{
		Project:  project,
		Zone:     zone,
		Instance: name,
		Command:  gcp.GuestSetContextCommand(n),
		Timeout:  90 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("guest context: %w", err)
	}
	if strings.Contains(out, "NOCONTAINER") {
		return fmt.Errorf("llama container missing — try /status then retry")
	}
	printKV(s.out(), "guest", dim(strings.TrimSpace(out)))
	printEnsureStep(s.out(), 1, 1, "openai", "waiting for llama.cpp after context change")
	if err := waitOpenAIReady(ctx, s.Target.BaseURL, s.Target.APIKey, 3*time.Second, remainingOr(ctx, 10*time.Minute), s.out(), 1, 1); err != nil {
		return fmt.Errorf("llama not ready after -c %d: %w", n, err)
	}
	fmt.Fprintln(s.out(), green("✓")+"  "+dim("context applied"))
	return nil
}
