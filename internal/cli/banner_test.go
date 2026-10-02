package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adamsiwiec1/runhug/internal/config"
)

func TestShowBannerRespectsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if showBanner() {
		t.Fatal("NO_COLOR should skip banner")
	}
}

func TestShowBannerRespectsSettingsNoColor(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RUNHUG_CONFIG", filepath.Join(dir, "registry.json"))
	t.Setenv("NO_COLOR", "")
	if err := config.SaveSettings(config.Settings{NoColor: true}); err != nil {
		t.Fatal(err)
	}
	if showBanner() {
		t.Fatal("settings.no_color should skip banner")
	}
}

func TestPrintBannerSkippedNonTTY(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if stdoutIsTTY() {
		t.Skip("stdout is a TTY")
	}
	var buf bytes.Buffer
	printBanner(&buf)
	if buf.Len() != 0 {
		t.Fatalf("non-TTY should skip banner, got %q", buf.String())
	}
}

func TestPrintUsageIncludesGroupedHelp(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	printUsage(&buf)
	out := buf.String()
	for _, want := range []string{
		"setup",
		"search",
		"deploy",
		"heretic",
		"run",
		"RunPod",
		"local",
		"config",
		"packs",
		"gpu",
		"update",
		"local index",
		"upgrade",
		"find + set gpu catalog",
		"find, deploy, and run",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("help missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "\nchat\n") {
		t.Fatal("root help should rename chat → run")
	}
	if strings.Contains(out, "connect [hf]") {
		t.Fatal("connect/disconnect belong under runhug config --help, not root help")
	}
	if strings.Contains(out, bannerASCII) {
		t.Fatal("NO_COLOR help must not include banner art")
	}
}

func TestBannerASCIIFits80Cols(t *testing.T) {
	for _, line := range strings.Split(bannerASCII, "\n") {
		if len(line) > 80 {
			t.Fatalf("banner line too wide (%d): %q", len(line), line)
		}
	}
}

func TestPrintUsageOmitsEnvDump(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	printUsage(&buf)
	out := buf.String()
	if strings.Contains(out, "Environment") {
		t.Fatal("default help must not dump Environment wall")
	}
	for _, leak := range []string{"HF_TOKEN", "RUNPOD_API_KEY", "RUNHUG_INDEX_LIMIT"} {
		if strings.Contains(out, leak) {
			t.Fatalf("default help must not list env %q", leak)
		}
	}
	if strings.Contains(out, "Tips") {
		t.Fatalf("default help must not include Tips block\n%s", out)
	}
	if !strings.Contains(out, "wizard") {
		t.Fatalf("expected wizard hint\n%s", out)
	}
	if strings.Contains(out, "--help-full") {
		t.Fatalf("bare usage must not suggest --help-full (only explicit --help)\n%s", out)
	}
	var helpBuf bytes.Buffer
	printUsage(&helpBuf, true)
	if !strings.Contains(helpBuf.String(), "--help-full") {
		t.Fatalf("explicit --help should point to --help-full\n%s", helpBuf.String())
	}
	if strings.Contains(out, "Tips") || strings.Contains(out, "New here?") {
		t.Fatalf("help must not include Tips block\n%s", out)
	}
	for _, leak := range []string{"gpus / import", "(guide", "(deployments)", "(serve)", "aliases:"} {
		if strings.Contains(out, leak) {
			t.Fatalf("root help must not include clutter %q\n%s", leak, out)
		}
	}
}

func TestPrintUsageFullHasDetail(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	printUsageFull(&buf)
	out := buf.String()
	for _, want := range []string{
		"Full command reference",
		"what",
		"when",
		"wizard",
		"deploy <model>",
		"RUNPOD_API_KEY",
		"HF_TOKEN",
		"typical flow",
		"--help",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("full help missing %q\n%s", want, out)
		}
	}
	if !strings.Contains(out, "Interactive first-run") && !strings.Contains(out, "first-run checklist") {
		t.Fatalf("full help should explain wizard\n%s", out)
	}
}

func TestHelpModeFromArgs(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{nil, ""},
		{[]string{"list"}, ""},
		{[]string{"--help"}, "short"},
		{[]string{"-h"}, "short"},
		{[]string{"help"}, "short"},
		{[]string{"help", "full"}, "full"},
		{[]string{"--help", "--full"}, "full"},
		{[]string{"--help-full"}, "full"},
		{[]string{"help-full"}, "full"},
	}
	for _, tc := range cases {
		if got := helpModeFromArgs(tc.args); got != tc.want {
			t.Fatalf("args=%v got %q want %q", tc.args, got, tc.want)
		}
	}
}

func TestGPUHelpFull(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var short, full strings.Builder
	printGPUHelp(&short)
	printGPUHelpFull(&full)
	if strings.Contains(short.String(), "--help-full") {
		t.Fatalf("short gpu help body should not embed details hint (showCmdHelp adds it)\n%s", short.String())
	}
	for _, want := range []string{"what", "when", "list [query]", "update", "examples", "Short list:"} {
		if !strings.Contains(full.String(), want) {
			t.Fatalf("gpu full help missing %q\n%s", want, full.String())
		}
	}
}
