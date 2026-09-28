package cli

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/hparams"
	"github.com/adamsiwiec1/runhug/internal/runpod"
	"github.com/adamsiwiec1/runhug/internal/store"
)

// EndpointTarget is an OpenAI-compatible chat target resolved for run/start.
type EndpointTarget struct {
	BaseURL    string // .../v1
	APIKey     string
	Model      string // served model name for chat completions
	Source     string // human-readable origin
	HFRepo     string
	NeedsProxy bool // true when using local runhug proxy listen addr
	Sampling   *hparams.Sampling
}

// ResolveEndpoint picks an OpenAI base URL for interactive use.
// Precedence: explicit baseURL → registry model (Runpod/local) → current + live proxy → error with deploy hint.
func ResolveEndpoint(modelKey, baseURL, apiKeyEnv, serveModel string) (EndpointTarget, error) {
	env := config.Load()
	key := env.RunpodAPIKey
	if apiKeyEnv != "" {
		if v := config.SanitizeAPIKey(os.Getenv(apiKeyEnv)); v != "" {
			key = v
		} else if apiKeyEnv != "" && strings.TrimSpace(os.Getenv(apiKeyEnv)) == "" {
			return EndpointTarget{}, fmt.Errorf("env %s is empty", apiKeyEnv)
		}
	}

	if bu := strings.TrimSpace(baseURL); bu != "" {
		t := EndpointTarget{
			BaseURL: strings.TrimRight(bu, "/"),
			APIKey:  key,
			Model:   strings.TrimSpace(serveModel),
			Source:  "--base-url",
		}
		if t.Model == "" {
			t.Model = "default"
		}
		return t, nil
	}

	reg, _, err := store.Load()
	if err != nil {
		return EndpointTarget{}, err
	}

	keyLookup := strings.TrimSpace(modelKey)
	if keyLookup != "" {
		if m, ok := reg.Lookup(keyLookup); ok {
			return targetFromModel(m, key, serveModel)
		}
		// Not in registry: do not auto-deploy.
		return EndpointTarget{}, fmt.Errorf(
			"no registry entry for %q\nHint: deploy first (`runhug deploy %s --dry-run` then confirm live deploy), or pass --base-url / use an existing endpoint id",
			keyLookup, keyLookup,
		)
	}

	// No model arg: prefer current registry entry.
	if reg.Current != "" {
		if m, ok := reg.Models[reg.Current]; ok {
			t, err := targetFromModel(m, key, serveModel)
			if err == nil {
				return t, nil
			}
		}
	}

	// Fall back to local proxy if something is listening.
	if listen := strings.TrimSpace(reg.Listen); listen != "" {
		if hostPortReachable(listen) {
			model := strings.TrimSpace(serveModel)
			if model == "" {
				model = "default"
				if reg.Current != "" {
					model = reg.Current
				}
			}
			return EndpointTarget{
				BaseURL:    "http://" + listen + "/v1",
				APIKey:     key,
				Model:      model,
				Source:     "local proxy (" + listen + ")",
				NeedsProxy: true,
			}, nil
		}
	}

	if len(reg.Models) == 0 {
		return EndpointTarget{}, fmt.Errorf(
			"no endpoint configured\nHint: run `runhug deploy <model> --dry-run` (then confirm to create), or `runhug local add` / `runhug proxy`",
		)
	}
	return EndpointTarget{}, fmt.Errorf(
		"could not resolve an OpenAI endpoint\nHint: pass a registry model (`runhug list`), --base-url, or start `runhug proxy`",
	)
}

func targetFromModel(m store.Model, apiKey, serveModel string) (EndpointTarget, error) {
	modelName := strings.TrimSpace(serveModel)
	if modelName == "" {
		modelName = m.UpstreamModel()
	}
	if m.Kind() == store.BackendLocal {
		if m.BaseURL == "" {
			return EndpointTarget{}, fmt.Errorf("local model %s has no base_url; run `runhug local start`", m.HFRepo)
		}
		return EndpointTarget{
			BaseURL:   strings.TrimRight(m.BaseURL, "/"),
			APIKey:    apiKey,
			Model:     modelName,
			Source:    "local " + m.Runtime,
			HFRepo:    m.HFRepo,
			Sampling:  m.Sampling,
		}, nil
	}
	if m.Kind() == store.BackendGCP {
		base := strings.TrimRight(m.BaseURL, "/")
		if base == "" {
			return EndpointTarget{}, fmt.Errorf("gcp model %s has no tunnel URL; run `runhug gcp tunnel`", m.HFRepo)
		}
		return EndpointTarget{
			BaseURL:  base,
			APIKey:   apiKey,
			Model:    modelName,
			Source:   "gcp " + m.PodID,
			HFRepo:   m.HFRepo,
			Sampling: m.Sampling,
		}, nil
	}
	if m.EndpointID == "" {
		return EndpointTarget{}, fmt.Errorf("registry entry %s has no endpoint id", m.HFRepo)
	}
	return EndpointTarget{
		BaseURL:  runpod.OpenAIURLFor(m.EndpointType, m.EndpointID),
		APIKey:   apiKey,
		Model:    modelName,
		Source:   "runpod " + m.EndpointID,
		HFRepo:   m.HFRepo,
		Sampling: m.Sampling,
	}, nil
}

func hostPortReachable(listen string) bool {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// NormalizeOpenAIBase ensures the URL ends at the /v1 root used by chat clients.
func NormalizeOpenAIBase(raw string) string {
	s := strings.TrimRight(strings.TrimSpace(raw), "/")
	if s == "" {
		return s
	}
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	path := strings.TrimRight(u.Path, "/")
	if strings.HasSuffix(path, "/v1") || strings.HasSuffix(path, "/openai/v1") {
		return s
	}
	// Already a full OpenAI-compat root from Runpod helpers.
	if strings.Contains(path, "/openai/v1") {
		return s
	}
	return s
}
