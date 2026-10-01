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
