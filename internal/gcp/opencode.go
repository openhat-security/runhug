package gcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	openCodeSchema      = "https://opencode.ai/config.json"
	openCodeProviderKey = "runhug"
)

// OpenCodeSpec is the project-local OpenCode wiring for a GCP tunnel endpoint.
// Soft lock: client-side only — no apiKey field is written.
type OpenCodeSpec struct {
	BaseURL string // http://127.0.0.1:<port>/v1
	ModelID string
	Source  string
}

// OpenCodeConfig builds an opencode.json fragment. Soft lock: no literal Bearer
// is written — only an `{env:OPENAI_API_KEY}` reference (CLI-managed).
func OpenCodeConfig(spec OpenCodeSpec) map[string]any {
	modelKey := openCodeModelKey(spec.ModelID)
	models := map[string]any{
		modelKey: map[string]any{
			"id":        spec.ModelID,
			"name":      spec.ModelID,
			"tool_call": true,
		},
	}
	options := map[string]any{
		"baseURL": spec.BaseURL,
		// Env ref only — never a literal Bearer. Soft lock: no secret in the file.
		"apiKey": "{env:OPENAI_API_KEY}",
	}
	return map[string]any{
		"$schema": openCodeSchema,
		"model":   openCodeProviderKey + "/" + modelKey,
		"provider": map[string]any{
			openCodeProviderKey: map[string]any{
				"npm":     "@ai-sdk/openai-compatible",
				"name":    "runhug (" + spec.Source + ")",
				"options": options,
				"models":  models,
			},
		},
	}
}

// ProjectOpenCodePath is <dir>/.opencode/opencode.json.
func ProjectOpenCodePath(dir string) string {
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, ".opencode", "opencode.json")
}

// WriteProjectOpenCode merges cfg into the project .opencode/opencode.json.
// Existing keys are preserved; apiKey is never added by this helper.
func WriteProjectOpenCode(dir string, spec OpenCodeSpec) (string, error) {
	path := ProjectOpenCodePath(dir)
	cfg := OpenCodeConfig(spec)
	existing := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &existing); err != nil {
			return "", fmt.Errorf("%s is not valid JSON: %w", path, err)
		}
	}
	merged := deepMerge(existing, cfg).(map[string]any)
	// Soft lock: never leave a literal Bearer in the file. Keep env refs.
	normalizeProviderAPIKey(merged)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return "", err
	}
	out = append(out, '\n')
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func normalizeProviderAPIKey(cfg map[string]any) {
	prov, _ := cfg["provider"].(map[string]any)
	if prov == nil {
		return
	}
	rh, _ := prov[openCodeProviderKey].(map[string]any)
	if rh == nil {
		return
	}
	opts, _ := rh["options"].(map[string]any)
	if opts == nil {
		return
	}
	if v, ok := opts["apiKey"].(string); ok {
		if strings.HasPrefix(v, "{env:") || strings.HasPrefix(v, "${") {
			return
		}
	}
	opts["apiKey"] = "{env:OPENAI_API_KEY}"
}

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
