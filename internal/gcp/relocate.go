package gcp

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// CapacityStockout is a ZONE_RESOURCE_POOL_EXHAUSTED start/create failure.
// Zones are the ones Google reported as having capacity, in that order.
type CapacityStockout struct {
	Zones       []string
	MachineType string
	Accelerator string
	AccelCount  int
}

var (
	stockoutZones = regexp.MustCompile(`zonesAvailable:\s*([a-z0-9]+(?:-[a-z0-9]+)+(?:,[a-z0-9]+(?:-[a-z0-9]+)+)*)`)
	stockoutVM    = regexp.MustCompile(`vmType:\s*([a-z0-9-]+)`)
	stockoutGPU   = regexp.MustCompile(`attachment:\s*([a-z0-9-]+):(\d+)`)
)

// ParseCapacityStockout reports zones Google says can take the VM.
// ok is false for any other error.
func ParseCapacityStockout(err error) (CapacityStockout, bool) {
	if err == nil {
		return CapacityStockout{}, false
	}
	msg := err.Error()
	if !strings.Contains(msg, "ZONE_RESOURCE_POOL_EXHAUSTED") && !strings.Contains(msg, "resource_availability") {
		return CapacityStockout{}, false
	}
	zm := stockoutZones.FindStringSubmatch(msg)
	if len(zm) < 2 {
		return CapacityStockout{}, false
	}
	var zones []string
	for _, z := range strings.Split(zm[1], ",") {
		z = strings.TrimSpace(z)
		if z != "" {
			zones = append(zones, z)
		}
	}
	if len(zones) == 0 {
		return CapacityStockout{}, false
	}
	out := CapacityStockout{Zones: zones, AccelCount: 1}
	if vm := stockoutVM.FindStringSubmatch(msg); len(vm) == 2 {
		out.MachineType = vm[1]
	}
	if gpu := stockoutGPU.FindStringSubmatch(msg); len(gpu) == 3 {
		out.Accelerator = gpu[1]
		fmt.Sscanf(gpu[2], "%d", &out.AccelCount)
		if out.AccelCount < 1 {
			out.AccelCount = 1
		}
	}
	return out, true
}

// RelocateResult is a VM recreated in DestZone from a machine image of the stopped source.
// The source instance is deleted when Warning is nil.
type RelocateResult struct {
	Zone    string
	Warning error
}

// RelocateStoppedInstance clones a TERMINATED VM into destZone and deletes the source.
// gcloud's instances move command is gone; a machine image is the supported replacement
// and keeps the boot disk (docker layers and the downloaded GGUF).
func (c *Client) RelocateStoppedInstance(ctx context.Context, project, zone, name, destZone string, spec CapacityStockout) (RelocateResult, error) {
	project = strings.TrimSpace(project)
	zone = strings.TrimSpace(zone)
	name = strings.TrimSpace(name)
	destZone = strings.TrimSpace(destZone)
	if project == "" || zone == "" || name == "" || destZone == "" {
		return RelocateResult{}, fmt.Errorf("relocate requires project, zone, instance, and destination zone")
	}
	if destZone == zone {
		return RelocateResult{}, fmt.Errorf("destination zone %s is the source zone", destZone)
	}
	image := relocateImageName(name)
	if _, err := c.runner().Run(ctx,
		"compute", "machine-images", "create", image,
		"--source-instance="+name,
		"--source-instance-zone="+zone,
		"--project="+project,
		"--quiet",
	); err != nil {
		return RelocateResult{}, fmt.Errorf("machine image of %s: %w", name, err)
	}
	defer func() {
		cctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_, _ = c.runner().Run(cctx, "compute", "machine-images", "delete", image, "--project="+project, "--quiet")
	}()

	args := []string{
		"compute", "instances", "create", name,
		"--project=" + project,
		"--zone=" + destZone,
		"--source-machine-image=" + image,
		"--provisioning-model=SPOT",
		"--instance-termination-action=STOP",
		"--maintenance-policy=TERMINATE",
		"--quiet",
	}
	if mt := strings.TrimSpace(spec.MachineType); mt != "" {
		args = append(args, "--machine-type="+mt)
	}
	// G2 machine types include the L4. T4 is an extra accelerator and its URL is zonal,
	// so the machine image's us-central1-a accelerator cannot be reused in another zone.
	if spec.Accelerator != "" && !strings.HasPrefix(spec.MachineType, "g2-") {
		n := spec.AccelCount
		if n < 1 {
			n = 1
		}
		args = append(args, fmt.Sprintf("--accelerator=type=%s,count=%d", spec.Accelerator, n))
	}
	if _, err := c.runner().Run(ctx, args...); err != nil {
		return RelocateResult{}, fmt.Errorf("create %s in %s: %w", name, destZone, err)
	}
	res := RelocateResult{Zone: destZone}
	if _, err := c.runner().Run(ctx,
		"compute", "instances", "delete", name,
		"--project="+project,
		"--zone="+zone,
		"--quiet",
	); err != nil {
		res.Warning = fmt.Errorf("new VM is in %s; delete the old one in %s failed: %w", destZone, zone, err)
	}
	return res, nil
}

func relocateImageName(instance string) string {
	sum := sha256.Sum256([]byte(instance))
	return fmt.Sprintf("runhug-mv-%d-%x", time.Now().Unix(), sum[:3])
}
