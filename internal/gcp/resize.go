package gcp

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func setMachineTypeArgs(project, zone, name, machine string) []string {
	return []string{
		"compute", "instances", "set-machine-type", name,
		"--project=" + project,
		"--zone=" + zone,
		"--machine-type=" + machine,
	}
}

func guestAcceleratorArgs(project, zone, name, accelType string, count int) []string {
	if count < 1 {
		count = 1
	}
	return []string{
		"compute", "instances", "update", name,
		"--project=" + project,
		"--zone=" + zone,
		fmt.Sprintf("--guest-accelerator=type=%s,count=%d", accelType, count),
	}
}

func removeGuestAcceleratorArgs(project, zone, name string) []string {
	return []string{
		"compute", "instances", "update", name,
		"--project=" + project,
		"--zone=" + zone,
		"--remove-guest-accelerators",
	}
}

// ResizeInstance stops a Spot VM, changes machine type (and n1 guest GPU), starts it.
func (c *Client) ResizeInstance(ctx context.Context, project, zone, name string, target GPUTarget) error {
	if strings.TrimSpace(target.MachineType) == "" {
		return fmt.Errorf("machine type required")
	}
	if err := c.StopInstance(ctx, project, zone, name); err != nil {
		return err
	}
	if err := c.waitInstanceDown(ctx, project, zone, name); err != nil {
		return err
	}
	// Drop attached GPUs before set-machine-type. G2 nvidia-l4 cannot move onto A2/A3/A4.
	_, _ = c.runner().Run(ctx, removeGuestAcceleratorArgs(project, zone, name)...)
	if _, err := c.runner().Run(ctx, setMachineTypeArgs(project, zone, name, target.MachineType)...); err != nil {
		return fmt.Errorf("set-machine-type: %w", err)
	}
	if strings.TrimSpace(target.Accelerator) != "" {
		if _, err := c.runner().Run(ctx, guestAcceleratorArgs(project, zone, name, target.Accelerator, target.AcceleratorCnt)...); err != nil {
			return fmt.Errorf("guest-accelerator: %w", err)
		}
	}
	if err := c.StartInstance(ctx, project, zone, name); err != nil {
		return err
	}
	_, err := c.WaitInstanceStatus(ctx, project, zone, name, "RUNNING", 5*time.Second)
	return err
}

func (c *Client) waitInstanceDown(ctx context.Context, project, zone, name string) error {
	every := 5 * time.Second
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		st, err := c.DescribeInstance(ctx, project, zone, name)
		if err != nil {
			return err
		}
		u := strings.ToUpper(st.Status)
		if u == "STOPPED" || u == "TERMINATED" {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w (last status %s)", ctx.Err(), st.Status)
		case <-time.After(every):
		}
	}
}
