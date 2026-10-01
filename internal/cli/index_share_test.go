package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/adamsiwiec1/runhug/internal/hf"
	"github.com/adamsiwiec1/runhug/internal/index"
	"github.com/adamsiwiec1/runhug/internal/packs"
)

func TestResolveShare(t *testing.T) {
	yes, err := resolveShare("true")
	if err != nil || !yes {
		t.Fatalf("true: %v %v", yes, err)
	}
	no, err := resolveShare("false")
	if err != nil || no {
		t.Fatalf("false: %v %v", no, err)
	}
	_, err = resolveShare("maybe")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSharePackContributionDryRun(t *testing.T) {
	t.Setenv("RUNHUG_SHARE_DRY_RUN", "1")
	dir := t.TempDir()
	packPath := filepath.Join(dir, "text-generation.db")
	idx, err := index.Open(packPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = idx.InsertModelWithPack(hf.Model{ID: "org/a", PipelineTag: "text-generation", Likes: 1}, "text-generation")
	idx.Close()

	if err := sharePackContribution("text-generation", packPath, "aero", 1); err != nil {
		t.Fatal(err)
	}
}

func TestIndexSearchPoolNoShare(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUNHUG_CONFIG", "")

	models := []hf.Model{
		{ID: "org/a", PipelineTag: "text-generation", Likes: 3, Downloads: 100},
		{ID: "org/b", PipelineTag: "text-generation", Likes: 4, Downloads: 200},
	}
	if err := indexSearchPool(models, "text-generation", "aero", "false"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(xdg, "runhug", "models.db")
	idx, err := index.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	n, _ := idx.Count()
	if n != 2 {
		t.Fatalf("count=%d path=%s", n, path)
	}
	ok, _ := idx.HasMembership("org/a", "text-generation")
	if !ok {
		t.Fatal("membership")
	}
}

func TestSearchHelpMentionsType(t *testing.T) {
	var b strings.Builder
	printSearchHelp(&b)
	s := b.String()
	for _, want := range []string{"--type", "--index", "--share", "--task"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in %s", want, s)
		}
	}
}

func TestResolveTypeTaskViaPacks(t *testing.T) {
	_, _, errMsg := packs.ResolveTypeAndTask("vision", "text-generation")
	if errMsg == "" {
		t.Fatal("expected incompatible")
	}
}
