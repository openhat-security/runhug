package gcp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/adamsiwiec1/runhug/internal/runtime"
)

// TunnelOpts configures an SSH local-forward tunnel to llama-server on the VM.
// Soft lock: llama binds 127.0.0.1 on the guest; IAP TCP to the NIC cannot reach
// loopback, so we forward via SSH (-L) instead of start-iap-tunnel.
type TunnelOpts struct {
	Project    string
	Zone       string
	Instance   string
	RemotePort int
	LocalPort  int
	// ThroughIAP forces --tunnel-through-iap (required for no-external-IP VMs).
	ThroughIAP bool
}

func (o TunnelOpts) withDefaults() TunnelOpts {
	if o.RemotePort <= 0 {
		o.RemotePort = ServerPort
	}
	if o.LocalPort <= 0 {
		o.LocalPort = LocalTunnelPort
	}
	return o
}

// TunnelArgs returns gcloud compute ssh args that open a local port forward.
// SSH options force non-interactive mode so background tunnels cannot hang on
// host-key prompts when stdin is not a TTY.
func TunnelArgs(o TunnelOpts) []string {
	o = o.withDefaults()
	fwd := fmt.Sprintf("%d:127.0.0.1:%d", o.LocalPort, o.RemotePort)
	args := []string{
		"compute", "ssh", o.Instance,
		"--project=" + o.Project,
		"--zone=" + o.Zone,
		"--quiet",
	}
	if o.ThroughIAP {
		args = append(args, "--tunnel-through-iap")
	}
	args = append(args,
		"--",
		"-N",
		"-L", fwd,
		"-o", "BatchMode=yes",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=3",
	)
	return args
}

// LocalOpenAIURL is the OpenAI-compatible base after the tunnel is up.
func LocalOpenAIURL(localPort int) string {
	if localPort <= 0 {
		localPort = LocalTunnelPort
	}
	return fmt.Sprintf("http://127.0.0.1:%d/v1", localPort)
}

// TunnelHandle is a background SSH local-forward started by StartTunnelBackground.
type TunnelHandle struct {
	Cmd       *exec.Cmd
	LocalPort int
	BaseURL   string

	mu     sync.Mutex
	stderr bytes.Buffer
	errCh  chan error
	waited bool
}

// Stop kills the background tunnel process (no-op if nil / already exited).
func (h *TunnelHandle) Stop() {
	if h == nil || h.Cmd == nil || h.Cmd.Process == nil {
		return
	}
	_ = h.Cmd.Process.Kill()
	h.waitOnce()
}

func (h *TunnelHandle) waitOnce() error {
	if h == nil || h.errCh == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.waited {
		return nil
	}
	h.waited = true
	return <-h.errCh
}

func (h *TunnelHandle) stderrText() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return strings.TrimSpace(h.stderr.String())
}

// StartTunnel runs gcloud compute ssh -N -L in the foreground.
func (c *Client) StartTunnel(ctx context.Context, o TunnelOpts) error {
	o = o.withDefaults()
	if err := EnsureProject(o.Project); err != nil {
		return err
	}
	if o.Instance == "" || o.Zone == "" {
		return fmt.Errorf("instance and zone required for tunnel")
	}
	args := TunnelArgs(o)
	bin := tunnelBinary(c)
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// StartTunnelBackground starts the SSH tunnel detached and returns a handle.
// Caller should WaitLocal / probe OpenAI, then Stop() when done (or leave it up).
func (c *Client) StartTunnelBackground(ctx context.Context, o TunnelOpts) (*TunnelHandle, error) {
	o = o.withDefaults()
	if err := EnsureProject(o.Project); err != nil {
		return nil, err
	}
	if o.Instance == "" || o.Zone == "" {
		return nil, fmt.Errorf("instance and zone required for tunnel")
	}
	if err := RequireGCloud(); err != nil {
		return nil, err
	}
	args := TunnelArgs(o)
	bin := tunnelBinary(c)
	cmd := exec.Command(bin, args...) //nolint:gosec // gcloud args from TunnelOpts
	h := &TunnelHandle{
		Cmd:       cmd,
		LocalPort: o.LocalPort,
		BaseURL:   LocalOpenAIURL(o.LocalPort),
		errCh:     make(chan error, 1),
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = &h.stderr
	runtime.DetachProcess(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start gcp tunnel: %w", err)
	}
	go func() {
		h.errCh <- cmd.Wait()
	}()
	_ = ctx
	return h, nil
}

// WaitLocal blocks until the local forward port accepts TCP connections.
func (h *TunnelHandle) WaitLocal(ctx context.Context, d time.Duration) error {
	if h == nil {
		return fmt.Errorf("nil tunnel handle")
	}
	deadline := time.Now().Add(d)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if runtime.PortOpen(h.LocalPort) {
			return nil
		}
		select {
		case err := <-h.errCh:
			h.mu.Lock()
			h.waited = true
			h.mu.Unlock()
			msg := h.stderrText()
			if msg != "" {
				return fmt.Errorf("gcp tunnel exited before 127.0.0.1:%d opened: %s", h.LocalPort, truncateTunnelErr(msg))
			}
			if err != nil {
				return fmt.Errorf("gcp tunnel exited before 127.0.0.1:%d opened: %w", h.LocalPort, err)
			}
			return fmt.Errorf("gcp tunnel exited before 127.0.0.1:%d opened", h.LocalPort)
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	msg := h.stderrText()
	if msg != "" {
		return fmt.Errorf("gcp tunnel: nothing listening on 127.0.0.1:%d — %s", h.LocalPort, truncateTunnelErr(msg))
	}
	return fmt.Errorf("gcp tunnel: nothing listening on 127.0.0.1:%d (ssh still connecting? try: runhug gcp tunnel)", h.LocalPort)
}

func truncateTunnelErr(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 400 {
		return s
	}
	return s[:400] + "…"
}

func tunnelBinary(c *Client) string {
	bin := "gcloud"
	if r, ok := c.runner().(DefaultRunner); ok && r.Bin != "" {
		bin = r.Bin
	}
	return bin
}
