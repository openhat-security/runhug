package gcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCodeConfigNoAPIKey(t *testing.T) {
	cfg := OpenCodeConfig(OpenCodeSpec{
		BaseURL: "http://127.0.0.1:8080/v1",
		ModelID: "TheBloke/TinyLlama-1.1B-Chat-v1.0-GGUF",
		Source:  "gcp iap tunnel",
	})
	raw, _ := json.Marshal(cfg)
	if strings.Contains(string(raw), "apiKey") {
		t.Fatalf("apiKey must not appear: %s", raw)
	}
	prov := cfg["provider"].(map[string]any)["runhug"].(map[string]any)
	opts := prov["options"].(map[string]any)
	if _, ok := opts["apiKey"]; ok {
		t.Fatal("options.apiKey present")
	}
	if opts["baseURL"] != "http://127.0.0.1:8080/v1" {
		t.Fatalf("baseURL=%v", opts["baseURL"])
	}
}

func TestWriteProjectOpenCodeMerge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".opencode", "opencode.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{"$schema":"https://opencode.ai/config.json","autoupdate":true,"provider":{"anthropic":{"options":{"apiKey":"keep-me"}}}}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := WriteProjectOpenCode(dir, OpenCodeSpec{
		BaseURL: "http://127.0.0.1:8080/v1",
		ModelID: "org/model",
		Source:  "gcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != path {
		t.Fatalf("path=%s", out)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["autoupdate"] != true {
		t.Fatal("lost autoupdate")
	}
	prov := got["provider"].(map[string]any)
	if _, ok := prov["anthropic"]; !ok {
		t.Fatal("lost anthropic")
	}
	rh := prov["runhug"].(map[string]any)
	opts := rh["options"].(map[string]any)
	if _, ok := opts["apiKey"]; ok {
		t.Fatal("runhug apiKey must stay absent")
	}
	// anthropic key preserved
	anth := prov["anthropic"].(map[string]any)["options"].(map[string]any)
	if anth["apiKey"] != "keep-me" {
		t.Fatalf("anthropic key=%v", anth["apiKey"])
	}
}

func TestGenerateBearer(t *testing.T) {
	a, err := GenerateBearer()
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateBearer()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(a, "rh_") || a == b {
		t.Fatalf("a=%s b=%s", a, b)
	}
}
