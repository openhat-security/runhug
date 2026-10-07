package gcp

import (
	"context"
	"strings"
	"testing"
)

func TestGuestExecArgs(t *testing.T) {
	args := GuestExecArgs(GuestOpts{
		Project:  "p",
		Zone:     "us-central1-a",
		Instance: "runhug-x",
		Command:  "nvidia-smi",
	})
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"compute ssh runhug-x",
		"--project=p",
		"--zone=us-central1-a",
		"--quiet",
		"BatchMode=yes",
		"--command=nvidia-smi",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %s", want, joined)
		}
	}
	if strings.Contains(joined, "--tunnel-through-iap") {
		t.Fatalf("IAP should be opt-in: %s", joined)
	}
	iap := GuestExecArgs(GuestOpts{
		Project: "p", Zone: "z", Instance: "i", Command: "true", ThroughIAP: true,
	})
	if !strings.Contains(strings.Join(iap, " "), "--tunnel-through-iap") {
		t.Fatalf("%v", iap)
	}
}

func TestGuestMetricsAndLogsCommands(t *testing.T) {
	m := GuestMetricsCommand()
	if !strings.Contains(m, "nvidia-smi") || !strings.Contains(m, "nproc") {
		t.Fatal(m)
	}
	l := GuestLogsCommand(40)
	if !strings.Contains(l, "docker logs --tail 40 "+LlamaContainer) {
		t.Fatal(l)
	}
	if GuestLogsCommand(0) == "" || !strings.Contains(GuestLogsCommand(99999), "2000") {
		t.Fatal("lines clamp")
	}
	boot := GuestBootCommand()
	if !strings.Contains(boot, "RH_NO_DOCKER") || !strings.Contains(boot, LlamaContainer) {
		t.Fatal(boot)
	}
	c := GuestSetContextCommand(32768)
	if !strings.Contains(c, "CONTEXT=\"$N\"") || !strings.Contains(c, "N=32768") || !strings.Contains(c, "sudo docker run") {
		t.Fatal(c)
	}
}

func TestSummarizeGuestLogsNeedsEgress(t *testing.T) {
	raw := "RH_NO_DOCKER\nrunhug: docker not available after install\nNetwork is unreachable"
	if !GuestNeedsEgress(raw) {
		t.Fatal("expected egress")
	}
	s := SummarizeGuestLogs(raw)
	if !strings.Contains(s, "no internet") {
		t.Fatalf("%q", s)
	}
}

func TestHasCloudNATFromRouters(t *testing.T) {
	f := &fakeRunner{jsonPayload: `[{"name":"r1","region":"https://www.googleapis.com/compute/v1/projects/p/regions/us-central1","nats":[{"name":"nat"}]}]`}
	c := &Client{Runner: f}
	ok, err := c.HasCloudNAT(context.Background(), "p", "us-central1")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	f2 := &fakeRunner{jsonPayload: `[]`}
	c2 := &Client{Runner: f2}
	ok, err = c2.HasCloudNAT(context.Background(), "p", "us-central1")
	if err != nil || ok {
		t.Fatalf("empty routers should be false, got %v %v", ok, err)
	}
}

func TestGuestExecUsesRunner(t *testing.T) {
	f := &fakeRunner{}
	c := &Client{Runner: f}
	_, err := c.GuestExec(context.Background(), GuestOpts{
		Project: "p", Zone: "z", Instance: "i", Command: "echo hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("calls=%v", f.calls)
	}
	if !strings.Contains(strings.Join(f.calls[0], " "), "--command=echo hi") {
		t.Fatalf("%v", f.calls[0])
	}
}
