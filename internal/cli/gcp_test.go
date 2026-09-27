package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestStripProviderFlag(t *testing.T) {
	got := stripProviderFlag([]string{"--provider", "gcp", "org/model", "--dry-run"})
	if strings.Join(got, " ") != "org/model --dry-run" {
		t.Fatalf("%v", got)
	}
	got = stripProviderFlag([]string{"--provider=gcp", "--yes", "m"})
	if strings.Join(got, " ") != "--yes m" {
		t.Fatalf("%v", got)
	}
}

func TestGCPHelp(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	printUsage(&buf)
	s := buf.String()
	if !strings.Contains(s, "gcp") {
		t.Fatal("usage should mention gcp")
	}
	if !strings.Contains(s, "--provider gcp") {
		t.Fatal("usage should mention --provider gcp")
	}
}

func TestGCPDockerfileCommand(t *testing.T) {
	// smoke: command is registered
	err := Run([]string{"gcp", "help"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPeekProvider(t *testing.T) {
	v, ok := peekProvider([]string{"--provider", "gcp", "m"})
	if !ok || v != "gcp" {
		t.Fatalf("%v %v", v, ok)
	}
	v, ok = peekProvider([]string{"--provider=runpod"})
	if !ok || v != "runpod" {
		t.Fatalf("%v %v", v, ok)
	}
	if _, ok := peekProvider([]string{"--dry-run"}); ok {
		t.Fatal("expected absent")
	}
}
