package find

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanFindsGGUF(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RVP_MODELS", dir)
	t.Setenv("RVP_CACHE", filepath.Join(t.TempDir(), "empty-cache"))
	// Real GGUFs are large; tests use a >1MiB stub so the size floor still applies.
	payload := make([]byte, 1<<20+64)
	copy(payload, "GGUF")
	if err := os.WriteFile(filepath.Join(dir, "Qwen2.5-1.5B-Instruct-Q4_K_M.gguf"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mmproj-f16.gguf"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tiny.gguf"), []byte("GGUF"), 0o644); err != nil {
		t.Fatal(err)
	}
	hits := Filter(scanRoots([]string{dir}), "qwen")
	if len(hits) != 1 || hits[0].Kind != "gguf" {
		t.Fatalf("%+v", hits)
	}
	if !filepath.IsAbs(hits[0].Path) {
		t.Fatal(hits[0].Path)
	}
}

func TestOllamaManifestsFromDisk(t *testing.T) {
	root := t.TempDir()
	man := filepath.Join(root, "manifests", "registry.ollama.ai", "library", "qwen2.5-coder")
	if err := os.MkdirAll(man, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(man, "1.5b"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OLLAMA_MODELS", root)
	names := ollamaManifestNames()
	if len(names) != 1 || names[0] != "qwen2.5-coder:1.5b" {
		t.Fatalf("%v", names)
	}
}

func TestOllamaNameFromRel(t *testing.T) {
	got, ok := ollamaNameFromRel("registry.ollama.ai/library/qwen2.5-coder/14b")
	if !ok || got != "qwen2.5-coder:14b" {
		t.Fatalf("%q %v", got, ok)
	}
	got, ok = ollamaNameFromRel("hf.co/bartowski/Foo-GGUF/Q4_K_M")
	if !ok || got != "hf.co/bartowski/Foo-GGUF:Q4_K_M" {
		t.Fatalf("%q %v", got, ok)
	}
}

func TestHuggingFaceHubRootsPreferHFHome(t *testing.T) {
	hub := t.TempDir()
	t.Setenv("HF_HOME", hub)
	t.Setenv("HUGGINGFACE_HUB_CACHE", "")
	roots := huggingfaceHubRoots()
	want := filepath.Join(hub, "hub")
	found := false
	for _, r := range roots {
		if r == hub || r == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected HF_HOME or HF_HOME/hub in %v", roots)
	}
}

func TestScanFindsGGUFUnderHFHomeHub(t *testing.T) {
	home := t.TempDir()
	hub := filepath.Join(home, "hub", "models--unsloth--Toy-GGUF", "snapshots", "abc")
	if err := os.MkdirAll(hub, 0o755); err != nil {
		t.Fatal(err)
	}
	gguf := filepath.Join(hub, "toy-Q4_K_M.gguf")
	payload := make([]byte, 1<<20+8)
	copy(payload, "GGUF")
	if err := os.WriteFile(gguf, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	// Only scan this synthetic hub — not the developer's real caches.
	hits := scanRoots([]string{filepath.Join(home, "hub")})
	if len(hits) != 1 || hits[0].Kind != "gguf" || !strings.Contains(hits[0].Name, "toy") {
		t.Fatalf("%+v", hits)
	}
}

func TestFilterEmptyQuery(t *testing.T) {
	in := []Found{{Name: "a"}, {Name: "b"}}
	if len(Filter(in, "")) != 2 {
		t.Fatal("empty query should keep all")
	}
}

func TestScanSkipsBrokenSymlinkGGUF(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "missing-blob"), filepath.Join(dir, "broken.gguf")); err != nil {
		t.Fatal(err)
	}
	hits := scanRoots([]string{dir})
	if len(hits) != 0 {
		t.Fatalf("want no hits for broken symlink, got %+v", hits)
	}
}
