package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/hf"
	"github.com/adamsiwiec1/runhug/internal/index"
	"github.com/adamsiwiec1/runhug/internal/runtime"
	"github.com/adamsiwiec1/runhug/internal/semantic"
)

type searchRequest struct {
	Query           string
	Author          string
	Task            string
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
}

type hubSearchFn func(ctx context.Context, client *hf.Client, opts hf.SearchOpts, sortKey string, displayLimit int, wantSemantic bool) ([]hf.Model, string, error)

// Test hooks. Production uses the real path helpers and Hub search.
var (
	userIndexPathFn                = indexFilePath
	bundledIndexPathFn             = bundledIndexPath
	liveHubSearchFn    hubSearchFn = searchRanked
)

func errNoSearchIndex() error {
	return fmt.Errorf("no local search index found.\nRun `runhug update` or `runhug init` to build one from Hugging Face.\nOr pass --online / --hub for a live Hub search (rate-limited; set HF_TOKEN).")
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

	if req.Online {
		return searchHubLive(ctx, req, sortKey)
	}

	path, source := resolveSearchIndex()
	if path == "" {
		return nil, searchMeta{}, errNoSearchIndex()
	}
	return searchIndexAtPath(ctx, req, sortKey, path, source)
}

func searchHubLive(ctx context.Context, req searchRequest, sortKey string) ([]hf.Model, searchMeta, error) {
	client := hf.New(config.Load().HFToken)
	meta := searchMeta{
		RankSource: "hub",
		Queries:    hf.HubSearchQueries(req.Query),
	}
	task := hf.ResolveTask(req.Task, req.Query)
	models, note, err := liveHubSearchFn(ctx, client, hf.SearchOpts{
		Query:   req.Query,
		Author:  req.Author,
		Task:    task,
		Library: req.Library,
		Filter:  req.Filter,
		License: req.License,
		Engine:  req.Engine,
	}, sortKey, req.Limit, !req.DisableSemantic)
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

func searchIndexAtPath(ctx context.Context, req searchRequest, sortKey string, path string, source string) ([]hf.Model, searchMeta, error) {
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

	pipeline := req.Task
	if pipeline == "any" || pipeline == "auto" || pipeline == "" {
		pipeline = ""
	}
	filters := index.SearchFilters{
		Author:      req.Author,
		Library:     req.Library,
		License:     req.License,
		PipelineTag: pipeline,
		Engine:      req.Engine,
		Filter:      req.Filter,
		Sort:        sortKey,
		Limit:       100,
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

	if len(models) > req.Limit {
		models = models[:req.Limit]
	}

	return models, meta, nil
}
