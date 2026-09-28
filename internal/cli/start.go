package cli

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/adamsiwiec1/runhug/internal/bridge"
	"github.com/adamsiwiec1/runhug/internal/version"
)

const (
	claudeSettingsOverlay = `{"env":{"CLAUDE_CODE_ATTRIBUTION_HEADER":"0"}}`
	claudeAuthTokenDummy  = "sk-runhug-local"
)

// Keys scrubbed from the Claude child env so it does not prompt for Anthropic login.
var claudeEnvUnset = []string{"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN"}

func cmdStart(args []string) error {
	fs := newFlagSet("start")
	baseURL := fs.String("base-url", "", "OpenAI-compatible base URL (…/v1)")
	apiKeyEnv := fs.String("api-key-env", "", "env var holding the API key")
	serveModel := fs.String("model", "", "served model name hint for the agent")
	modelKey := fs.String("endpoint", "", "registry model / endpoint id (alias for positional model)")
	noLaunch := fs.Bool("no-launch", false, "print env/command only; do not exec the agent")
	yes := fs.Bool("yes", false, "non-interactive: auto-pick when exactly one registry/remote model")
	bridgePort := fs.Int("bridge-port", 0, "local Anthropic→OpenAI bridge port (0 = ephemeral)")
	dryRun := fs.Bool("dry-run", false, "print the opencode config JSON only (opencode)")
	cfgPath := fs.String("config", "", "opencode config path (default ~/.config/opencode/opencode.json)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: %s start <agent> [model]\nAgents: claude, opencode (codex stub)\nExample: %s start opencode org/model", version.Name, version.Name)
	}
	agent := strings.ToLower(strings.TrimSpace(fs.Arg(0)))
	registryKey := strings.TrimSpace(*modelKey)
	if fs.NArg() > 1 {
		registryKey = fs.Arg(1)
	}
	skipPrompt := *yes || *noLaunch || !canPrompt()

	switch agent {
	case "claude":
		return startClaude(registryKey, *baseURL, *apiKeyEnv, *serveModel, *bridgePort, *noLaunch, skipPrompt)
	case "opencode":
		return startOpenCode(registryKey, *baseURL, *apiKeyEnv, *serveModel, *noLaunch, *dryRun, *cfgPath, skipPrompt)
	case "codex":
		return startCodexStub(registryKey, *baseURL, *apiKeyEnv, *serveModel, *noLaunch, skipPrompt)
	default:
		return fmt.Errorf("unknown agent %q (supported: claude, opencode; stub: codex)", agent)
	}
}

