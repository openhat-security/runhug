package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/adamsiwiec1/runhug/internal/store"
)

// openCodeProviderKey is the opencode provider id that runhug configures.
const openCodeProviderKey = "runhug"

// openCodeSchema anchors the config to opencode's published JSON schema.
const openCodeSchema = "https://opencode.ai/config.json"

// openCodeSpec is the resolved OpenAI-compatible target for opencode.
type openCodeSpec struct {
	BaseURL string // .../v1 as opencode should dial it
	APIKey  string
	ModelID string // actual model id sent to the server (registry id or served name)
	Source  string
}

// startOpenCode wires opencode to a runhug deployment. It mirrors `start
// claude` but is config-based: opencode already speaks OpenAI-compatible
// chat completions, so no bridge is needed. The runhug provider is written to
// ~/.config/opencode/opencode.json (global scope).
func startOpenCode(registryKey, baseURL, apiKeyEnv, serveModel string, noLaunch, dryRun bool, cfgPath string, skipPrompt bool) error {
	var spec openCodeSpec
	buildDirect := func(target EndpointTarget) openCodeSpec {
		key := target.APIKey
		if key == "" {
			key = claudeAuthTokenDummy
		}
		return openCodeSpec{BaseURL: target.BaseURL, APIKey: key, ModelID: target.Model, Source: target.Source}
	}

	if strings.TrimSpace(baseURL) != "" {
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
		spec = buildDirect(target)
	} else {
		reg, _, err := store.Load()
		if err != nil {
			return err
		}
		// Prefer the local runhug proxy when it is listening: it routes to the
		// registry entry by id (or "default") and keeps auth local.
		if listen := strings.TrimSpace(reg.Listen); listen != "" && hostPortReachable(listen) {
			modelID := strings.TrimSpace(registryKey)
			if modelID == "" {
				modelID = reg.Current
			}
			if modelID == "" {
				modelID = "default"
			}
			spec = openCodeSpec{
				BaseURL: "http://" + listen + "/v1",
				APIKey:  claudeAuthTokenDummy,
				ModelID: modelID,
				Source:  "local proxy " + listen,
			}
		} else {
			var err error
			registryKey, serveModel, err = pickStartModel(registryKey, baseURL, serveModel, apiKeyEnv, skipPrompt)
			if err != nil {
				return err
			}
			target, cleanup, err := resolveReadyEndpoint(registryKey, baseURL, apiKeyEnv, serveModel, noLaunch || dryRun)
			if err != nil {
				return err
			}
			defer cleanup()
			target.BaseURL = NormalizeOpenAIBase(target.BaseURL)
			spec = buildDirect(target)
		}
	}

	modelKey := openCodeModelKey(spec.ModelID)
	cfg := openCodeConfig(spec, modelKey)

	heading(os.Stdout, "Wire opencode")
	printKV(os.Stdout, "base_url", cyan(spec.BaseURL))
	printKV(os.Stdout, "model", bold(spec.ModelID))
	printKV(os.Stdout, "opencode_id", "runhug/"+modelKey)
	printKV(os.Stdout, "source", spec.Source)
	if cfgPath == "" {
		cfgPath = defaultOpenCodeConfigPath()
	}
	printKV(os.Stdout, "config", cfgPath)
	fmt.Fprintln(os.Stdout)

	if dryRun {
		out, err := json.MarshalIndent(maskOpenCodeSecrets(cfg), "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}

	if err := writeOpenCodeConfig(cfgPath, cfg); err != nil {
		return err
	}
	commands(os.Stdout, "Health:",
		fmt.Sprintf("curl %s/models", strings.TrimRight(spec.BaseURL, "/")),
	)
	fmt.Fprintln(os.Stdout, dim("Config is read at opencode startup — quit any running opencode session and relaunch it."))

	if noLaunch {
		commands(os.Stdout, "Launch:", "opencode")
		return nil
	}

	bin, err := exec.LookPath("opencode")
	if err != nil {
		fmt.Fprintln(os.Stderr, dim("opencode not found on PATH — config written to "+cfgPath+", run `opencode` once it is installed."))
		return nil
	}
	cmd := exec.Command(bin)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Fprintf(os.Stderr, "%s launching %s (%s)\n", dim("→"), bin, spec.BaseURL)
	return cmd.Run()
}

// openCodeConfig builds an opencode.json fragment for the runhug provider.
func openCodeConfig(spec openCodeSpec, modelKey string) map[string]any {
	models := map[string]any{
		modelKey: map[string]any{
			"id":        spec.ModelID,
			"name":      spec.ModelID,
			"tool_call": true,
		},
	}
	cfg := map[string]any{
		"$schema": openCodeSchema,
		"model":   openCodeProviderKey + "/" + modelKey,
		"provider": map[string]any{
			openCodeProviderKey: map[string]any{
				"npm":  "@ai-sdk/openai-compatible",
				"name": "runhug (" + spec.Source + ")",
				"options": map[string]any{
					"baseURL": spec.BaseURL,
					"apiKey":  spec.APIKey,
				},
				"models": models,
			},
		},
	}
	return cfg
}

// maskOpenCodeSecrets returns a deep copy of cfg with provider apiKey values redacted for stdout.
func maskOpenCodeSecrets(cfg map[string]any) map[string]any {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return cfg
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return cfg
	}
	providers, _ := out["provider"].(map[string]any)
	for _, p := range providers {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		opts, _ := pm["options"].(map[string]any)
		if opts == nil {
			continue
		}
		if key, ok := opts["apiKey"].(string); ok && key != "" {
			opts["apiKey"] = maskSecret(key)
		}
	}
	return out
}

// writeOpenCodeConfig deep-merges cfg into the opencode config at path,
// preserving anything the user already configured.
func writeOpenCodeConfig(path string, cfg map[string]any) error {
	existing := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &existing); err != nil {
			return fmt.Errorf("%s is not valid JSON (%v)\nFix it by hand or point --config at a fresh opencode.json", path, err)
		}
	}

	cfg = deepMerge(existing, cfg).(map[string]any)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return os.WriteFile(path, out, 0o644)
}

// deepMerge overlays src onto dst (maps merge recursively; other values replace).
func deepMerge(dst, src any) any {
	srcMap, sOK := src.(map[string]any)
	dstMap, dOK := dst.(map[string]any)
	if !sOK || !dOK {
		return src
	}
	out := make(map[string]any, len(dstMap)+len(srcMap))
	for k, v := range dstMap {
		out[k] = v
	}
	for k, v := range srcMap {
		if prev, ok := out[k]; ok {
			out[k] = deepMerge(prev, v)
		} else {
			out[k] = v
		}
	}
	return out
}

// openCodeModelKey turns a served model id into a slash-free opencode model
// key (opencode expects "provider/model"; the sent id lives in models.<key>.id).
func openCodeModelKey(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || id == "default" {
		return "default"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(id) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "default"
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

func defaultOpenCodeConfigPath() string {
	if v := os.Getenv("OPENCODE_CONFIG"); strings.TrimSpace(v) != "" {
		return v
	}
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		return filepath.Join(u.HomeDir, ".config", "opencode", "opencode.json")
	}
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, ".config", "opencode", "opencode.json")
	}
	return filepath.Join(".", "opencode.json")
}
