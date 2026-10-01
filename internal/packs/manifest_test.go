package packs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adamsiwiec1/runhug/internal/hf"
	"github.com/adamsiwiec1/runhug/internal/index"
)

func TestParseManifest(t *testing.T) {
	raw := []byte(`{
  "version": 1,
  "generated_at": "2026-09-13T12:00:00Z",
  "packs": [
    {
      "id": "text-generation",
      "title": "Text Generation (LLMs)",
      "pipeline": "text-generation",
      "rows": 100,
      "size_bytes": 1234,
      "sha256": "abc",
      "db_filename": "index-text-generation.db",
      "watermark": "2026-09-13T11:00:00Z"
    }
  ]
}`)
	m, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != 1 || len(m.Packs) != 1 {
		t.Fatalf("got %+v", m)
	}
	p, ok := m.FindPack("text-generation")
	if !ok || p.Rows != 100 || p.DBFilename != "index-text-generation.db" {
		t.Fatalf("pack: %+v", p)
	}
}

func TestFileSHA256AndVerify(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.bin")
	if err := os.WriteFile(path, []byte("hello-packs"), 0644); err != nil {
		t.Fatal(err)
	}
	sum, err := FileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum) != 64 {
		t.Fatalf("sha len %d", len(sum))
	}
	if err := VerifySHA256(path, sum); err != nil {
		t.Fatal(err)
	}
	if err := VerifySHA256(path, strings.ToUpper(sum)); err != nil {
		t.Fatal(err)
	}
	if err := VerifySHA256(path, "0"+sum[1:]); err == nil {
		t.Fatal("expected mismatch")
	}
}

func TestUpsertAndWatermarkAdvance(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "models.db")
	idx, err := index.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	models := []hf.Model{
		{
			ID:           "org/a",
			Author:       "org",
			Description:  "first",
			Tags:         []string{"tag:a"},
			Likes:        1,
			Downloads:    10,
			LibraryName:  "transformers",
			PipelineTag:  "text-generation",
			LastModified: "2026-01-01T00:00:00Z",
		},
		{
			ID:           "org/b",
			Author:       "org",
			Description:  "second",
			Tags:         []string{"tag:b"},
			Likes:        2,
			Downloads:    20,
			LibraryName:  "transformers",
			PipelineTag:  "text-generation",
			LastModified: "2026-06-01T00:00:00Z",
		},
	}
	n, maxLM, err := UpsertModels(idx, models)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("n=%d", n)
	}
	want := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if !maxLM.Equal(want) {
		t.Fatalf("maxLM=%v want %v", maxLM, want)
	}

	// Idempotent upsert with newer watermark
	models[0].LastModified = "2026-08-01T00:00:00Z"
	models[0].Likes = 99
	n, maxLM, err = UpsertModels(idx, models[:1])
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("n=%d", n)
	}
	want = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if !maxLM.Equal(want) {
		t.Fatalf("maxLM=%v", maxLM)
	}
	count, err := idx.Count()
	if err != nil || count != 2 {
		t.Fatalf("count=%d err=%v", count, err)
	}

	wm := formatWatermark(maxLM)
	if err := idx.SetMetadata(MetadataKeyWatermark("text-generation"), wm); err != nil {
		t.Fatal(err)
	}
	got, err := idx.GetMetadata(MetadataKeyWatermark("text-generation"))
	if err != nil || got != wm {
		t.Fatalf("wm meta %q err=%v", got, err)
	}
}

func TestMergePackDB(t *testing.T) {
	dir := t.TempDir()
	packPath := filepath.Join(dir, "index-text-generation.db")
	src, err := index.Open(packPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := src.InsertModel(hf.Model{
		ID: "x/y", Author: "x", PipelineTag: "text-generation",
		LastModified: "2026-03-01T00:00:00Z", Downloads: 5,
	}); err != nil {
		t.Fatal(err)
	}
	src.Close()

	destPath := filepath.Join(dir, "models.db")
	dest, err := index.Open(destPath)
	if err != nil {
		t.Fatal(err)
	}
	defer dest.Close()

	n, wm, err := MergePackDB(dest, packPath)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || wm == "" {
		t.Fatalf("n=%d wm=%q", n, wm)
	}
	count, _ := dest.Count()
	if count != 1 {
		t.Fatalf("count=%d", count)
	}
}

func TestApplyDeltaJSONL(t *testing.T) {
	dir := t.TempDir()
	delta := filepath.Join(dir, "delta.jsonl")
	lines := `{"id":"a/b","author":"a","pipeline_tag":"text-generation","lastModified":"2026-04-01T00:00:00Z","likes":3}
{"id":"c/d","author":"c","pipeline_tag":"text-generation","lastModified":"2026-05-01T00:00:00Z","likes":4}
`
	if err := os.WriteFile(delta, []byte(lines), 0644); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "models.db")
	idx, err := index.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	n, wm, err := ApplyDeltaJSONL(idx, delta)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || !strings.HasPrefix(wm, "2026-05-01") {
		t.Fatalf("n=%d wm=%q", n, wm)
	}
}

func TestDefaultCategories(t *testing.T) {
	ids := CategoryIDs()
	want := []string{"text-generation", "vision", "text-to-image", "video", "audio", "embeddings", "gguf"}
	if len(ids) != len(want) {
		t.Fatalf("%v", ids)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("%v", ids)
		}
	}
}

func TestWriteLoadManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ManifestFilename)
	m := &Manifest{
		Version: ManifestVersion,
		Packs: []PackInfo{{
			ID: "gguf", Title: "GGUF", Filter: "gguf", Rows: 10,
			SHA256: "deadbeef", DBFilename: "index-gguf.db", Watermark: "2026-01-01T00:00:00Z",
		}},
	}
	if err := WriteManifest(path, m); err != nil {
		t.Fatal(err)
	}
	got, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Packs) != 1 || got.Packs[0].ID != "gguf" {
		t.Fatalf("%+v", got)
	}
}
