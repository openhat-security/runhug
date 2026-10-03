package gcp

import (
	"context"
	"strings"
	"testing"
)

func TestStartInstanceArgs(t *testing.T) {
	f := &fakeRunner{}
	c := &Client{Runner: f}
	if err := c.StartInstance(context.Background(), "proj", "us-central1-a", "runhug-x"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("calls=%v", f.calls)
	}
	joined := strings.Join(f.calls[0], " ")
	if !strings.Contains(joined, "instances start runhug-x") || !strings.Contains(joined, "--project=proj") {
		t.Fatalf("%s", joined)
	}
}
