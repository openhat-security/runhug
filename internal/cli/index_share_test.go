package cli

import (
	"strings"
	"testing"

	"github.com/adamsiwiec1/runhug/internal/packs"
)

func TestSearchHelpMentionsType(t *testing.T) {
	var b strings.Builder
	printSearchHelp(&b)
	s := b.String()
	for _, want := range []string{"--type", "--task", "--online"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in %s", want, s)
		}
	}
	for _, bad := range []string{"--index", "--share"} {
		if strings.Contains(s, bad) {
			t.Fatalf("unexpected %q in %s", bad, s)
		}
	}
}

func TestResolveTypeTaskViaPacks(t *testing.T) {
	_, _, errMsg := packs.ResolveTypeAndTask("vision", "text-generation")
	if errMsg == "" {
		t.Fatal("expected incompatible")
	}
}
