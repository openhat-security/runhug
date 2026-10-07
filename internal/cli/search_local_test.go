package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adamsiwiec1/runhug/internal/hf"
	"github.com/adamsiwiec1/runhug/internal/index"
)

func stubNoHub(t *testing.T) {
	t.Helper()
	orig := liveHubSearchFn
	t.Cleanup(func() { liveHubSearchFn = orig })
	liveHubSearchFn = func(ctx context.Context, client *hf.Client, opts hf.SearchOpts, sortKey string, displayLimit int, wantSemantic bool) ([]hf.Model, string, error) {
		t.Fatal("live Hub search must not run when a local index exists")
		return nil, "", errNoSearchIndex()
	}
}

func withIndexPaths(t *testing.T, user, bundled string) {
	t.Helper()
	origUser, origBundled := userIndexPathFn, bundledIndexPathFn
	t.Cleanup(func() {
		userIndexPathFn = origUser
		bundledIndexPathFn = origBundled
	})
	userIndexPathFn = func() string { return user }
	bundledIndexPathFn = func() string { return bundled }
}

func writeTestIndex(t *testing.T, models []hf.Model) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "models.db")
	idx, err := index.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	for _, m := range models {
		if err := idx.InsertModel(m); err != nil {
			t.Fatalf("insert %s: %v", m.ID, err)
		}
	}
	return path
}

func TestSearchModelsUsesLocalIndexWithoutHub(t *testing.T) {
	stubNoHub(t)
	path := writeTestIndex(t, []hf.Model{
		{ID: "lab/offsec-hacking-model", Likes: 42, Downloads: 100, Description: "offsec pentest helper", Tags: []string{"text-generation"}},
		{ID: "acme/chat", Likes: 999, Downloads: 5000, Description: "general chat", Tags: []string{"text-generation"}},
	})
	withIndexPaths(t, path, "")

	models, meta, err := searchModels(context.Background(), searchRequest{
		Query:           "offsec hacking model",
		Sort:            "likes",
		Limit:           10,
		Task:            "any",
		DisableSemantic: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(meta.RankSource, "local") {
		t.Fatalf("rank source %q", meta.RankSource)
	}
	if len(models) == 0 {
		t.Fatal("expected local hits")
	}
	found := false
	for _, m := range models {
		if m.ID == "lab/offsec-hacking-model" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing offsec model: %#v", ids(models))
	}
}

func TestSearchModelsSortLikesUsesLocalPool(t *testing.T) {
	stubNoHub(t)
	path := writeTestIndex(t, []hf.Model{
		{ID: "lab/alpha-offsec", Likes: 10, Downloads: 50, Description: "offsec", Tags: []string{"text-generation"}},
		{ID: "lab/beta-offsec", Likes: 80, Downloads: 10, Description: "offsec", Tags: []string{"text-generation"}},
		{ID: "lab/gamma-offsec", Likes: 40, Downloads: 90, Description: "offsec", Tags: []string{"text-generation"}},
	})
	withIndexPaths(t, path, "")

	models, meta, err := searchModels(context.Background(), searchRequest{
		Query:           "offsec",
		Sort:            "likes",
		Limit:           10,
		Task:            "any",
		DisableSemantic: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(meta.RankSource, "likes") {
		t.Fatalf("rank source %q", meta.RankSource)
	}
	if len(models) < 3 {
		t.Fatalf("got %d", len(models))
	}
	if models[0].ID != "lab/beta-offsec" || models[1].ID != "lab/gamma-offsec" || models[2].ID != "lab/alpha-offsec" {
		t.Fatalf("likes order: %v", ids(models))
	}

	models, _, err = searchModels(context.Background(), searchRequest{
		Query:           "offsec",
		Sort:            "downloads",
		Limit:           10,
		Task:            "any",
		DisableSemantic: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if models[0].ID != "lab/gamma-offsec" {
		t.Fatalf("downloads order: %v", ids(models))
	}
}

func TestSearchModelsMissingIndexSuggestsUpdate(t *testing.T) {
	stubNoHub(t)
	missing := filepath.Join(t.TempDir(), "missing.db")
	withIndexPaths(t, missing, "")

	_, _, err := searchModels(context.Background(), searchRequest{
		Query:           "offsec hacking model",
		Limit:           5,
		DisableSemantic: true,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"runhug packs install", "--online", "--hub"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("missing %q in %q", want, msg)
		}
	}
}

func TestSearchModelsOnlineBypassesLocalIndex(t *testing.T) {
	path := writeTestIndex(t, []hf.Model{
		{ID: "local/only", Likes: 1, Description: "offsec", Tags: []string{"text-generation"}},
	})
	withIndexPaths(t, path, "")
	orig := liveHubSearchFn
	t.Cleanup(func() { liveHubSearchFn = orig })
	called := false
	liveHubSearchFn = func(ctx context.Context, client *hf.Client, opts hf.SearchOpts, sortKey string, displayLimit int, wantSemantic bool) ([]hf.Model, string, error) {
		called = true
		return []hf.Model{{ID: "hub/live", Likes: 2}}, "", nil
	}

	models, meta, err := searchModels(context.Background(), searchRequest{
		Query:           "offsec",
		Limit:           5,
		Online:          true,
		DisableSemantic: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected Hub search")
	}
	if len(models) != 1 || models[0].ID != "hub/live" {
		t.Fatalf("got %#v", models)
	}
	if !strings.Contains(meta.RankSource, "hub") {
		t.Fatalf("rank source %q", meta.RankSource)
	}
}

func TestSearchAndPrintUsesLocalIndex(t *testing.T) {
	stubNoHub(t)
	path := writeTestIndex(t, []hf.Model{
		{ID: "lab/offsec-hacking-model", Likes: 42, Description: "offsec", Tags: []string{"text-generation"}},
	})
	withIndexPaths(t, path, "")
	if err := searchAndPrint("offsec", hubOpts{Sort: "likes", Limit: 5}); err != nil {
		t.Fatal(err)
	}
}

func ids(models []hf.Model) []string {
	out := make([]string, len(models))
	for i, m := range models {
		out[i] = m.ID
	}
	return out
}
