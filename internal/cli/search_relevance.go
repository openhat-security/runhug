package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/hf"
	"github.com/adamsiwiec1/runhug/internal/index"
	"github.com/adamsiwiec1/runhug/internal/packs"
	"github.com/adamsiwiec1/runhug/internal/runtime"
	"github.com/adamsiwiec1/runhug/internal/semantic"
)

type searchRequest struct {
	Query           string
	Author          string
	Task            string // resolved Hub pipeline_tag or any
	Type            string // --type id/alias (broad pack bucket)
	Library         string
	Filter          string
	License         string
	Engine          string
	Sort            string
	Limit           int
	DisableSemantic bool
	// Online forces a live Hub API search. Default search never hits the Hub
	// when a local or bundled SQLite index exists.
	Online bool
}

type searchMeta struct {
	RankSource string
	Queries    []string
	Notes      string
	// Pool is the ranked result set before display trim.
	Pool []hf.Model
	// PackID is the canonical type id when --type was set.
	PackID string
}

type hubSearchFn func(ctx context.Context, client *hf.Client, opts hf.SearchOpts, sortKey string, displayLimit int, wantSemantic bool) ([]hf.Model, string, error)

// Test hooks. Production uses the real path helpers and Hub search.
var (
	userIndexPathFn                = indexFilePath
	bundledIndexPathFn             = bundledIndexPath
	liveHubSearchFn    hubSearchFn = searchRanked
)

func errNoSearchIndex() error {
	return fmt.Errorf("no local search index found.\nRun `runhug packs install` or `runhug init` to download packs from hfpacks Releases.\nOr pass --online / --hub for a live Hub search (rate-limited; set HF_TOKEN).")
}

func resolveSearchIndex() (path, source string) {
	if p := userIndexPathFn(); p != "" && index.Exists(p) {
		return p, "local index"
	}
	if p := bundledIndexPathFn(); p != "" && index.Exists(p) {
		return p, "bundled index"
	}
	return "", ""
}

func searchModels(ctx context.Context, req searchRequest) ([]hf.Model, searchMeta, error) {
	sortKey, err := hf.NormalizeSort(req.Sort)
	if err != nil {
		return nil, searchMeta{}, err
	}
	if req.Limit <= 0 {
		req.Limit = 10
	}

	pipes, filt, errMsg := packs.ResolveTypeAndTask(req.Type, req.Task)
	if errMsg != "" {
		return nil, searchMeta{}, fmt.Errorf("%s", errMsg)
	}
	packID := packs.CanonicalTypeID(req.Type)
	if filt != "" {
		if req.Filter == "" {
			req.Filter = filt
		} else if !strings.EqualFold(req.Filter, filt) {
			// keep both via tag filter already set; Hub filter is single — prefer type filter
			req.Filter = filt
		}
	}

	var models []hf.Model
	var meta searchMeta
	if req.Online {
		models, meta, err = searchHubLive(ctx, req, sortKey, pipes)
	} else {
		path, source := resolveSearchIndex()
		if path == "" {
			return nil, searchMeta{}, errNoSearchIndex()
		}
		models, meta, err = searchIndexAtPath(ctx, req, sortKey, path, source, pipes, packID)
	}
	if err != nil {
		return nil, meta, err
	}
	meta.PackID = packID
	meta.Pool = append([]hf.Model{}, models...)
	if len(models) > req.Limit {
		models = models[:req.Limit]
	}
	return models, meta, nil
}