func startClaude(registryKey, baseURL, apiKeyEnv, serveModel string, bridgePort int, noLaunch, skipPrompt bool) error {
	var err error
	registryKey, serveModel, err = pickStartModel(registryKey, baseURL, serveModel, apiKeyEnv, skipPrompt)
	if err != nil {
		return err
	}
	target, err := ResolveEndpoint(registryKey, baseURL, apiKeyEnv, serveModel)
	if err != nil {
		return err
	}
	target.BaseURL = NormalizeOpenAIBase(target.BaseURL)

	authToken := strings.TrimSpace(target.APIKey)
	if authToken == "" {
		authToken = claudeAuthTokenDummy
	}

	heading(os.Stdout, "Start claude")
	printKV(os.Stdout, "openai_upstream", cyan(target.BaseURL))
	printKV(os.Stdout, "model", bold(target.Model))
	printKV(os.Stdout, "source", target.Source)
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, dim("runhug starts a local Anthropic→OpenAI bridge so Claude Code can talk to"))
	fmt.Fprintln(os.Stdout, dim("Runpod vLLM (OpenAI chat completions) via ANTHROPIC_BASE_URL → /v1/messages."))
	fmt.Fprintln(os.Stdout)

	if noLaunch {
		listenHint := "127.0.0.1:<ephemeral>"
		if bridgePort > 0 {
			listenHint = fmt.Sprintf("127.0.0.1:%d", bridgePort)
		}
		bridgeURL := "http://" + listenHint
		env := buildClaudeEnv(bridgeURL, authToken, target.Model)
		args := buildClaudeArgs(target.Model, detectClaudeVersion())
		fmt.Fprintln(os.Stdout, bold("Environment")+dim(" (bridge would listen on "+listenHint+")"))
		for _, k := range claudeEnvUnset {
			fmt.Fprintf(os.Stdout, "  unset %s\n", k)
		}
		for _, line := range formatClaudeEnvExports(env, authToken) {
			fmt.Fprintln(os.Stdout, "  "+line)
		}
		fmt.Fprintln(os.Stdout)
		commands(os.Stdout, "Launch:", formatClaudeCommand(args))
		fmt.Fprintln(os.Stdout, dim("Note: with --no-launch the bridge is not started; use a live `start claude` to wire it."))
		return nil
	}

	listen := "127.0.0.1:0"
	if bridgePort > 0 {
		listen = fmt.Sprintf("127.0.0.1:%d", bridgePort)
	}
	srv, bridgeURL, err := bridge.Start(bridge.Config{
		UpstreamBase:  target.BaseURL,
		UpstreamKey:   target.APIKey,
		DefaultModel:  target.Model,
		ListenAddr:    listen,
		ExpectedToken: authToken,
		Sampling:      target.Sampling,
	})
	if err != nil {
		return fmt.Errorf("start anthropic bridge: %w", err)
	}
	defer func() { _ = srv.Close() }()

	printKV(os.Stdout, "anthropic_bridge", cyan(bridgeURL))
	fmt.Fprintln(os.Stdout)

	env := buildClaudeEnv(bridgeURL, authToken, target.Model)
	args := buildClaudeArgs(target.Model, detectClaudeVersion())

	fmt.Fprintln(os.Stdout, bold("Environment"))
	for _, k := range claudeEnvUnset {
		fmt.Fprintf(os.Stdout, "  unset %s\n", k)
	}
	for _, line := range formatClaudeEnvExports(env, authToken) {
		fmt.Fprintln(os.Stdout, "  "+line)
	}
	fmt.Fprintln(os.Stdout)
	commands(os.Stdout, "Launch:", formatClaudeCommand(args))

	bin, err := exec.LookPath("claude")
	if err != nil {
		fmt.Fprintln(os.Stderr, dim("claude not found on PATH — bridge is up at "+bridgeURL+" until this process exits."))
		fmt.Fprintln(os.Stderr, dim("Install Claude Code: curl -fsSL https://claude.ai/install.sh | bash"))
		fmt.Fprintln(os.Stderr, dim("Re-run without --no-launch once `claude` is on PATH, or export the env above against the bridge."))
		// Keep bridge alive briefly is wrong; without claude we should stop.
		return nil
	}

	cmd := exec.Command(bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = scrubAndMergeEnv(os.Environ(), env, claudeEnvUnset)
	fmt.Fprintf(os.Stderr, "%s launching %s via bridge %s (upstream %s)\n", dim("→"), bin, bridgeURL, target.BaseURL)
	err = cmd.Run()
	// Bridge stopped by defer.
	return err
}

// buildClaudeEnv returns Unsloth-style Anthropic env for Claude Code.
func buildClaudeEnv(bridgeBaseURL, authToken, model string) map[string]string {
	env := map[string]string{
		"ANTHROPIC_BASE_URL":                       strings.TrimRight(bridgeBaseURL, "/"),
		"ANTHROPIC_AUTH_TOKEN":                     authToken,
		"CLAUDE_CODE_ATTRIBUTION_HEADER":           "0",
		"CLAUDE_CODE_ENABLE_TELEMETRY":             "0",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
		"CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS":   "1",
	}
	if model != "" {
		env["ANTHROPIC_MODEL"] = model
		env["ANTHROPIC_DEFAULT_MODEL"] = model
	}
	return env
}

// buildClaudeArgs returns `claude` argv after the binary (model + optional settings flags).
func buildClaudeArgs(model string, version *claudeVersion) []string {
	args := []string{}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, claudeOptionalFlags(version)...)
	return args
}

func claudeOptionalFlags(version *claudeVersion) []string {
	// Claude Code < 2.1.98 aborts on unknown flags; gate like Unsloth.
	if version != nil && version.Less(2, 1, 98) {
		return nil
	}
	return []string{
		"--exclude-dynamic-system-prompt-sections",
		"--settings", claudeSettingsOverlay,
	}
}

type claudeVersion struct {
	Major, Minor, Patch int
}

