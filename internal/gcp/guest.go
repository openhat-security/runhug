package gcp

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// LlamaContainer is the docker --name used by the Spot startup script.
const LlamaContainer = "runhug-llama"

// GuestOpts configures a non-interactive gcloud compute ssh --command.
type GuestOpts struct {
	Project    string
	Zone       string
	Instance   string
	Command    string
	ThroughIAP bool
	Timeout    time.Duration // 0 → 45s
}

// GuestExecArgs builds gcloud args for a one-shot remote command.
func GuestExecArgs(o GuestOpts) []string {
	args := []string{
		"compute", "ssh", o.Instance,
		"--project=" + o.Project,
		"--zone=" + o.Zone,
		"--quiet",
		"--ssh-flag=-oBatchMode=yes",
		"--ssh-flag=-oStrictHostKeyChecking=accept-new",
		"--ssh-flag=-oConnectTimeout=20",
	}
	if o.ThroughIAP {
		args = append(args, "--tunnel-through-iap")
	}
	args = append(args, "--command="+o.Command)
	return args
}

// GuestExec runs command on the VM and returns combined stdout (stderr folded into error on failure).
func (c *Client) GuestExec(ctx context.Context, o GuestOpts) (string, error) {
	if strings.TrimSpace(o.Instance) == "" || strings.TrimSpace(o.Project) == "" || strings.TrimSpace(o.Zone) == "" {
		return "", fmt.Errorf("guest exec requires project, zone, and instance")
	}
	if strings.TrimSpace(o.Command) == "" {
		return "", fmt.Errorf("guest exec requires a command")
	}
	if err := EnsureProject(o.Project); err != nil {
		return "", err
	}
	if err := RequireGCloud(); err != nil {
		return "", err
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out, err := c.runner().Run(ctx, GuestExecArgs(o)...)
	if err == nil {
		return strings.TrimRight(out, "\n"), nil
	}
	if o.ThroughIAP {
		return "", err
	}
	// Retry via IAP (no-external-IP / firewall).
	o.ThroughIAP = true
	out2, err2 := c.runner().Run(ctx, GuestExecArgs(o)...)
	if err2 != nil {
		return "", fmt.Errorf("%v; IAP retry: %w", err, err2)
	}
	return strings.TrimRight(out2, "\n"), nil
}

// GuestSetContextCommand recreates runhug-llama with a new llama.cpp -c (CONTEXT).
func GuestSetContextCommand(n int) string {
	if n < 512 {
		n = 512
	}
	if n > 262144 {
		n = 262144
	}
	return fmt.Sprintf(`set -euo pipefail
NAME=%s
N=%d
if ! sudo docker inspect "$NAME" >/dev/null 2>&1; then
  echo NOCONTAINER
  exit 1
fi
IMG=$(sudo docker inspect -f '{{.Config.Image}}' "$NAME")
ARGS=()
while IFS= read -r line; do
  [ -z "$line" ] && continue
  case "$line" in
    CONTEXT=*) continue ;;
  esac
  ARGS+=(-e "$line")
done <<EOF
$(sudo docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$NAME")
EOF
sudo docker rm -f "$NAME" >/dev/null
sudo docker run -d --name "$NAME" --restart=no --gpus all --network host --privileged \
  -e CONTEXT="$N" ${ARGS[@]+"${ARGS[@]}"} "$IMG" >/dev/null
echo CONTEXT=$N
`, LlamaContainer, n)
}

// GuestMetricsCommand returns a parseable snapshot: GPUs, RAM, load, disk, uptime, docker.
func GuestMetricsCommand() string {
	return strings.Join([]string{
		`echo RH_GPU`,
		`nvidia-smi --query-gpu=index,name,utilization.gpu,utilization.memory,memory.used,memory.total,temperature.gpu,power.draw,power.limit --format=csv,noheader,nounits 2>/dev/null || echo none`,
		`echo RH_MEM`,
		`awk '/MemTotal:/{t=$2} /MemAvailable:/{a=$2} /MemFree:/{f=$2} /Buffers:/{b=$2} /^Cached:/{c=$2} /SwapTotal:/{st=$2} /SwapFree:/{sf=$2} END{print "total_kb=" t+0 " avail_kb=" a+0 " free_kb=" f+0 " buffers_kb=" b+0 " cached_kb=" c+0 " swap_total_kb=" st+0 " swap_free_kb=" sf+0}' /proc/meminfo 2>/dev/null || true`,
		`echo RH_LOAD`,
		`cat /proc/loadavg 2>/dev/null || true`,
		`echo RH_NPROC`,
		`nproc 2>/dev/null || echo 0`,
		`echo RH_DISK`,
		`df -kP / 2>/dev/null | awk 'NR==2{print "total_kb=" $2 " used_kb=" $3 " avail_kb=" $4 " pct=" $5 " mount=" $6}'`,
		`echo RH_UP`,
		`cut -d. -f1 /proc/uptime 2>/dev/null || echo 0`,
		`echo RH_DOCKER`,
		`docker stats --no-stream --format '{{.Name}}|{{.CPUPerc}}|{{.MemUsage}}|{{.MemPerc}}' 2>/dev/null | head -n 8 || true`,
	}, "; ")
}

// GuestLogsCommand tails docker logs for the llama container (and startup log).
func GuestLogsCommand(lines int) string {
	if lines <= 0 {
		lines = 80
	}
	if lines > 2000 {
		lines = 2000
	}
	return fmt.Sprintf(
		`docker logs --tail %d %s 2>&1; echo '--- startup ---'; tail -n %d /var/log/runhug-startup.log 2>/dev/null || true`,
		lines, LlamaContainer, lines,
	)
}

// GuestBootCommand reports whether docker exists and tails startup (no docker required).
func GuestBootCommand() string {
	return strings.Join([]string{
		`if command -v docker >/dev/null 2>&1; then echo RH_DOCKER_OK; docker ps -a --filter name=` + LlamaContainer + ` --format '{{.Names}} {{.Status}}' 2>/dev/null; docker logs --tail 25 ` + LlamaContainer + ` 2>&1 | tail -n 25; else echo RH_NO_DOCKER; fi`,
		`echo '--- startup ---'`,
		`tail -n 50 /var/log/runhug-startup.log 2>/dev/null || true`,
	}, "; ")
}

func latestStartupSlice(raw string) string {
	const mark = "runhug: startup begin"
	if i := strings.LastIndex(raw, mark); i >= 0 {
		return raw[i:]
	}
	return raw
}

func lastUsefulLine(raw string) string {
	lines := strings.Split(raw, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" || strings.HasPrefix(l, "---") || strings.HasPrefix(l, "RH_") {
			continue
		}
		if len(l) > 140 {
			return l[:137] + "…"
		}
		return l
	}
	return ""
}

