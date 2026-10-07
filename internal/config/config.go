package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	EnvRunpodAPIKey    = "RUNPOD_API_KEY"
	EnvHFToken         = "HF_TOKEN"
	EnvConfig          = "RUNHUG_CONFIG"
	EnvConfigLegacy    = "RVP_CONFIG"
	EnvUpdateLimit     = "RUNHUG_UPDATE_LIMIT"
	DefaultUpdateLimit = 2000

	appDirName        = "runhug"
	prevAppDirName    = "runhug-cli" // immediate predecessor config dir
	oldAppDirName     = "runpod-vllm-proxy"
	storedKeyName     = "runpod.key"
	storedHFTokenName = "hf.token"
	settingsFileName  = "settings.json"
)

type Env struct {
	RunpodAPIKey string
	HFToken      string
}

// SanitizeAPIKey trims space, strips a UTF-8 BOM, drops ASCII controls
// (including CR/LF), removes a leading "Bearer ", and keeps only printable
// ASCII so net/http will accept the Authorization header. The value is never
// logged. An empty result means reject the key.
func SanitizeAPIKey(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "\ufeff")
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		// RFC 7230: no CTL in header values. Clipboard paste can also insert
		// non-ASCII; keep printable ASCII only (token-safe for Bearer).
		if c < 0x21 || c > 0x7e {
			continue
		}
		b.WriteByte(c)
	}
	s = b.String()
	const bearer = "bearer"
	if len(s) > len(bearer) && strings.EqualFold(s[:len(bearer)], bearer) {
		s = s[len(bearer):]
	}
	return s
}

func Load() Env {
	key := SanitizeAPIKey(os.Getenv(EnvRunpodAPIKey))
	if key == "" {
		key = loadStoredKey()
	}
	hf := SanitizeAPIKey(os.Getenv(EnvHFToken))
	if hf == "" {
		hf = loadStoredHFToken()
	}
	return Env{
		RunpodAPIKey: key,
		HFToken:      hf,
	}
}

func (e Env) RequireRunpod() error {
	if e.RunpodAPIKey == "" {
		return fmt.Errorf("not connected — run `runhug connect` or set %s", EnvRunpodAPIKey)
	}
	return nil
}

func (e Env) Connected() bool {
	return e.RunpodAPIKey != ""
}

// ConfigPathOverride returns RUNHUG_CONFIG, or RVP_CONFIG during transition.
func ConfigPathOverride() string {
	if v := strings.TrimSpace(os.Getenv(EnvConfig)); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv(EnvConfigLegacy))
}

// XDGConfigHome returns $XDG_CONFIG_HOME when set, otherwise ~/.config.
func XDGConfigHome() (string, error) {
	if v := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

// Dir is the preferred config directory: ~/.config/runhug
// (or $XDG_CONFIG_HOME/runhug). Env overrides point at a registry file;
// Dir is that file's parent.
func Dir() (string, error) {
	if override := ConfigPathOverride(); override != "" {
		return filepath.Dir(override), nil
	}
	base, err := XDGConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, appDirName), nil
}

// LegacyDirs lists prior config directories (runhug-cli, then runpod-vllm-proxy).
// Order prefers the more recent predecessor so keys/registry migrate from there first.
func LegacyDirs() []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	if base, err := XDGConfigHome(); err == nil {
		add(filepath.Join(base, prevAppDirName))
	}
	if dir, err := os.UserConfigDir(); err == nil {
		add(filepath.Join(dir, prevAppDirName))
		add(filepath.Join(dir, oldAppDirName))
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, "Library", "Application Support", oldAppDirName))
	}
	return out
}

// MigrateFileIfMissing copies name from a legacy dir into destDir when dest is absent.
func MigrateFileIfMissing(destDir, name string) error {
	dest := filepath.Join(destDir, name)
	if _, err := os.Stat(dest); err == nil {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, oldDir := range LegacyDirs() {
		src := filepath.Join(oldDir, name)
		in, err := os.Open(src)
		if err != nil {
			continue
		}
		if err := os.MkdirAll(destDir, 0o700); err != nil {
			in.Close()
			return err
		}
		out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			in.Close()
			if os.IsExist(err) {
				return nil
			}
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		in.Close()
		if copyErr != nil {
			_ = os.Remove(dest)
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		_ = os.Chmod(dest, 0o600)
		return nil
	}
	return nil
}

func StoredKeyPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, storedKeyName), nil
}

