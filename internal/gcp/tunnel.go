package gcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
}

func (o TunnelOpts) withDefaults() TunnelOpts {
	if o.RemotePort <= 0 {
		o.RemotePort = ServerPort
	}
	if o.LocalPort <= 0 {
		o.LocalPort = ServerPort
	}
	return o
}

// TunnelArgs returns gcloud compute ssh args that open a local port forward.
func TunnelArgs(o TunnelOpts) []string {
	o = o.withDefaults()
	fwd := fmt.Sprintf("%d:127.0.0.1:%d", o.LocalPort, o.RemotePort)
	return []string{
		"compute", "ssh", o.Instance,
		"--project=" + o.Project,
		"--zone=" + o.Zone,
		"--",
		"-N",
		"-L", fwd,
	}
}

// LocalOpenAIURL is the OpenAI-compatible base after the tunnel is up.
func LocalOpenAIURL(localPort int) string {
	if localPort <= 0 {
		localPort = ServerPort
	}
	return fmt.Sprintf("http://127.0.0.1:%d/v1", localPort)
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
	bin := "gcloud"
	if r, ok := c.runner().(DefaultRunner); ok && r.Bin != "" {
		bin = r.Bin
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