func searchHubLive(ctx context.Context, req searchRequest, sortKey string, pipes []string) ([]hf.Model, searchMeta, error) {
	client := hf.New(config.Load().HFToken)
	meta := searchMeta{
		RankSource: "hub",
		Queries:    hf.HubSearchQueries(req.Query),
	}

	base := hf.SearchOpts{
		Query:   req.Query,
		Author:  req.Author,
		Library: req.Library,
		Filter:  req.Filter,
		License: req.License,
		Engine:  req.Engine,
	}

	var models []hf.Model
	var note string
	var err error
	if len(pipes) <= 1 {
		task := "any"
		if len(pipes) == 1 {
			task = pipes[0]
		}
		base.Task = task
		models, note, err = liveHubSearchFn(ctx, client, base, sortKey, searchPoolCap, !req.DisableSemantic)
	} else {
		models, note, err = searchHubMultiPipeline(ctx, client, base, pipes, sortKey, !req.DisableSemantic)
	}
	if err != nil {
		return nil, meta, err
	}
	if note != "" {
		meta.Notes = note
		if runtime.Verbose() {
			fmt.Fprintln(os.Stderr, dim(note))
		}
	}
	if sortKey == "likes" || sortKey == "downloads" {
		meta.RankSource = sortKey + " (from expanded pool)"
	} else if strings.HasPrefix(note, "semantic rank") {
		meta.RankSource = "semantic + hub"
	} else {
		meta.RankSource = "hub + descriptions"
	}
	return models, meta, nil
}

func searchHubMultiPipeline(ctx context.Context, client *hf.Client, base hf.SearchOpts, pipes []string, sortKey string, wantSemantic bool) ([]hf.Model, string, error) {
	seen := map[string]hf.Model{}
	var note string
	per := searchPoolCap / len(pipes)
	if per < 20 {
		per = 20
	}
	for _, p := range pipes {
		opts := base
		opts.Task = p
		batch, n, err := liveHubSearchFn(ctx, client, opts, sortKey, per, false)
		if err != nil {
			continue
		}
		if n != "" {
			note = n
		}
		for _, m := range batch {
			id := m.RepoID()
			if id == "" {
				continue
			}
			if _, ok := seen[id]; !ok {
				seen[id] = m
			}
		}
	}
	out := make([]hf.Model, 0, len(seen))
	for _, m := range seen {
		out = append(out, m)
	}
	if sortKey == "likes" || sortKey == "downloads" {
		hf.SortModels(out, sortKey)
	} else {
		hf.ScoreRelevance(out, base.Query)
		out, note = rankSemantic(ctx, base.Query, sortKey, wantSemantic, out, func() semantic.Embedder {
			return semantic.Discover(ctx, client.Token)
		})
	}
	if len(out) > searchPoolCap {
		out = out[:searchPoolCap]
	}
	return out, note, nil
}

func searchIndexAtPath(ctx context.Context, req searchRequest, sortKey string, path string, source string, pipes []string, packID string) ([]hf.Model, searchMeta, error) {
	idx, err := index.Open(path)
	if err != nil {
		return nil, searchMeta{}, fmt.Errorf("open %s: %w", source, err)
	}
	defer idx.Close()

	meta := searchMeta{RankSource: source, Queries: hf.HubSearchQueries(req.Query)}

	q := req.Query
	if extra := hf.AliasTerms(req.Query); len(extra) > 0 {
		q = strings.TrimSpace(req.Query + " " + strings.Join(extra, " "))
	}

	filters := index.SearchFilters{
		Author:   req.Author,
		Library:  req.Library,
		License:  req.License,
		Engine:   req.Engine,
		Filter:   req.Filter,
		Sort:     sortKey,
		Limit:    100,
		PackType: packID,
	}
	switch {
	case len(pipes) == 1:
		filters.PipelineTag = pipes[0]
	case len(pipes) > 1:
		filters.PipelineTags = pipes
	}

	models, err := idx.Search(ctx, q, filters)
	if err != nil {
		return nil, searchMeta{}, fmt.Errorf("search local index: %w", err)
	}

	if sortKey == "relevance" {
		hf.ScoreRelevance(models, req.Query)
		meta.RankSource = source + " + descriptions"
		var note string
		models, note = rankSemantic(ctx, req.Query, sortKey, !req.DisableSemantic, models, func() semantic.Embedder {
			return semantic.Discover(ctx, config.Load().HFToken)
		})
		if note != "" {
			meta.Notes = note
			if runtime.Verbose() {
				fmt.Fprintln(os.Stderr, dim(note))
			}
		}
		if strings.HasPrefix(note, "semantic rank") {
			meta.RankSource = source + " + semantic"
		}
	}
	if sortKey == "likes" || sortKey == "downloads" {
		hf.SortModels(models, sortKey)
		meta.RankSource = source + " + " + sortKey
	}

	return models, meta, nil
}
