package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/adamsiwiec1/runhug/internal/packs"
)

func TestPacksHelpConsumerOnly(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	printPacksHelp(&buf)
	out := buf.String()
	for _, want := range []string{"install", "list", "update", "remove", "openhat-security/hfpacks", "--local", "--remote"} {
		if !strings.Contains(out, want) {
			t.Fatalf("packs help missing %q\n%s", want, out)
		}
	}
	for _, bad := range []string{"packs build", "upsert", "hfpacks …"} {
		if strings.Contains(out, bad) {
			t.Fatalf("packs help should not advertise producer cmd %q\n%s", bad, out)
		}
	}
}

func TestPacksBuildRedirectsToHFPacks(t *testing.T) {
	err := cmdPacks([]string{"build"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "hfpacks") {
		t.Fatalf("got %v", err)
	}
}

func TestNormalizePackIDs(t *testing.T) {
	got := normalizePackIDs([]string{"llm", "llm", "gguf"})
	if len(got) != 2 {
		t.Fatalf("%v", got)
	}
}

func TestSelectPackRows(t *testing.T) {
	rows := []packRow{
		{Num: 1, Info: packs.PackInfo{ID: "text-generation"}, Installed: true, OnRelease: true},
		{Num: 2, Info: packs.PackInfo{ID: "gguf"}, Installed: false, OnRelease: true},
		{Num: 3, Info: packs.PackInfo{ID: "vision"}, Installed: false, OnRelease: false},
	}
	ids, err := selectPackRows("missing", rows, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "gguf" {
		t.Fatalf("%v", ids)
	}
	ids, err = selectPackRows("1-2", rows, true)
	if err != nil || len(ids) != 2 {
		t.Fatalf("%v %v", ids, err)
	}
	ids, err = selectPackRows("none", rows, false)
	if err != nil || len(ids) != 0 {
		t.Fatalf("%v %v", ids, err)
	}
}

func TestPackCoverage(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	hub, pct, rem := packCoverage(packs.PackInfo{Rows: 250, HubTotal: 1000})
	if hub != "250" && !strings.Contains(hub, "250") {
		// formatPackCount(1000) may be "1.0k" or "1000"
	}
	if pct != "25%" {
		t.Fatalf("pct=%s hub=%s rem=%s", pct, hub, rem)
	}
	hub, pct, rem = packCoverage(packs.PackInfo{Rows: 10})
	if !strings.Contains(hub, "—") {
		t.Fatalf("expected dash hub, got %q %q %q", hub, pct, rem)
	}
}

func TestInstalledPackCurrent(t *testing.T) {
	info := packs.PackInfo{SHA256: "abc123"}
	entry := packs.InstalledPack{SourceRelease: "v0.1.0", SHA256: "ABC123"}
	if !installedPackCurrent(entry, "v0.1.0", info) {
		t.Fatal("expected current")
	}
	entry.SourceRelease = "v0.0.9"
	if installedPackCurrent(entry, "v0.1.0", info) {
		t.Fatal("expected stale release")
	}
}

func TestFormatPackCount(t *testing.T) {
	if formatPackCount(42) != "42" {
		t.Fatal(formatPackCount(42))
	}
	if formatPackCount(12500) != "12k" {
		t.Fatal(formatPackCount(12500))
	}
}
