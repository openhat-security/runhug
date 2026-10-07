package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/hf"
)

func TestParseSearchPick(t *testing.T) {
	act, idx, repo, err := parseSearchPick("3", 15)
	if err != nil || act != "inspect" || idx != 3 || repo != "" {
		t.Fatalf("%s %d %q %v", act, idx, repo, err)
	}
	act, idx, _, err = parseSearchPick("deploy 12", 15)
	if err != nil || act != "deploy" || idx != 12 {
		t.Fatalf("deploy %s %d %v", act, idx, err)
	}
	act, _, _, err = parseSearchPick("copy 1", 15)
	if err != nil || act != "copy" {
		t.Fatalf("copy %s %v", act, err)
	}
	act, _, _, err = parseSearchPick("", 15)
	if err != nil || act != "skip" {
		t.Fatalf("empty %s %v", act, err)
	}
	act, _, repo, err = parseSearchPick("org/model", 15)
	if err != nil || act != "inspect" || repo != "org/model" {
		t.Fatalf("repo %s %q %v", act, repo, err)
	}
	if _, _, _, err := parseSearchPick("99", 15); err == nil {
		t.Fatal("want range error")
	}
}

func TestResolveModelArgFromLastSearch(t *testing.T) {
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "registry.json"))
	saveLastSearch("cyber", []hf.Model{
		{ID: "a/one"},
		{ID: "b/two"},
		{ID: "c/three"},
	})
	got, err := resolveModelArg("3")
	if err != nil || got != "c/three" {
		t.Fatalf("%q %v", got, err)
	}
	got, err = resolveModelArg("org/keep")
	if err != nil || got != "org/keep" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := resolveModelArg("99"); err == nil {
		t.Fatal("unmapped row should error")
	}
}

func TestResolveModelArgNoLastSearch(t *testing.T) {
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "registry.json"))
	_, err := resolveModelArg("3")
	if err == nil || !strings.Contains(err.Error(), "no last search") {
		t.Fatalf("got %v", err)
	}
}
