package gcp

import (
	"strings"
	"testing"
)

func TestTunnelArgsDefaultLocalPort(t *testing.T) {
	args := TunnelArgs(TunnelOpts{
		Project:  "p",
		Zone:     "us-central1-a",
		Instance: "runhug-x",
	})
	joined := strings.Join(args, " ")
	want := "18080:127.0.0.1:8080"
	if !strings.Contains(joined, want) {
		t.Fatalf("want forward %s in %s", want, joined)
	}
}

func TestLocalOpenAIURL(t *testing.T) {
	if got := LocalOpenAIURL(0); got != "http://127.0.0.1:18080/v1" {
		t.Fatalf("%s", got)
	}
	if got := LocalOpenAIURL(8080); got != "http://127.0.0.1:8080/v1" {
		t.Fatalf("%s", got)
	}
}
