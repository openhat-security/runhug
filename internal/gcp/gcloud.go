package gcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Runner executes gcloud. Tests swap this for a fake.
type Runner interface {
	Run(ctx context.Context, args ...string) (stdout string, err error)
	RunJSON(ctx context.Context, dest any, args ...string) error
}

// DefaultRunner shells out to the gcloud binary on PATH.
type DefaultRunner struct {
	Bin string
	Env []string // optional extra env; nil = inherit
}

func (r DefaultRunner) bin() string {
	if r.Bin != "" {
		return r.Bin
	}
	return "gcloud"
}

func (r DefaultRunner) Run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, r.bin(), args...)
	if r.Env != nil {
		cmd.Env = r.Env
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), fmt.Errorf("gcloud %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

func (r DefaultRunner) RunJSON(ctx context.Context, dest any, args ...string) error {
	out, err := r.Run(ctx, args...)
	if err != nil {
		return err
	}
	out = strings.TrimSpace(out)
	if out == "" || out == "[]" || out == "null" {
		// leave dest zero / empty slice
		if dest != nil {
			_ = json.Unmarshal([]byte(out), dest)
		}
		return nil
	}
	if err := json.Unmarshal([]byte(out), dest); err != nil {
		return fmt.Errorf("gcloud json: %w\n%s", err, truncate(out, 400))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// LookPath reports whether gcloud is installed.
func LookPath() (string, error) {
	return exec.LookPath("gcloud")
}

// RequireGCloud returns a clear error when gcloud is missing.
func RequireGCloud() error {
	if _, err := LookPath(); err != nil {
		return fmt.Errorf("gcloud not on PATH — install the Google Cloud SDK (https://cloud.google.com/sdk/docs/install), then run `gcloud auth application-default login`")
	}
	return nil
}

// NewClient returns a Client that shells out to gcloud.
func NewClient() *Client {
	return &Client{Runner: DefaultRunner{}}
}

// Client is the Phase-1 GCP provider surface (gcloud + ADC).
type Client struct {
	Runner Runner
}

func (c *Client) runner() Runner {
	if c == nil || c.Runner == nil {
		return DefaultRunner{}
	}
	return c.Runner
}

// HasADC probes application-default credentials without printing the token.
func (c *Client) HasADC(ctx context.Context) bool {
	_, err := c.runner().Run(ctx, "auth", "application-default", "print-access-token")
	return err == nil
}

// RequireADC ensures least-privilege Application Default Credentials are present.
func (c *Client) RequireADC(ctx context.Context) error {
	if err := RequireGCloud(); err != nil {
		return err
	}
	if c.HasADC(ctx) {
		return nil
	}
	return fmt.Errorf("GCP Application Default Credentials missing — run `gcloud auth application-default login` (least-privilege user ADC; no service-account key files)")
}

// CurrentAccount returns the active gcloud account email, if any.
func (c *Client) CurrentAccount(ctx context.Context) string {
	out, err := c.runner().Run(ctx, "config", "get-value", "account")
	if err != nil {
		return ""
	}
	a := strings.TrimSpace(out)
	if a == "(unset)" || a == "" {
		return ""
	}
	return a
}

// CurrentProject returns the active gcloud config project, if any.
func (c *Client) CurrentProject(ctx context.Context) string {
	out, err := c.runner().Run(ctx, "config", "get-value", "project")
	if err != nil {
		return ""
	}
	p := strings.TrimSpace(out)
	if p == "(unset)" || p == "" {
		return ""
	}
	return p
}

// Project is a GCP project from `gcloud projects list`.
type Project struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	State     string `json:"lifecycleState"`
}

// ListProjects returns accessible projects (requires an authenticated gcloud account).
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	var projects []Project
	err := c.runner().RunJSON(ctx, &projects,
		"projects", "list",
		"--format=json(projectId,name,lifecycleState)",
	)
	if err != nil {
		return nil, err
	}
	var live []Project
	for _, p := range projects {
		if p.State == "" || p.State == "ACTIVE" {
			live = append(live, p)
		}
	}
	return live, nil
}

// EnsureProject validates project is non-empty (never hardcoded by runhug).
func EnsureProject(project string) error {
	project = strings.TrimSpace(project)
	if project == "" {
		return fmt.Errorf("GCP project required — pass --project or pick one interactively (runhug never hardcodes a project)")
	}
	return nil
}

// WriteTempFile writes data to a temp file and returns its path.
func WriteTempFile(pattern string, data []byte) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}
