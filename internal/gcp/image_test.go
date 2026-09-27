package gcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerfileNoSecrets(t *testing.T) {
	df := Dockerfile(ImageConfig{ModelID: "Qwen/Qwen2.5-1.5B-Instruct-GGUF", GGUFFile: "model.Q4_K_M.gguf"})
	for _, ban := range []string{"BEGIN PRIVATE KEY", "client_email", "refresh_token", "API_KEY=", "runhug-api-key="} {
		if strings.Contains(df, ban) {
			t.Fatalf("Dockerfile must not contain %q", ban)
		}
	}
	// Comment may mention Bearer as a soft-lock reminder; ensure no assignment/value.
	if strings.Contains(df, "Bearer rh_") || strings.Contains(df, "Authorization:") {
		t.Fatal("Dockerfile must not embed Authorization/Bearer values")
	}
	if !strings.Contains(df, "llama-server") {
		t.Fatal("expected llama-server target")
	}
	if !strings.Contains(df, "127.0.0.1") {
		t.Fatal("expected bind hint / HOST default")
	}
	if !strings.Contains(df, "MODEL_ID=") {
		t.Fatal("expected MODEL_ID env hint")
	}
}

func TestEntrypointSoftLocks(t *testing.T) {
	ep := EntrypointScript(ImageConfig{IdleSeconds: 120})
	for _, want := range []string{"127.0.0.1", "--api-key", "IDLE_SECONDS", "shutdown", "runhug-api-key", "huggingface-cli"} {
		if !strings.Contains(ep, want) {
			t.Fatalf("entrypoint missing %q", want)
		}
	}
	// Must not embed a concrete bearer.
	if strings.Contains(ep, "rh_") {
		t.Fatal("entrypoint unexpectedly contains rh_ bearer prefix")
	}
}

func TestStartupEmbedsEntrypointBase64(t *testing.T) {
	st := StartupScript(ImageConfig{ModelID: "org/model"})
	if !strings.Contains(st, "base64 -d") {
		t.Fatal("startup should decode entrypoint via base64")
	}
	if strings.Contains(st, "BEGIN PRIVATE KEY") {
		t.Fatal("startup must not contain SA keys")
	}
}

func TestWriteImageFiles(t *testing.T) {
	dir := t.TempDir()
	if err := WriteImageFiles(dir, ImageConfig{ModelID: "a/b"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Dockerfile", "entrypoint.sh"} {
		p := filepath.Join(dir, name)
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if name == "entrypoint.sh" && st.Mode()&0o111 == 0 {
			t.Fatalf("entrypoint not executable: %v", st.Mode())
		}
	}
}
