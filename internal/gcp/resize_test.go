package gcp

import (
	"context"
	"strings"
	"testing"
)

type resizeSeqRunner struct {
	calls    [][]string
	statuses []string
	i        int
}

func (f *resizeSeqRunner) Run(ctx context.Context, args ...string) (string, error) {
	cp := append([]string{}, args...)
	f.calls = append(f.calls, cp)
	return "", nil
}

func (f *resizeSeqRunner) RunJSON(ctx context.Context, dest any, args ...string) error {
	_, err := f.Run(ctx, args...)
	if st, ok := dest.(*InstanceStatus); ok {
		st.Name = "runhug-x"
		st.Status = "TERMINATED"
		if f.i < len(f.statuses) {
			st.Status = f.statuses[f.i]
			f.i++
		}
	}
	return err
}

func TestSetMachineTypeArgs(t *testing.T) {
	got := strings.Join(setMachineTypeArgs("p", "us-central1-a", "vm", "g2-standard-4"), " ")
	if !strings.Contains(got, "set-machine-type vm") || !strings.Contains(got, "--machine-type=g2-standard-4") {
		t.Fatal(got)
	}
}

func TestResizeInstanceL4(t *testing.T) {
	f := &resizeSeqRunner{statuses: []string{"TERMINATED", "RUNNING"}}
	c := &Client{Runner: f}
	err := c.ResizeInstance(context.Background(), "p", "us-central1-a", "vm", GPUTarget{
		Name: GPUTypeL4, MachineType: MachineTypeL4,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, call := range f.calls {
		joined += strings.Join(call, " ") + "\n"
	}
	if !strings.Contains(joined, "instances stop vm") {
		t.Fatalf("missing stop:\n%s", joined)
	}
	if !strings.Contains(joined, "set-machine-type vm") || !strings.Contains(joined, "g2-standard-4") {
		t.Fatalf("missing machine type:\n%s", joined)
	}
	if !strings.Contains(joined, "instances start vm") {
		t.Fatalf("missing start:\n%s", joined)
	}
	if !strings.Contains(joined, "--remove-guest-accelerators") {
		t.Fatalf("expected drop n1 accelerators on G2:\n%s", joined)
	}
	rm := strings.Index(joined, "--remove-guest-accelerators")
	set := strings.Index(joined, "set-machine-type")
	if rm < 0 || set < 0 || rm > set {
		t.Fatalf("must remove accelerators before set-machine-type:\n%s", joined)
	}
}

func TestResizeInstanceT4Accelerator(t *testing.T) {
	f := &resizeSeqRunner{statuses: []string{"STOPPED", "RUNNING"}}
	c := &Client{Runner: f}
	err := c.ResizeInstance(context.Background(), "p", "z", "vm", GPUTarget{
		Name: GPUTypeT4, MachineType: MachineTypeT4, Accelerator: "nvidia-tesla-t4", AcceleratorCnt: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, call := range f.calls {
		joined += strings.Join(call, " ") + "\n"
	}
	if !strings.Contains(joined, "--guest-accelerator=type=nvidia-tesla-t4,count=1") {
		t.Fatalf("%s", joined)
	}
	rm := strings.Index(joined, "--remove-guest-accelerators")
	set := strings.Index(joined, "set-machine-type")
	add := strings.Index(joined, "--guest-accelerator=")
	if rm < 0 || set < 0 || add < 0 || !(rm < set && set < add) {
		t.Fatalf("want remove → set-machine-type → add T4:\n%s", joined)
	}
}

func TestResizeInstanceA100DropsL4First(t *testing.T) {
	f := &resizeSeqRunner{statuses: []string{"TERMINATED", "RUNNING"}}
	c := &Client{Runner: f}
	err := c.ResizeInstance(context.Background(), "p", "us-central1-a", "vm", GPUTarget{
		Name: "A100", MachineType: "a2-highgpu-1g",
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, call := range f.calls {
		joined += strings.Join(call, " ") + "\n"
	}
	if strings.Contains(joined, "--guest-accelerator=") {
		t.Fatalf("A2 must not add a guest GPU:\n%s", joined)
	}
	rm := strings.Index(joined, "--remove-guest-accelerators")
	set := strings.Index(joined, "--machine-type=a2-highgpu-1g")
	if rm < 0 || set < 0 || rm > set {
		t.Fatalf("L4 accelerator must come off before A2:\n%s", joined)
	}
}
