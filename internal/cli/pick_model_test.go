package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/store"
)

func TestUsableRegistryEntriesFiltersAndOrders(t *testing.T) {
	reg := &store.Registry{
		Current: "org/b",
		Models: map[string]store.Model{
			"org/a": {HFRepo: "org/a", Backend: store.BackendRunpod, EndpointID: "ep-a", EndpointType: "QUEUE"},
			"org/b": {HFRepo: "org/b", Backend: store.BackendLocal, BaseURL: "http://127.0.0.1:1/v1", Runtime: "ollama"},
			"org/c": {HFRepo: "org/c", Backend: store.BackendRunpod},                         // no endpoint id → skip
			"org/d": {HFRepo: "org/d", Backend: store.BackendLocal, GGUFPath: "/tmp/x.gguf"}, // no base_url → skip
			"org/g": {HFRepo: "org/g", Backend: store.BackendGCP, EndpointID: "vm-1", EndpointType: "us-central1-a", BaseURL: "http://127.0.0.1:8080/v1"},
		},
	}
	got := usableRegistryEntries(reg)
	if len(got) != 3 {
		t.Fatalf("got %d: %+v", len(got), got)
	}
	if got[0].HFRepo != "org/b" {
		t.Fatalf("current should be first: %+v", got)
	}
	byRepo := map[string]registryPickEntry{}
	for _, e := range got {
		byRepo[e.HFRepo] = e
	}
	if e := byRepo["org/a"]; e.Kind != "runpod" || e.Where != "ep-a" || e.Extra != "QUEUE" {
		t.Fatalf("%+v", e)
	}
	if e := byRepo["org/b"]; e.Kind != "local" || e.Where != "http://127.0.0.1:1/v1" || e.Extra != "ollama" {
		t.Fatalf("%+v", e)
	}
	if e := byRepo["org/g"]; e.Kind != "gcp" || e.Where != "vm-1" || e.Extra != "us-central1-a" {
		t.Fatalf("%+v", e)
	}
}

func TestPickStartModelPassthrough(t *testing.T) {
	key, model, err := pickStartModel("org/m", "", "", "", true)
	if err != nil || key != "org/m" || model != "" {
		t.Fatalf("%q %q %v", key, model, err)
	}
	key, model, err = pickStartModel("", "http://x/v1", "served", "", true)
	if err != nil || key != "" || model != "served" {
		t.Fatalf("%q %q %v", key, model, err)
	}
}

func TestPickStartModelEmptyRegistryErrors(t *testing.T) {
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "registry.json"))
	_, _, err := pickStartModel("", "", "", "", true)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no registry endpoints") {
		t.Fatalf("%v", err)
	}
}

func TestPickStartModelAutoPickSingle(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.EnvConfig, filepath.Join(dir, "registry.json"))
	reg := &store.Registry{
		Models: map[string]store.Model{
			"org/only": {HFRepo: "org/only", Backend: store.BackendRunpod, EndpointID: "ep1"},
		},
	}
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
	key, model, err := pickStartModel("", "", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if key != "org/only" || model != "" {
		t.Fatalf("%q %q", key, model)
	}
}

func TestPickStartModelMultiNonInteractiveErrors(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.EnvConfig, filepath.Join(dir, "registry.json"))
	reg := &store.Registry{
		Models: map[string]store.Model{
			"org/a": {HFRepo: "org/a", Backend: store.BackendRunpod, EndpointID: "a"},
			"org/b": {HFRepo: "org/b", Backend: store.BackendRunpod, EndpointID: "b"},
		},
	}
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
	_, _, err := pickStartModel("", "", "", "", true)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "multiple models") {
		t.Fatalf("%v", err)
	}
}

func TestPickStartModelBaseURLListsRemote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{"id": "alpha"},
				{"id": "beta"},
			},
		})
	}))
	defer srv.Close()

	_, _, err := pickStartModel("", srv.URL+"/v1", "", "", true)
	if err == nil || !strings.Contains(err.Error(), "multiple models") {
		t.Fatalf("expected multi error, got %v", err)
	}

	// Single remote model auto-picks under skipPrompt.
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "solo"}},
		})
	}))
	defer srv1.Close()
	key, model, err := pickStartModel("", srv1.URL+"/v1", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if key != "" || model != "solo" {
		t.Fatalf("%q %q", key, model)
	}
}

func TestPrintRegistryPickTable(t *testing.T) {
	long := "org/" + strings.Repeat("x", 60)
	var buf strings.Builder
	printRegistryPickTable(&buf, []registryPickEntry{
		{HFRepo: long, Kind: "runpod", Where: "vllm-abc123", Extra: "QUEUE"},
		{HFRepo: "gemma4:e4b", Kind: "local", Where: "http://127.0.0.1:11434/v1", Extra: "ollama"},
		{HFRepo: "org/g", Kind: "gcp", Where: "otw-portal-dev", Extra: "us-central1-a"},
	})
	out := buf.String()
	for _, want := range []string{"Endpoints", "MODEL", "BACKEND", "WHERE", "DETAIL", "QUEUE", "ollama", "gcp"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q\n%s", want, out)
		}
	}
	if !strings.Contains(out, truncateRunes(long, pickColModel)) {
		t.Fatalf("long model should truncate\n%s", out)
	}
	if strings.Contains(out, long) {
		t.Fatalf("full long model should not appear\n%s", out)
	}
}
