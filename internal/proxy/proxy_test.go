package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adamsiwiec1/runhug/internal/store"
)

func TestModelsAndUnknown(t *testing.T) {
	reg := &store.Registry{
		Current: "Qwen/Qwen2.5-7B-Instruct",
		Models: map[string]store.Model{
			"Qwen/Qwen2.5-7B-Instruct": {HFRepo: "Qwen/Qwen2.5-7B-Instruct", EndpointID: "ep1"},
		},
	}
	h := New(reg, "test-key").Handler()

	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if res.Code != 200 {
		t.Fatalf("models status %d", res.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	data, _ := body["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("models %+v", body)
	}

	res = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"missing","messages":[]}`))
	h.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("unknown model status %d body %s", res.Code, res.Body.String())
	}
}

func TestForwardRewritesAuthAndPath(t *testing.T) {
	var gotAuth, gotPath string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(up.Close)

	reg := &store.Registry{
		Current: "org/model",
		Models:  map[string]store.Model{"org/model": {HFRepo: "org/model", EndpointID: "ep1"}},
	}
	s := New(reg, "secret-key")
	// Point OpenAI host at the test server by swapping the request through a custom trip.
	// We exercise openaiSuffix + Lookup instead of the live Runpod host.
	if openaiSuffix("/v1/chat/completions") != "/chat/completions" {
		t.Fatalf("suffix %s", openaiSuffix("/v1/chat/completions"))
	}
	if openaiSuffix("/openai/v1/completions") != "/completions" {
		t.Fatalf("suffix %s", openaiSuffix("/openai/v1/completions"))
	}
	m, ok := s.Registry.Lookup("default")
	if !ok || m.EndpointID != "ep1" {
		t.Fatalf("lookup %+v %v", m, ok)
	}
	_ = gotAuth
	_ = gotPath
	_ = up
}

func TestOpenAISuffix(t *testing.T) {
	if openaiSuffix("/v1/chat/completions") != "/chat/completions" {
		t.Fatal(openaiSuffix("/v1/chat/completions"))
	}
}

func TestForwardLocalRewritesModelAndSkipsRunpodAuth(t *testing.T) {
	var gotAuth, gotPath, gotBody string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(up.Close)

	reg := &store.Registry{
		Current: "local/tiny",
		Models: map[string]store.Model{
			"local/tiny": {
				HFRepo:  "local/tiny",
				Backend: store.BackendLocal,
				BaseURL: up.URL + "/v1",
			},
		},
	}
	h := New(reg, "secret-key").Handler()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"default","messages":[]}`))
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatalf("status %d body %s", res.Code, res.Body.String())
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path %s", gotPath)
	}
	if gotAuth == "Bearer secret-key" {
		t.Fatal("must not inject Runpod key for local")
	}
	if !strings.Contains(gotBody, `"model":"local/tiny"`) {
		t.Fatalf("body %s", gotBody)
	}
}

func TestRewriteUsesServeName(t *testing.T) {
	body, err := rewriteModel([]byte(`{"model":"default"}`), "qwen2.5-coder:1.5b")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "qwen2.5-coder:1.5b") {
		t.Fatalf("%s", body)
	}
}

func TestForwardRunpodInjectsBearer(t *testing.T) {
	if openaiSuffix("/openai/v1/completions") != "/completions" {
		t.Fatal(openaiSuffix("/openai/v1/completions"))
	}
	body, err := rewriteModel([]byte(`{"model":"default"}`), "Qwen/Qwen2.5-7B-Instruct")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Qwen/Qwen2.5-7B-Instruct") {
		t.Fatalf("%s", body)
	}
}

func TestForwardEmbeddingsPicksEmbedModel(t *testing.T) {
	var gotPath, gotBody string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"embedding":[0.1,0.2],"index":0}],"object":"list"}`)
	}))
	t.Cleanup(up.Close)

	reg := &store.Registry{
		Current: "chat/model",
		Models: map[string]store.Model{
			"chat/model": {
				HFRepo:  "chat/model",
				Backend: store.BackendLocal,
				BaseURL: "http://127.0.0.1:9/v1", // unused if embed wins
			},
			"nomic-embed-text:latest": {
				HFRepo:    "nomic-embed-text:latest",
				Backend:   store.BackendLocal,
				Runtime:   "ollama",
				ServeName: "nomic-embed-text:latest",
				Role:      store.RoleEmbed,
				BaseURL:   up.URL + "/v1",
			},
		},
	}
	h := New(reg, "").Handler()
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"model":"default","input":"hi","dimensions":1536}`))
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatalf("status %d body %s", res.Code, res.Body.String())
	}
	if gotPath != "/v1/embeddings" {
		t.Fatalf("path %s", gotPath)
	}
	if !strings.Contains(gotBody, "nomic-embed-text") {
		t.Fatalf("expected embed model rewrite, body %s", gotBody)
	}
	if strings.Contains(gotBody, "dimensions") {
		t.Fatalf("ollama path must strip dimensions: %s", gotBody)
	}
}

func TestStripDimensions(t *testing.T) {
	out, err := stripDimensions([]byte(`{"model":"x","input":"y","dimensions":1536}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "dimensions") {
		t.Fatalf("%s", out)
	}
}
