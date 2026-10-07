package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/store"
)

func TestLocalPortFromBaseURL(t *testing.T) {
	if got := localPortFromBaseURL("http://127.0.0.1:18080/v1"); got != 18080 {
		t.Fatalf("got %d", got)
	}
	if got := localPortFromBaseURL("http://127.0.0.1:8080/v1"); got != 8080 {
		t.Fatalf("got %d", got)
	}
	if got := gcpListenPort(EnsureOpts{}, store.Model{BaseURL: "http://127.0.0.1:8080/v1"}); got != 18080 {
		t.Fatalf("8080 registry must not be the local tunnel: %d", got)
	}
	if got := gcpListenPort(EnsureOpts{}, store.Model{BaseURL: "http://127.0.0.1:18080/v1"}); got != 18080 {
		t.Fatalf("got %d", got)
	}
	if got := gcpListenPort(EnsureOpts{LocalPort: 19090}, store.Model{BaseURL: "http://127.0.0.1:8080/v1"}); got != 19090 {
		t.Fatalf("got %d", got)
	}
	if got := localPortFromBaseURL(""); got != 0 {
		t.Fatalf("got %d", got)
	}
}

func TestPrintEnsureStep(t *testing.T) {
	var buf strings.Builder
	printEnsureStep(&buf, 2, 4, "tunnel", "127.0.0.1:18080")
	if !strings.Contains(buf.String(), "2/4") || !strings.Contains(buf.String(), "tunnel") {
		t.Fatal(buf.String())
	}
}

func TestEnsureReadyLocalAlreadyUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"x"}]}`))
	}))
	defer srv.Close()

	m := store.Model{
		HFRepo:  "local/x",
		Backend: store.BackendLocal,
		BaseURL: srv.URL + "/v1",
		Runtime: "ollama",
	}
	h, err := EnsureReady(context.Background(), m, EnsureOpts{
		Writer: io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.Target.BaseURL != srv.URL+"/v1" {
		t.Fatalf("%+v", h.Target)
	}
	h.Cleanup()
}

func TestEnsureReadyGCPAlreadyReachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv(config.EnvConfig, filepath.Join(dir, "cfg"))
	m := store.Model{
		HFRepo:       "org/g",
		Backend:      store.BackendGCP,
		EndpointID:   "proj",
		EndpointType: "us-central1-a",
		PodID:        "runhug-org-g",
		BaseURL:      srv.URL + "/v1",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h, err := EnsureReady(ctx, m, EnsureOpts{Writer: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	// Port from BaseURL is used for LocalOpenAIURL rewrite — server must match that port.
	// When already reachable via probe on rewritten URL, fast path uses LocalOpenAIURL(port).
	if h.Target.Model != "org/g" {
		t.Fatalf("%+v", h.Target)
	}
	h.Cleanup()
}

func TestProbeOpenAIModelsAuthAndStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	if err := probeOpenAIModels(context.Background(), srv.URL+"/v1", "secret"); err != nil {
		t.Fatal(err)
	}
	if err := probeOpenAIModels(context.Background(), srv.URL+"/v1", "wrong"); err == nil {
		t.Fatal("expected auth failure")
	}
}

func TestWaitOpenAIReadyTimeout(t *testing.T) {
	ln := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ln.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	var buf strings.Builder
	err := waitOpenAIReady(ctx, ln.URL+"/v1", "", 50*time.Millisecond, 300*time.Millisecond, &buf, 3, 4)
	if err == nil {
		t.Fatal("expected timeout")
	}
	if !strings.Contains(buf.String(), "openai") {
		t.Fatalf("expected progress log, got %q", buf.String())
	}
}

func TestInspectLocalHTTPForeignExpress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Powered-By", "Express")
		http.NotFound(w, r)
	}))
	defer srv.Close()
	hit := inspectLocalHTTP(context.Background(), srv.URL+"/v1", "")
	if !hit.foreign || hit.status != 404 {
		t.Fatalf("%+v", hit)
	}
}

func TestFormatProbeErr(t *testing.T) {
	got := formatProbeErr(fmt.Errorf("HTTP 404"))
	if !strings.Contains(got, "404") || !strings.Contains(got, "GGUF") {
		t.Fatalf("%q", got)
	}
	rst := formatProbeErr(fmt.Errorf(`Get "http://127.0.0.1:18080/v1/models": connection reset by peer`))
	if !strings.Contains(rst, "8080") {
		t.Fatalf("%q", rst)
	}
}