// SummarizeGuestLogs is a one-line status from docker/startup output (never secrets).
func SummarizeGuestLogs(raw string) string {
	slice := latestStartupSlice(raw)
	low := strings.ToLower(slice)
	switch {
	case strings.Contains(raw, "RH_NO_DOCKER") && GuestNeedsEgress(raw):
		return "guest: no docker — VM has no internet (need public IP or Cloud NAT)"
	case strings.Contains(raw, "RH_NO_DOCKER"):
		return "guest: docker not installed yet"
	case strings.Contains(low, "fetching") || strings.Contains(low, "downloading"):
		return "guest: downloading GGUF"
	case strings.Contains(low, "llama-server") && strings.Contains(low, "idle"):
		return "guest: starting llama-server"
	case strings.Contains(low, "out of memory") || strings.Contains(low, "cuda_error"):
		return "guest: GPU OOM / CUDA error — try a smaller GGUF or GPU"
	}
	if s := lastUsefulLine(slice); s != "" {
		return "guest: " + s
	}
	return ""
}

// GuestNeedsEgress is true when startup failed because apt/HF/docker could not reach the internet.
func GuestNeedsEgress(raw string) bool {
	raw = latestStartupSlice(raw)
	for _, s := range []string{
		"docker not available after install",
		"Network is unreachable",
		"Could not connect to",
		"Package 'docker-ce' has no installation candidate",
		"Failed to fetch http://",
		"Failed to fetch https://",
	} {
		if strings.Contains(raw, s) {
			return true
		}
	}
	return false
}

type gceRouter struct {
	Name   string `json:"name"`
	Region string `json:"region"`
	Nats   []struct {
		Name string `json:"name"`
	} `json:"nats"`
}

// HasCloudNAT reports whether any Cloud Router in region has a NAT config.
func (c *Client) HasCloudNAT(ctx context.Context, project, region string) (bool, error) {
	if err := EnsureProject(project); err != nil {
		return false, err
	}
	region = strings.TrimSpace(region)
	if region == "" {
		return false, fmt.Errorf("region required")
	}
	var routers []gceRouter
	if err := c.runner().RunJSON(ctx, &routers,
		"compute", "routers", "list",
		"--project="+project,
		"--format=json(name,region,nats)",
	); err != nil {
		return false, err
	}
	for _, r := range routers {
		if len(r.Nats) == 0 {
			continue
		}
		if strings.Contains(r.Region, region) || strings.HasSuffix(r.Region, "/"+region) {
			return true, nil
		}
	}
	return false, nil
}

// EnsureExternalIP attaches an ephemeral access config. No-op if one already exists.
func (c *Client) EnsureExternalIP(ctx context.Context, project, zone, name string) error {
	_, err := c.runner().Run(ctx,
		"compute", "instances", "add-access-config", name,
		"--project="+project,
		"--zone="+zone,
		"--quiet",
	)
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "already has") || strings.Contains(msg, "Duplicate") {
		return nil
	}
	return err
}

// ResetInstance reboots the VM (re-runs the startup script).
func (c *Client) ResetInstance(ctx context.Context, project, zone, name string) error {
	_, err := c.runner().Run(ctx,
		"compute", "instances", "reset", name,
		"--project="+project,
		"--zone="+zone,
		"--quiet",
	)
	return err
}