func (v *claudeVersion) Less(maj, min, pat int) bool {
	if v.Major != maj {
		return v.Major < maj
	}
	if v.Minor != min {
		return v.Minor < min
	}
	return v.Patch < pat
}

var claudeVerRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

func parseClaudeVersion(stdout string) *claudeVersion {
	m := claudeVerRe.FindStringSubmatch(stdout)
	if m == nil {
		return &claudeVersion{} // unparseable → treat as too old (no optional flags)
	}
	maj, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	pat, _ := strconv.Atoi(m[3])
	return &claudeVersion{Major: maj, Minor: min, Patch: pat}
}

func detectClaudeVersion() *claudeVersion {
	bin, err := exec.LookPath("claude")
	if err != nil {
		return nil // no binary → assume current (for --no-launch printouts)
	}
	out, err := exec.Command(bin, "--version").CombinedOutput()
	if err != nil {
		return &claudeVersion{}
	}
	return parseClaudeVersion(string(out))
}

// scrubAndMergeEnv copies base, deletes unsetKeys, then applies set.
func scrubAndMergeEnv(base []string, set map[string]string, unsetKeys []string) []string {
	deny := map[string]struct{}{}
	for _, k := range unsetKeys {
		deny[k] = struct{}{}
	}
	out := make([]string, 0, len(base)+len(set))
	seen := map[string]struct{}{}
	for _, kv := range base {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		k := kv[:i]
		if _, drop := deny[k]; drop {
			continue
		}
		if _, ok := set[k]; ok {
			continue // replaced below
		}
		out = append(out, kv)
		seen[k] = struct{}{}
	}
	for k, v := range set {
		out = append(out, k+"="+v)
		seen[k] = struct{}{}
	}
	_ = seen
	return out
}

func formatClaudeEnvExports(env map[string]string, realToken string) []string {
	order := []string{
		"ANTHROPIC_BASE_URL",
		"ANTHROPIC_AUTH_TOKEN",
		"ANTHROPIC_MODEL",
		"ANTHROPIC_DEFAULT_MODEL",
		"CLAUDE_CODE_ATTRIBUTION_HEADER",
		"CLAUDE_CODE_ENABLE_TELEMETRY",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC",
		"CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS",
	}
	var lines []string
	for _, k := range order {
		v, ok := env[k]
		if !ok {
			continue
		}
		disp := v
		if k == "ANTHROPIC_AUTH_TOKEN" && realToken != "" && v == realToken {
			disp = maskSecret(realToken)
		}
		lines = append(lines, "export "+k+"="+shellQuote(disp))
	}
	return lines
}

func formatClaudeCommand(args []string) string {
	parts := make([]string, 0, 1+len(args))
	parts = append(parts, "claude")
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func startCodexStub(registryKey, baseURL, apiKeyEnv, serveModel string, noLaunch, skipPrompt bool) error {
	var err error
	registryKey, serveModel, err = pickStartModel(registryKey, baseURL, serveModel, apiKeyEnv, skipPrompt)
	if err != nil {
		return err
	}
	target, err := ResolveEndpoint(registryKey, baseURL, apiKeyEnv, serveModel)
	if err != nil {
		return err
	}
	target.BaseURL = NormalizeOpenAIBase(target.BaseURL)

	heading(os.Stdout, "Start codex (stub)")
	printKV(os.Stdout, "base_url", cyan(target.BaseURL))
	printKV(os.Stdout, "model", bold(target.Model))
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Codex launch is not fully wired yet. Typical OpenAI-compatible env:")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "  export OPENAI_BASE_URL=%s\n", shellQuote(target.BaseURL))
	keyDisp := "YOUR_RUNPOD_API_KEY"
	if target.APIKey != "" {
		keyDisp = maskSecret(target.APIKey)
	}
	fmt.Fprintf(os.Stdout, "  export OPENAI_API_KEY=%s\n", shellQuote(keyDisp))
	fmt.Fprintln(os.Stdout, "  codex")
	fmt.Fprintln(os.Stdout)
	if noLaunch {
		return nil
	}
	fmt.Fprintln(os.Stderr, dim("Not launching codex (stub). Use --no-launch to silence this note."))
	return nil
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '"' || r == '\'' || r == '$' || r == '`' || r == '\\'
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func maskSecret(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "…" + s[len(s)-4:]
}

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
