package packs

import "testing"

func TestLookupTypeAliases(t *testing.T) {
	for _, id := range []string{"llm", "LLM", "text-generation", "chat"} {
		spec, ok := LookupType(id)
		if !ok || spec.ID != "text-generation" {
			t.Fatalf("%q → %+v ok=%v", id, spec, ok)
		}
	}
	spec, ok := LookupType("vision")
	if !ok || len(spec.Pipelines) < 2 {
		t.Fatalf("vision: %+v", spec)
	}
	_, _, ok = ExpandType("gguf")
	if !ok {
		t.Fatal("gguf")
	}
	pipes, filt, ok := ExpandType("gguf")
	if !ok || filt != "gguf" || len(pipes) != 0 {
		t.Fatalf("gguf expand pipes=%v filt=%q", pipes, filt)
	}
}

func TestResolveTypeAndTask(t *testing.T) {
	pipes, filt, err := ResolveTypeAndTask("llm", "any")
	if err != "" || filt != "" || len(pipes) != 1 || pipes[0] != "text-generation" {
		t.Fatalf("llm/any: %v %q %q", pipes, filt, err)
	}
	pipes, _, err = ResolveTypeAndTask("vision", "image-to-text")
	if err != "" || len(pipes) != 1 || pipes[0] != "image-to-text" {
		t.Fatalf("vision/image-to-text: %v %q", pipes, err)
	}
	_, _, err = ResolveTypeAndTask("llm", "text-to-image")
	if err == "" {
		t.Fatal("expected incompatible")
	}
	pipes, _, err = ResolveTypeAndTask("", "summarization")
	if err != "" || len(pipes) != 1 || pipes[0] != "summarization" {
		t.Fatalf("task-only: %v %q", pipes, err)
	}
}

func TestDefaultCategoriesExpanded(t *testing.T) {
	ids := CategoryIDs()
	want := map[string]bool{
		"text-generation": true, "vision": true, "text-to-image": true,
		"video": true, "audio": true, "embeddings": true, "gguf": true,
	}
	if len(ids) != len(want) {
		t.Fatalf("%v", ids)
	}
	for _, id := range ids {
		if !want[id] {
			t.Fatalf("unexpected %s in %v", id, ids)
		}
	}
}
