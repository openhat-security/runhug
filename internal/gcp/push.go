package gcp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// PushRequest builds the thin llama-server image locally and pushes it to a
// registry (Artifact Registry / GCR). No Cloud Build.
type PushRequest struct {
	// Image is the full destination tag (required), e.g.
	// us-central1-docker.pkg.dev/PROJECT/runhug/llama-server:cuda
	Image string
	// Dir is where Dockerfile + entrypoint.sh are written. Empty → temp dir.
	Dir string
	// Config drives generated Dockerfile/entrypoint (optional model hints).
	Config ImageConfig
	// Platform defaults to linux/amd64 (GCE Spot).
	Platform string
	// DryRun writes files and prints docker commands without running them.
	DryRun bool
	// Out receives docker stdout+stderr. Nil uses the process TTY (verbose).
	Out io.Writer
	// OnStep is called with "build" then "push" around the docker commands.
	OnStep func(string)
	// Quiet adds --progress=plain so output is log-friendly (no TTY spinner).
	Quiet bool
}

// PushResult is the outcome of a local build/push.
type PushResult struct {
	Dir      string
	Image    string
	Platform string
	BuildCmd []string
	PushCmd  []string
}

// DefaultImagePlatform is the GCE Spot target.
const DefaultImagePlatform = "linux/amd64"

// Push builds and pushes the thin wrap image with local docker (no Cloud Build).
func Push(ctx context.Context, req PushRequest) (*PushResult, error) {
	image := strings.TrimSpace(req.Image)
	if image == "" {
		return nil, fmt.Errorf("image required: pass --image REGION-docker.pkg.dev/PROJECT/runhug/llama-server:TAG")
	}
	platform := strings.TrimSpace(req.Platform)
	if platform == "" {
		platform = DefaultImagePlatform
	}

	dir := strings.TrimSpace(req.Dir)
	cleanup := func() {}
	if dir == "" {
		tmp, err := os.MkdirTemp("", "runhug-gcp-img-*")
		if err != nil {
			return nil, err
		}
		dir = tmp
		cleanup = func() { _ = os.RemoveAll(tmp) }
	}
	if err := WriteImageFiles(dir, req.Config); err != nil {
		cleanup()
		return nil, err
	}

	buildCmd := []string{"docker", "build", "--platform", platform, "-t", image}
	if req.Quiet {
		buildCmd = append(buildCmd, "--progress=plain")
	}
	buildCmd = append(buildCmd, "-f", filepath.Join(dir, "Dockerfile"), dir)
	pushCmd := []string{"docker", "push", image}

	res := &PushResult{
		Dir:      dir,
		Image:    image,
		Platform: platform,
		BuildCmd: buildCmd,
		PushCmd:  pushCmd,
	}

	if req.DryRun {
		return res, nil
	}

	if _, err := exec.LookPath("docker"); err != nil {
		cleanup()
		return nil, fmt.Errorf("docker not on PATH — install Docker Desktop / Engine, then retry (local build only; no Cloud Build)")
	}

	out := req.Out
	if req.OnStep != nil {
		req.OnStep("build")
	}
	if err := runDocker(ctx, out, buildCmd[1:]...); err != nil {
		cleanup()
		return nil, fmt.Errorf("docker build: %w", err)
	}
	if req.OnStep != nil {
		req.OnStep("push")
	}
	if err := runDocker(ctx, out, pushCmd[1:]...); err != nil {
		cleanup()
		return nil, fmt.Errorf("docker push: %w", err)
	}
	return res, nil
}

func runDocker(ctx context.Context, out io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = nil
	var stderr bytes.Buffer
	if out != nil {
		cmd.Stdout = out
		cmd.Stderr = io.MultiWriter(out, &stderr)
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = &teeWriter{a: os.Stderr, b: &stderr}
	}
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

type teeWriter struct {
	a, b interface{ Write([]byte) (int, error) }
}

func (t *teeWriter) Write(p []byte) (int, error) {
	n, err := t.a.Write(p)
	_, _ = t.b.Write(p)
	return n, err
}
