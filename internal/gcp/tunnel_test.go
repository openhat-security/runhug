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
	for _, want := range []string{
		"18080:127.0.0.1:8080",
		"BatchMode=yes",
		"ExitOnForwardFailure=yes",
		"StrictHostKeyChecking=accept-new",
		"--quiet",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("want %q in %s", want, joined)
		}
	}
	if strings.Contains(joined, "--tunnel-through-iap") {
		t.Fatalf("IAP should be opt-in: %s", joined)
	}
	iap := TunnelArgs(TunnelOpts{
		Project: "p", Zone: "z", Instance: "i", ThroughIAP: true,
	})
	if !strings.Contains(strings.Join(iap, " "), "--tunnel-through-iap") {
		t.Fatalf("missing IAP flag: %v", iap)
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
