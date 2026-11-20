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
		"ConnectTimeout=20",
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

func TestSummarizeTunnelErrDropsTraceback(t *testing.T) {
	raw := `WARNING:

To increase the performance of the tunnel, consider installing NumPy. For instructions,
please see https://cloud.google.com/iap/docs/using-tcp-forwarding#increasing_the_tcp_upload_bandwidth

ERROR: [0] Error during local connection to [stdin]: Error while connecting [4003: 'failed to connect to backend']. (Failed to connect to port 22)
Traceback (most recent call last):
  File "/opt/homebrew/lib/python3.11/site-packages/googlecloudsdk/core/util/files.py", line 1, in <module>
    raise SystemExit
`
	got := summarizeTunnelErr(raw)
	if strings.Contains(got, "NumPy") || strings.Contains(got, "Traceback") || strings.Contains(got, "File ") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "Failed to connect to port 22") {
		t.Fatalf("got %q", got)
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
