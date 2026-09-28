package hf

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPickGGUFPrefersQ4KM(t *testing.T) {
	m := Model{
		Siblings: []Sibling{
			{RFilename: "model.Q8_0.gguf"},
			{RFilename: "model.Q4_K_M.gguf"},
			{RFilename: "model.Q3_K_M.gguf"},
			{RFilename: "mmproj.gguf"},
		},
	}
	file, quant, ok := PickGGUF(m)
	if !ok || file != "model.Q4_K_M.gguf" {
		t.Fatalf("file %s quant %s ok %v", file, quant, ok)
	}
	if quant != "Q4_K_M" {
		t.Fatalf("quant %s", quant)
	}
}

func TestDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/org/m/resolve/main/a.gguf" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("GGUF"))
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "a.gguf")
	c := New("")
	c.BaseURL = srv.URL
	if err := c.Download(context.Background(), "org/m", "a.gguf", dest); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "GGUF" {
		t.Fatalf("got %q", raw)
	}
}

func TestGenerationConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/org/m/resolve/main/generation_config.json":
			_, _ = w.Write([]byte(`{"temperature":0.2,"top_p":0.9}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := New("")
	c.BaseURL = srv.URL
	got, err := c.GenerationConfig(context.Background(), "org/m")
	if err != nil {
		t.Fatal(err)
	}
	if got["temperature"] != 0.2 {
		t.Fatalf("%+v", got)
	}
	empty, err := c.GenerationConfig(context.Background(), "org/missing")
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("404 should be empty, got %+v", empty)
	}
}
