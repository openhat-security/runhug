package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/hparams"
)

const (
	BackendRunpod = "runpod"
	BackendLocal  = "local"
	BackendGCP    = "gcp"
)

type Registry struct {
	Current string           `json:"current,omitempty"`
	Listen  string           `json:"listen,omitempty"`
	Models  map[string]Model `json:"models"`
}

const (
	RoleChat  = "chat"
	RoleEmbed = "embed"
)

type Model struct {
	HFRepo        string `json:"hf_repo"`
	Backend       string `json:"backend,omitempty"`
	EndpointID    string `json:"endpoint_id,omitempty"`
	EndpointType  string `json:"endpoint_type,omitempty"`
	PodID         string `json:"pod_id,omitempty"`
	PodCloud      string `json:"pod_cloud,omitempty"`
	DashboardURL  string `json:"dashboard_url,omitempty"`
	DashboardPort int    `json:"dashboard_port,omitempty"`
	BaseURL       string `json:"base_url,omitempty"`
	GGUFPath      string `json:"gguf_path,omitempty"`
	Runtime       string `json:"runtime,omitempty"`
	ServeName     string `json:"serve_name,omitempty"`
	// Role is chat (default) or embed. Embed models are preferred for /v1/embeddings.
	Role      string            `json:"role,omitempty"`
	LocalPID  int               `json:"local_pid,omitempty"`
	GPUPool   string            `json:"gpu_pool,omitempty"`
	GPUCount  int               `json:"gpu_count,omitempty"`
	Image     string            `json:"image,omitempty"`
	HourlyUSD float64           `json:"hourly_usd,omitempty"`
	Sampling  *hparams.Sampling `json:"sampling,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// IsEmbed reports whether this registry row is an embedding checkpoint.
func (m Model) IsEmbed() bool {
	if strings.EqualFold(strings.TrimSpace(m.Role), RoleEmbed) {
		return true
	}
	name := strings.ToLower(m.HFRepo + " " + m.ServeName)
	return strings.Contains(name, "embed")
}

func (m Model) Kind() string {
	if m.Backend != "" {
		return m.Backend
	}
	if m.BaseURL != "" || m.GGUFPath != "" || m.Runtime != "" {
		return BackendLocal
	}
	return BackendRunpod
}

func (m Model) UpstreamModel() string {
	if m.ServeName != "" {
		return m.ServeName
	}
	return m.HFRepo
}

func DefaultPath() (string, error) {
	if override := config.ConfigPathOverride(); override != "" {
		return override, nil
	}
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "registry.json"), nil
}

func Load() (*Registry, string, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, "", err
	}
	if override := config.ConfigPathOverride(); override == "" {
		_ = config.MigrateFileIfMissing(filepath.Dir(path), "registry.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Registry{Models: map[string]Model{}, Listen: "127.0.0.1:8080"}, path, nil
		}
		return nil, path, err
	}
	var r Registry
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, path, fmt.Errorf("registry %s: %w", path, err)
	}
	if r.Models == nil {
		r.Models = map[string]Model{}
	}
	if r.Listen == "" {
		r.Listen = "127.0.0.1:8080"
	}
	return &r, path, nil
}

func (r *Registry) Save() error {
	path, err := DefaultPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}

func (r *Registry) Put(m Model) {
	if r.Models == nil {
		r.Models = map[string]Model{}
	}
	r.Models[m.HFRepo] = m
	if r.Current == "" {
		r.Current = m.HFRepo
	}
}

func (r *Registry) Remove(key string) bool {
	m, ok := r.Lookup(key)
	if !ok {
		return false
	}
	delete(r.Models, m.HFRepo)
	if r.Current == m.HFRepo {
		r.Current = ""
		for id := range r.Models {
			r.Current = id
			break
		}
	}
	return true
}

func (r *Registry) Lookup(key string) (Model, bool) {
	key = strings.TrimSpace(key)
	if key == "" || key == "default" {
		if r.Current == "" {
			return Model{}, false
		}
		m, ok := r.Models[r.Current]
		return m, ok
	}
	if m, ok := r.Models[key]; ok {
		return m, true
	}
	for _, m := range r.Models {
		if m.EndpointID != "" && m.EndpointID == key {
			return m, true
		}
		if m.PodID != "" && m.PodID == key {
			return m, true
		}
		if m.BaseURL != "" && m.BaseURL == key {
			return m, true
		}
		if m.ServeName != "" && m.ServeName == key {
			return m, true
		}
	}
	lower := strings.ToLower(key)
	var hit Model
	hits := 0
	for id, m := range r.Models {
		if strings.Contains(strings.ToLower(id), lower) {
			hit = m
			hits++
		}
	}
	if hits == 1 {
		return hit, true
	}
	return Model{}, false
}

func (r *Registry) Use(key string) (Model, error) {
	m, ok := r.Lookup(key)
	if !ok {
		return Model{}, fmt.Errorf("unknown model %q (run `list`)", key)
	}
	r.Current = m.HFRepo
	return m, nil
}

// PickEmbed returns the best embedding model in the registry.
// preferred (settings.embed_model) wins when present; else score by name/role.
func (r *Registry) PickEmbed(preferred string) (Model, bool) {
	if r == nil {
		return Model{}, false
	}
	preferred = strings.TrimSpace(preferred)
	if preferred != "" {
		if m, ok := r.Lookup(preferred); ok {
			return m, true
		}
	}
	best, bestScore := Model{}, -1
	found := false
	for _, m := range r.Models {
		score := 0
		if m.IsEmbed() {
			score = 50
		}
		name := strings.ToLower(m.HFRepo + " " + m.ServeName)
		switch {
		case strings.Contains(name, "nomic-embed"):
			score += 100
		case strings.Contains(name, "mxbai-embed"):
			score += 70
		case strings.Contains(name, "all-minilm"):
			score += 60
		case strings.Contains(name, "embed"):
			score += 40
		}
		if score > bestScore {
			best, bestScore, found = m, score, true
		}
	}
	if !found || bestScore <= 0 {
		return Model{}, false
	}
	return best, true
}
