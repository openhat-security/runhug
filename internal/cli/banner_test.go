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
		"search & index",
		"runpod",
		"local",
		"config",
		"update",
		"connect hf",
		"search nlp",
		"local index",
		"upgrade",
		"deploy it in minutes",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("help missing %q\n%s", want, out)
		}
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
	if !strings.Contains(out, "\nwizard\n") && !strings.HasSuffix(strings.TrimSpace(out), "wizard") {
		t.Fatalf("expected trailing wizard hint\n%s", out)
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
