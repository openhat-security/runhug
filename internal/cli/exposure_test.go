package cli

import (
	"strings"
	"testing"

	"github.com/adamsiwiec1/runhug/internal/netguard"
)

func TestStartRefusesRemoteBridgeWithoutOptIn(t *testing.T) {
	t.Setenv(netguard.EnvAllowRemote, "")
	t.Setenv(netguard.EnvAPIKey, "")
	err := cmdStart([]string{"--bridge-host", "0.0.0.0", "--no-launch", "--base-url", "http://127.0.0.1:1/v1", "claude", "m"})
	if err == nil || !strings.Contains(err.Error(), "allow-remote") {
		t.Fatalf("want refusal mentioning --allow-remote, got %v", err)
	}
}

func TestStartRefusesRemoteBridgeWithoutKey(t *testing.T) {
	t.Setenv(netguard.EnvAPIKey, "")
	err := cmdStart([]string{"--bridge-host", "0.0.0.0", "--allow-remote", "--no-launch", "--base-url", "http://127.0.0.1:1/v1", "claude", "m"})
	if err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("want missing-key refusal, got %v", err)
	}
}
