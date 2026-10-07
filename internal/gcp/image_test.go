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
	if !strings.Contains(df, PrebuiltLlamaServerCUDA) {
		t.Fatal("expected thin wrap of official server-cuda image")
	}
	if strings.Contains(df, "cmake -B") || strings.Contains(df, "git clone") {
		t.Fatal("Dockerfile must not compile llama.cpp from source")
	}
	if !strings.Contains(df, "iproute2") {
		t.Fatal("expected iproute2 for idle ss peers")
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
	for _, want := range []string{
		"127.0.0.1", "--api-key", "IDLE_SECONDS", "shutdown", "runhug-api-key",
		"runhug-hf-token", "runhug-model-id", "runhug-gguf-file",
		"hf \"", "instances/", "/stop", "keep-up", "EMBEDDINGS",
	} {
		if !strings.Contains(ep, want) {
			t.Fatalf("entrypoint missing %q", want)
		}
	}
	// Must not embed a concrete bearer.
	if strings.Contains(ep, "rh_") {
		t.Fatal("entrypoint unexpectedly contains rh_ bearer prefix")
	}
}

func TestEntrypointEmbeddingsFlag(t *testing.T) {
	ep := EntrypointScript(ImageConfig{Embeddings: true})
	if !strings.Contains(ep, "--embeddings") {
		t.Fatal("expected --embeddings when Embeddings=true")
	}
	st := StartupScript(ImageConfig{Embeddings: true})
	if !strings.Contains(st, "EMBEDDINGS=1") {
		t.Fatal("startup must pass EMBEDDINGS=1")
	}
}

func TestEntrypointKeepUpSkipsIdleStop(t *testing.T) {
	ep := EntrypointScript(ImageConfig{KeepUp: true, IdleSeconds: 0})
	if !strings.Contains(ep, "stop-on-idle disabled") {
		t.Fatal("expected keep-up message")
	}
}

func TestStartupDockerLauncher(t *testing.T) {
	st := StartupScript(ImageConfig{ModelID: "org/model"})
	if strings.Contains(st, "cmake -B") || strings.Contains(st, "git clone") {
		t.Fatal("startup must not compile llama on the VM")
	}
	if strings.Contains(st, "BEGIN PRIVATE KEY") {
		t.Fatal("startup must not contain SA keys")
	}
	for _, want := range []string{"docker pull", "docker run", "--gpus all", "--network host", "runhug-container-image", "127.0.0.1", "ensure_docker", "nvidia-container-toolkit", "docker.io", "ForceIPv4"} {
		if !strings.Contains(st, want) {
			t.Fatalf("startup missing %q", want)
		}
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

func TestPushDryRun(t *testing.T) {
	dir := t.TempDir()
	res, err := Push(t.Context(), PushRequest{
		Image:  "us-central1-docker.pkg.dev/p/runhug/llama-server:cuda",
		Dir:    dir,
		DryRun: true,
		Config: ImageConfig{ModelID: "org/m"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Image != "us-central1-docker.pkg.dev/p/runhug/llama-server:cuda" {
		t.Fatalf("image=%s", res.Image)
	}
	if res.Platform != DefaultImagePlatform {
		t.Fatalf("platform=%s", res.Platform)
	}
	joined := strings.Join(res.BuildCmd, " ")
	if !strings.Contains(joined, "docker build") || !strings.Contains(joined, "--platform") {
		t.Fatalf("build cmd=%v", res.BuildCmd)
	}
	if !strings.Contains(strings.Join(res.PushCmd, " "), "docker push") {
		t.Fatalf("push cmd=%v", res.PushCmd)
	}
	if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err != nil {
		t.Fatal(err)
	}
}

func TestPushQuietAddsProgressPlain(t *testing.T) {
	res, err := Push(t.Context(), PushRequest{
		Image:  "us-central1-docker.pkg.dev/p/runhug/llama-server:cuda",
		Dir:    t.TempDir(),
		DryRun: true,
		Quiet:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(res.BuildCmd, " "), "--progress=plain") {
		t.Fatalf("%v", res.BuildCmd)
	}
}

func TestPushRequiresImage(t *testing.T) {
	_, err := Push(t.Context(), PushRequest{DryRun: true})
	if err == nil || !strings.Contains(err.Error(), "image required") {
		t.Fatalf("want image required, got %v", err)
	}
}
