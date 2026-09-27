package gcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

// TunnelOpts configures an IAP TCP tunnel to llama-server on the VM.
type TunnelOpts struct {
	Project   string
	Zone      string
	Instance  string
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

// TunnelArgs returns gcloud args for start-iap-tunnel (OpenAI on 127.0.0.1).
func TunnelArgs(o TunnelOpts) []string {
	o = o.withDefaults()
	return []string{
		"compute", "start-iap-tunnel", o.Instance, strconv.Itoa(o.RemotePort),
		"--project=" + o.Project,
		"--zone=" + o.Zone,
		fmt.Sprintf("--local-host-port=localhost:%d", o.LocalPort),
	}
}

// LocalOpenAIURL is the OpenAI-compatible base after the tunnel is up.
func LocalOpenAIURL(localPort int) string {
	if localPort <= 0 {
		localPort = ServerPort
	}
	return fmt.Sprintf("http://127.0.0.1:%d/v1", localPort)
}

// StartTunnel runs gcloud compute start-iap-tunnel in the foreground.
func (c *Client) StartTunnel(ctx context.Context, o TunnelOpts) error {
	o = o.withDefaults()
	if err := EnsureProject(o.Project); err != nil {
		return err
	}
	if o.Instance == "" || o.Zone == "" {
		return fmt.Errorf("instance and zone required for IAP tunnel")
	}
	args := TunnelArgs(o)
	// Prefer attaching stdio so the user sees tunnel logs.
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
