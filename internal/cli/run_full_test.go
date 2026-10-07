package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/adamsiwiec1/runhug/internal/store"
)

func TestWriteFullHeaderHasCwdModelInstance(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	s := &runSession{
		Target: EndpointTarget{
			Model:   "DavidAU/Qwen3.5-9B-The-Defiant-Fable-Uncensored-Heretic-NEO-IMATRIX-MAX-MTP-GGUF",
			Source:  "gcp ssh tunnel",
			BaseURL: "http://127.0.0.1:8080/v1",
		},
		Model: store.Model{
			HFRepo: "org/m", Backend: store.BackendGCP, PodID: "runhug-davidau-qwen3-instance",
		},
		Cwd: "/tmp/runhug-cli",
	}
	var buf bytes.Buffer
	n := writeFullHeader(&buf, s, 56)
	if n < 8 {
		t.Fatalf("header rows %d", n)
	}
	out := buf.String()
	for _, want := range []string{"runhug-cli", "/help", "/mini", "Ctrl-D", "model", "instance", "runhug-davidau-qwen3-instance"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
	// Keys sit on the right of the wordmark.
	lines := strings.Split(out, "\n")
	helpAt := -1
	artAt := -1
	for _, line := range lines {
		if strings.Contains(line, "/help") {
			helpAt = strings.Index(line, "/help")
		}
		if i := strings.Index(line, "____"); i >= 0 && artAt < 0 {
			artAt = i
		}
	}
	if helpAt >= 0 && artAt >= 0 && helpAt < artAt {
		t.Fatalf("expected /help on the right of the logo, help col %d art col %d\n%s", helpAt, artAt, out)
	}
}

func TestFullBannerArtAligned(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	s := &runSession{}
	s.setMetricsCache(hostMetrics{
		GPUs:       []gpuSnap{{Name: "Tesla T4", Util: 0, MemUsed: 6649, MemTot: 15360, Temp: 63}},
		MemTotalKB: 14 * 1024 * 1024, MemAvailKB: 10 * 1024 * 1024,
		Nproc: 4, Load1: 0.23,
	})
	lines := fullBannerLines(s, 100)
	banner := strings.Split(bannerASCII, "\n")
	start := -1
	wantCol := -1
	artW := 0
	for _, art := range banner {
		if l := len(art); l > artW {
			artW = l
		}
	}
	wantCol = (100 - artW) / 2
	for i, art := range banner {
		pos := strings.Index(lines[i], art)
		if pos < 0 {
			t.Fatalf("logo line %d missing from %q", i, lines[i])
		}
		col := visibleLen(lines[i][:pos])
		if start < 0 {
			start = col
		}
		if col != start {
			t.Fatalf("logo skewed: line %d col %d want %d\n%s", i, col, start, strings.Join(lines, "\n"))
		}
		if col != wantCol {
			t.Fatalf("logo not centered: col %d want %d\n%s", col, wantCol, strings.Join(lines, "\n"))
		}
	}
	if !strings.Contains(lines[0], "/help") || !strings.Contains(lines[1], "/mini") || !strings.Contains(lines[2], "Ctrl-D") {
		t.Fatalf("keys:\n%s", strings.Join(lines, "\n"))
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Tesla T4") || !strings.Contains(joined, "gpu") {
		t.Fatalf("metrics should sit left of logo:\n%s", joined)
	}
	ctxAt := strings.Index(lines[0], "ctx")
	helpAt := strings.Index(lines[0], "/help")
	if ctxAt < 0 || helpAt < 0 || ctxAt >= wantCol || helpAt <= wantCol {
		t.Fatalf("expected metrics left and /help right: ctx=%d help=%d art=%d\n%s", ctxAt, helpAt, wantCol, joined)
	}
}

func TestWriteFullFooterVersionOnly(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	writeFullFooter(&buf, &runSession{Cwd: "/tmp/x"}, 80, 24)
	out := buf.String()
	if !strings.Contains(out, "runhug") || !strings.Contains(out, "ctx") {
		t.Fatalf("%s", out)
	}
	if strings.Contains(out, "/mini") || strings.Contains(out, "/tmp/x") {
		t.Fatalf("cwd/mini belong in header, got %s", out)
	}
}
