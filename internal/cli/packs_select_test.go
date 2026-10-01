package cli

import (
	"testing"

	"github.com/adamsiwiec1/runhug/internal/packs"
)

func TestParseCategorySelection(t *testing.T) {
	cats := packs.DefaultCategories()
	ids, err := parseCategorySelection("1,3", cats)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "text-generation" || ids[1] != "text-to-image" {
		t.Fatalf("%v", ids)
	}
	ids, err = parseCategorySelection("1-2", cats)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "text-generation" || ids[1] != "vision" {
		t.Fatalf("%v", ids)
	}
	ids, err = parseCategorySelection("all", cats)
	if err != nil || len(ids) != len(cats) {
		t.Fatalf("%v %v", ids, err)
	}
	ids, err = parseCategorySelection("none", cats)
	if err != nil || len(ids) != 0 {
		t.Fatalf("%v %v", ids, err)
	}
	ids, err = parseCategorySelection("gguf", cats)
	if err != nil || len(ids) != 1 || ids[0] != "gguf" {
		t.Fatalf("%v %v", ids, err)
	}
	ids, err = parseCategorySelection("llm", cats)
	if err != nil || len(ids) != 1 || ids[0] != "text-generation" {
		t.Fatalf("alias llm: %v %v", ids, err)
	}
}
