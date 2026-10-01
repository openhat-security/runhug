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
	full := gcpHelpText()
	if !strings.Contains(full, "--image") {
		t.Fatal("gcp help should mention --image")
	}
	var fullBuf bytes.Buffer
	printGCPHelpFull(&fullBuf)
	if !strings.Contains(fullBuf.String(), "when") {
		t.Fatal("gcp --help-full should include when-to-use detail")
	}
}

func TestGCPDockerfileCommand(t *testing.T) {
	// smoke: command is registered
	err := Run([]string{"gcp", "help"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGCPHelpMentionsPush(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	s := gcpHelpText()
	for _, want := range []string{"push", "dockerfile", "deploy", "--image", "--estimate", "tunnel"} {
		if !strings.Contains(s, want) {
			t.Fatalf("gcp help missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "soft locks") || strings.Contains(s, "Phase 1") {
		t.Fatalf("gcp help should stay short:\n%s", s)
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
