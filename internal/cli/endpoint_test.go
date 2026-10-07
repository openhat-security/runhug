package cli

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/runpod"
	"github.com/adamsiwiec1/runhug/internal/store"
)

func TestResolveEndpointBaseURLOverride(t *testing.T) {
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "registry.json"))
	t.Setenv(config.EnvRunpodAPIKey, "k")
	got, err := ResolveEndpoint("", "http://127.0.0.1:9/v1", "", "my-model")
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseURL != "http://127.0.0.1:9/v1" || got.Model != "my-model" || got.Source != "--base-url" {
		t.Fatalf("%+v", got)
	}
}

func TestResolveEndpointRunpodFromRegistry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.EnvConfig, filepath.Join(dir, "registry.json"))
	t.Setenv(config.EnvRunpodAPIKey, "rpakey")
	reg := &store.Registry{
		Current: "org/model",
		Models: map[string]store.Model{
			"org/model": {
				HFRepo:       "org/model",
				Backend:      store.BackendRunpod,
				EndpointID:   "abc123",
				EndpointType: runpod.EndpointTypeLoadBalancer,
				ServeName:    "org/model",
			},
		},
	}
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveEndpoint("org/model", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	want := runpod.OpenAIURLFor(runpod.EndpointTypeLoadBalancer, "abc123")
	if got.BaseURL != want {
		t.Fatalf("url %q want %q", got.BaseURL, want)
	}
	if got.APIKey != "rpakey" || got.Model != "org/model" {
		t.Fatalf("%+v", got)
	}
}

func TestResolveEndpointLocalFromRegistry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.EnvConfig, filepath.Join(dir, "registry.json"))
	t.Setenv(config.EnvRunpodAPIKey, "")
	reg := &store.Registry{
		Models: map[string]store.Model{
			"local/qwen": {
				HFRepo:    "local/qwen",
				Backend:   store.BackendLocal,
				BaseURL:   "http://127.0.0.1:11434/v1",
				Runtime:   "ollama",
				ServeName: "qwen2.5:1.5b",
			},
		},
	}
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveEndpoint("local/qwen", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseURL != "http://127.0.0.1:11434/v1" || got.Model != "qwen2.5:1.5b" {
		t.Fatalf("%+v", got)
	}
}

func TestResolveEndpointUnknownNoAutoDeploy(t *testing.T) {
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "registry.json"))
	_, err := ResolveEndpoint("missing/model", "", "", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "dry-run") {
		t.Fatalf("%v", err)
	}
}

func TestResolveEndpointUsesLiveProxy(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}))

	dir := t.TempDir()
	t.Setenv(config.EnvConfig, filepath.Join(dir, "registry.json"))
	reg := &store.Registry{
		Listen:  ln.Addr().String(),
		Current: "org/m",
		Models: map[string]store.Model{
			"org/m": {HFRepo: "org/m", Backend: store.BackendRunpod, EndpointID: "x"},
		},
	}
	// With current set, ResolveEndpoint prefers registry Runpod URL over proxy.
	// Clear current and wipe models so proxy path is used.
	reg.Current = ""
	reg.Models = map[string]store.Model{}
	raw, _ := json.MarshalIndent(reg, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "registry.json"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	// Empty models → error before proxy. Put a dummy and no current, with listen up:
	reg.Models = map[string]store.Model{
		"org/m": {HFRepo: "org/m", Backend: store.BackendRunpod, EndpointID: "x"},
	}
	reg.Current = ""
	raw, _ = json.MarshalIndent(reg, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "registry.json"), append(raw, '\n'), 0o600)

	// No model key, no current → fall through to proxy if reachable.
	got, err := ResolveEndpoint("", "", "", "default")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.BaseURL, ln.Addr().String()) {
		t.Fatalf("expected proxy url, got %+v", got)
	}
}

func TestNormalizeOpenAIBase(t *testing.T) {
	if NormalizeOpenAIBase("http://x/v1/") != "http://x/v1" {
		t.Fatal(NormalizeOpenAIBase("http://x/v1/"))
	}
	lb := "https://abc.api.runpod.ai/v1"
	if NormalizeOpenAIBase(lb) != lb {
		t.Fatal(NormalizeOpenAIBase(lb))
	}
}

func TestChatCompletionsNonStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": "hi there"}},
			},
		})
	}))
	t.Cleanup(srv.Close)
	var sink bytes.Buffer
	got, _, err := chatCompletions(t.Context(), EndpointTarget{
		BaseURL: srv.URL + "/v1",
		Model:   "m",
	}, []chatMsg{{Role: "user", Content: "hi"}}, false, &sink)
	if err != nil {
		t.Fatal(err)
	}
	if got != "hi there" {
		t.Fatalf("%q", got)
	}
}
