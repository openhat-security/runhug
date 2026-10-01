package index

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/adamsiwiec1/runhug/internal/hf"
)

func TestWatermarkAndMaxLastModified(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.db")
	idx, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	if err := idx.InsertModel(hf.Model{
		ID: "a/b", PipelineTag: "text-generation",
		LastModified: "2026-02-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	max, err := idx.MaxLastModified()
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if !max.Equal(want) {
		t.Fatalf("max=%v", max)
	}
	if err := idx.SetMetadata("watermark", "2026-03-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	wm, err := idx.Watermark()
	if err != nil {
		t.Fatal(err)
	}
	if !wm.Equal(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("wm=%v", wm)
	}
}

func TestAllModelsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.db")
	idx, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.InsertModel(hf.Model{
		ID: "org/model", Author: "org", Description: "desc",
		Tags: []string{"foo"}, Likes: 1, Downloads: 2,
		LibraryName: "gguf", PipelineTag: "text-generation",
		LastModified: "2026-01-15T12:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	idx.Close()

	ro, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	models, err := ro.AllModels()
	if err != nil || len(models) != 1 || models[0].ID != "org/model" {
		t.Fatalf("%v %v", models, err)
	}
}

func TestPackMembershipAndPipelineTags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.db")
	idx, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	a := hf.Model{ID: "org/a", PipelineTag: "text-generation", Likes: 10}
	b := hf.Model{ID: "org/b", PipelineTag: "image-to-text", Likes: 5}
	if err := idx.InsertModelWithPack(a, "text-generation"); err != nil {
		t.Fatal(err)
	}
	if err := idx.InsertModelWithPack(b, "vision"); err != nil {
		t.Fatal(err)
	}
	ok, err := idx.HasMembership("org/a", "text-generation")
	if err != nil || !ok {
		t.Fatalf("membership a: %v %v", ok, err)
	}
	// Re-upsert same membership is idempotent
	if err := idx.AddMembership("org/a", "text-generation"); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	got, err := idx.Search(ctx, "", SearchFilters{PackType: "vision", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "org/b" {
		t.Fatalf("pack filter: %+v", got)
	}

	got, err = idx.Search(ctx, "", SearchFilters{
		PipelineTags: []string{"text-generation", "image-to-text"}, Limit: 10, Sort: "likes",
	})
	if err != nil || len(got) != 2 {
		t.Fatalf("pipeline OR: %d %v", len(got), err)
	}
}

func TestCountByAndMembership(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.db")
	idx, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	models := []hf.Model{
		{ID: "a/1", LibraryName: "transformers", PipelineTag: "text-generation", Tags: []string{"license:mit"}},
		{ID: "a/2", LibraryName: "gguf", PipelineTag: "text-generation", Tags: []string{"license:apache-2.0"}},
		{ID: "a/3", LibraryName: "transformers", PipelineTag: "text-to-image", Tags: []string{"license:mit"}},
	}
	for _, m := range models {
		if err := idx.InsertModel(m); err != nil {
			t.Fatal(err)
		}
	}
	if err := idx.AddMembership("a/1", "text-generation"); err != nil {
		t.Fatal(err)
	}
	if err := idx.AddMembership("a/3", "text-to-image"); err != nil {
		t.Fatal(err)
	}

	libs, err := idx.CountBy("library_name")
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 2 || libs[0].Label != "transformers" || libs[0].Count != 2 {
		t.Fatalf("libs=%+v", libs)
	}
	pipes, err := idx.CountBy("pipeline_tag")
	if err != nil {
		t.Fatal(err)
	}
	if len(pipes) != 2 {
		t.Fatalf("pipes=%+v", pipes)
	}
	packs, err := idx.MembershipCounts()
	if err != nil || len(packs) != 2 {
		t.Fatalf("packs=%+v err=%v", packs, err)
	}
}
