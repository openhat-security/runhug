package gcp

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

const stockoutErr = `gcloud compute instances start vm --project=otw-portal-dev --zone=us-central1-a: ERROR: (gcloud.compute.instances.start) ---
code: ZONE_RESOURCE_POOL_EXHAUSTED_WITH_DETAILS
errorInfo:
    metadatas:
      attachment: nvidia-tesla-t4:1
      vmType: n1-standard-4
      zone: us-central1-a
      zonesAvailable: us-central1-b,us-central1-c,us-central1-f
    reason: resource_availability`

func TestParseCapacityStockout(t *testing.T) {
	got, ok := ParseCapacityStockout(fmt.Errorf("%s", stockoutErr))
	if !ok {
		t.Fatal("expected stockout")
	}
	if strings.Join(got.Zones, ",") != "us-central1-b,us-central1-c,us-central1-f" {
		t.Fatalf("zones: %#v", got.Zones)
	}
	if got.MachineType != "n1-standard-4" || got.Accelerator != "nvidia-tesla-t4" || got.AccelCount != 1 {
		t.Fatalf("%#v", got)
	}
	if _, ok := ParseCapacityStockout(fmt.Errorf("permission denied")); ok {
		t.Fatal("unrelated error")
	}
}

func TestRelocateStoppedInstanceArgs(t *testing.T) {
	f := &fakeRunner{}
	c := &Client{Runner: f}
	res, err := c.RelocateStoppedInstance(context.Background(), "proj", "us-central1-a", "runhug-x", "us-central1-b", CapacityStockout{
		MachineType: "n1-standard-4",
		Accelerator: "nvidia-tesla-t4",
		AccelCount:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Zone != "us-central1-b" || res.Warning != nil {
		t.Fatalf("%#v", res)
	}
	if len(f.calls) != 4 {
		t.Fatalf("calls=%d %#v", len(f.calls), f.calls)
	}
	joined := func(i int) string { return strings.Join(f.calls[i], " ") }
	if !strings.Contains(joined(0), "machine-images create runhug-mv-") || !strings.Contains(joined(0), "--source-instance-zone=us-central1-a") {
		t.Fatalf("image: %s", joined(0))
	}
	create := joined(1)
	for _, want := range []string{
		"instances create runhug-x",
		"--zone=us-central1-b",
		"--source-machine-image=runhug-mv-",
		"--provisioning-model=SPOT",
		"--instance-termination-action=STOP",
		"--machine-type=n1-standard-4",
		"--accelerator=type=nvidia-tesla-t4,count=1",
	} {
		if !strings.Contains(create, want) {
			t.Fatalf("missing %q in %s", want, create)
		}
	}
	if !strings.Contains(joined(2), "instances delete runhug-x") || !strings.Contains(joined(2), "--zone=us-central1-a") {
		t.Fatalf("delete: %s", joined(2))
	}
	if !strings.Contains(joined(3), "machine-images delete runhug-mv-") {
		t.Fatalf("image delete: %s", joined(3))
	}
}

func TestRelocateG2SkipsAcceleratorFlag(t *testing.T) {
	f := &fakeRunner{}
	c := &Client{Runner: f}
	if _, err := c.RelocateStoppedInstance(context.Background(), "proj", "us-central1-a", "runhug-x", "us-central1-b", CapacityStockout{
		MachineType: "g2-standard-4",
		Accelerator: "nvidia-l4",
		AccelCount:  1,
	}); err != nil {
		t.Fatal(err)
	}
	create := strings.Join(f.calls[1], " ")
	if strings.Contains(create, "--accelerator=") {
		t.Fatalf("G2 should not pass a zonal accelerator: %s", create)
	}
}