func SaveKey(key string) error {
	key = SanitizeAPIKey(key)
	if key == "" {
		return fmt.Errorf("empty API key")
	}
	path, err := StoredKeyPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func DeleteKey() error {
	path, err := StoredKeyPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func HasStoredKey() bool {
	return loadStoredKey() != ""
}

func loadStoredKey() string {
	path, err := StoredKeyPath()
	if err != nil {
		return ""
	}
	// Do not migrate into an explicit override path (tests / custom installs).
	if ConfigPathOverride() == "" {
		_ = MigrateFileIfMissing(filepath.Dir(path), storedKeyName)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return SanitizeAPIKey(string(raw))
}

func StoredHFTokenPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, storedHFTokenName), nil
}

func SaveHFToken(token string) error {
	token = SanitizeAPIKey(token)
	if token == "" {
		return fmt.Errorf("empty Hugging Face token")
	}
	path, err := StoredHFTokenPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func DeleteHFToken() error {
	path, err := StoredHFTokenPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func HasStoredHFToken() bool {
	return loadStoredHFToken() != ""
}

func loadStoredHFToken() string {
	path, err := StoredHFTokenPath()
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return SanitizeAPIKey(string(raw))
}

// GPUPreference is the user's saved default accelerator for deploy / local flows.
type GPUPreference struct {
	Provider string  `json:"provider"` // local | runpod | gcp
	Key      string  `json:"key"`      // pool id, L4/T4, or local matched name
	Name     string  `json:"name,omitempty"`
	MemoryGB float64 `json:"memory_gb,omitempty"`
}

// Settings holds optional CLI defaults under ~/.config/runhug/settings.json.
type Settings struct {
	NoColor        bool   `json:"no_color"`
	UpdateLimit    *int   `json:"update_limit,omitempty"` // Hub update row cap; 0=unlimited; nil=default
	AdvisorBaseURL string `json:"advisor_base_url,omitempty"`
	AdvisorModel   string `json:"advisor_model,omitempty"`
	// EmbedModel is the registry id used for POST /v1/embeddings when the
	// requested model is a chat checkpoint (or unset). Prefer nomic-embed-*.
	EmbedModel string         `json:"embed_model,omitempty"`
	GPU        *GPUPreference `json:"gpu,omitempty"`
	// RunMode is "agent" (default) or "plan" for runhug run.
	RunMode string `json:"run_mode,omitempty"`
	// RunPerm is "ask" (default), "allow", or "deny" for mutating tools.
	RunPerm string `json:"run_perm,omitempty"`
}

func SettingsPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, settingsFileName), nil
}

func LoadSettings() Settings {
	path, err := SettingsPath()
	if err != nil {
		return Settings{}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Settings{}
	}
	var s Settings
	if err := json.Unmarshal(raw, &s); err != nil {
		return Settings{}
	}
	return s
}

func SaveSettings(s Settings) error {
	path, err := SettingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// ResolveUpdateLimit returns the Hub update row cap.
// Precedence: flag (>=0) > RUNHUG_UPDATE_LIMIT > settings.update_limit > DefaultUpdateLimit (2000).
// 0 means unlimited. flagVal < 0 means the flag was not set.
func ResolveUpdateLimit(flagVal int) int {
	if flagVal >= 0 {
		return flagVal
	}
	if v := strings.TrimSpace(os.Getenv(EnvUpdateLimit)); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n >= 0 {
			return n
		}
	}
	s := LoadSettings()
	if s.UpdateLimit != nil && *s.UpdateLimit >= 0 {
		return *s.UpdateLimit
	}
	return DefaultUpdateLimit
}

// EffectiveUpdateLimit returns the resolved limit when no CLI flag is set.
func EffectiveUpdateLimit() int {
	return ResolveUpdateLimit(-1)
}

// ColorDisabled reports whether ANSI should be off via env or settings.
func ColorDisabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return true
	}
	return LoadSettings().NoColor
}
