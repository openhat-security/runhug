package cli

import (
	"strings"
	"testing"
	"time"
)

func TestClassifyJobKey(t *testing.T) {
	if classifyJobKey('v') != "verbose" || classifyJobKey('V') != "verbose" {
		t.Fatal("v")
	}
	if classifyJobKey('h') != "hurry" {
		t.Fatal("h")
	}
	if classifyJobKey('c') != "cancel" || classifyJobKey(3) != "cancel" {
		t.Fatal("c")
	}
	if classifyJobKey('\n') != "" || classifyJobKey('x') != "" {
		t.Fatal("ignore")
	}
}

func TestJobLineRewritesInPlace(t *testing.T) {
	var b strings.Builder
	line := newJobLine(&b)
	line.update("build")
	line.update("build 8s")
	line.finish()
	line.update("ignored")
	if got, want := b.String(), "build\r\033[Kbuild 8s\r\n"; got != want {
		t.Fatalf("got %q", got)
	}
}

func TestJobLinePauseDoesNotEraseNextOutput(t *testing.T) {
	var b strings.Builder
	line := newJobLine(&b)
	line.update("build")
	line.pause()
	line.update("tick")
	if got, want := b.String(), "build\r\n"; got != want {
		t.Fatalf("got %q", got)
	}
	line.resume()
	line.update("build 16s")
	line.finish()
	if got, want := b.String(), "build\r\nbuild\r\033[Kbuild 16s\r\n"; got != want {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeCRLF(t *testing.T) {
	if got := string(normalizeCRLF([]byte("a\nb\r\nc"))); got != "a\r\nb\r\nc" {
		t.Fatalf("got %q", got)
	}
	if got := string(normalizeCRLF([]byte("none"))); got != "none" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatJobStatusSingleLine(t *testing.T) {
	line := formatJobStatus(time.Now().Add(-8*time.Second), 3, 4, "build", "cuda image")
	if strings.Contains(line, "\n") || strings.Contains(line, "\r") {
		t.Fatalf("status must stay one line: %q", line)
	}
	if !strings.Contains(line, "3/4") || !strings.Contains(line, "cuda image") || !strings.Contains(line, "8s") {
		t.Fatalf("got %q", line)
	}
	if !strings.Contains(line, "verbose") {
		t.Fatalf("hint dropped on a wide terminal: %q", line)
	}
}
