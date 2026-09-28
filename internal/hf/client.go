package hf

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adamsiwiec1/runhug/internal/version"
)

const BaseURL = "https://huggingface.co"

type Client struct {
	HTTP    *http.Client
	Token   string
	BaseURL string
}

func New(token string) *Client {
	return &Client{
		HTTP:    &http.Client{Timeout: 30 * time.Second},
		Token:   token,
		BaseURL: BaseURL,
	}
}

type SearchOpts struct {
	Query       string
	Author      string
	Task        string
	Library     string
	Filter      string
	License     string // apache-2.0, mit, gemma, other, …
	Engine      string // vllm, gguf, or any DetectFormat engine string
	Sort        string // relevance (default), likes, downloads
	Limit       int
	Offset      int // Hub has no official offset; ignored by Search
	Full        bool
	Expand      bool   // extra Hub search= aliases / tokens, then local score
	ExtraFilter string // additional Hub filter= tag (tag-probe recall)
}

type Model struct {
	ID           string         `json:"id"`
	ModelID      string         `json:"modelId"`
	Author       string         `json:"author"`
	PipelineTag  string         `json:"pipeline_tag"`
	LibraryName  string         `json:"library_name"`
	Tags         []string       `json:"tags"`
	Downloads    int64          `json:"downloads"`
	Likes        int            `json:"likes"`
	Private      bool           `json:"private"`
	Gated        any            `json:"gated"`
	LastModified string         `json:"lastModified"`
	SHA          string         `json:"sha"`
	UsedStorage  int64          `json:"usedStorage"`
	Siblings     []Sibling      `json:"siblings"`
	Safetensors  *Safetensors   `json:"safetensors"`
	CardData     map[string]any `json:"cardData"`
	Config       map[string]any `json:"config"`
	Description  string         `json:"description"`
}

type Sibling struct {
	RFilename string `json:"rfilename"`
}

type Safetensors struct {
	Total      int64            `json:"total"`
	Parameters map[string]int64 `json:"parameters"`
}

func (m Model) RepoID() string {
	if m.ID != "" {
		return m.ID
	}
	return m.ModelID
}

func (m Model) IsGated() bool {
	switch v := m.Gated.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		s := strings.TrimSpace(strings.ToLower(v))
		return s != "" && s != "false"
	default:
		return true
	}
}

func (m Model) License() string {
	for _, tag := range m.Tags {
		if strings.HasPrefix(tag, "license:") {
			return strings.TrimPrefix(tag, "license:")
		}
	}
	if m.CardData != nil {
		if lic, ok := m.CardData["license"].(string); ok && lic != "" {
			return lic
		}
	}
	return ""
}

// CardDescription is the model card / Hub description, if the list or Get
// payload included one. Does not download weights or README files.
func (m Model) CardDescription() string {
	if s := strings.TrimSpace(m.Description); s != "" {
		return clipDesc(s)
	}
	if m.CardData == nil {
		return ""
	}
	for _, key := range []string{"description", "summary", "text"} {
		if s, ok := m.CardData[key].(string); ok {
			if s = strings.TrimSpace(s); s != "" {
				return clipDesc(s)
			}
		}
	}
	return ""
}

// FamilyHints collects Hub signals used to pick worker-vllm family env
// (ENABLE_AUTO_TOOL_CHOICE, TOOL_CALL_PARSER, …) when the repo id alone is
// insufficient — e.g. fine-tunes whose names omit "qwen"/"mistral"/"llama".
func (m Model) FamilyHints() []string {
	var out []string
	out = append(out, m.Tags...)
	if m.CardData != nil {
		switch bm := m.CardData["base_model"].(type) {
		case string:
			if s := strings.TrimSpace(bm); s != "" {
				out = append(out, s)
			}
		case []any:
			for _, x := range bm {
				if s, ok := x.(string); ok {
					if s = strings.TrimSpace(s); s != "" {
						out = append(out, s)
					}
				}
			}
		}
	}
	if m.Config != nil {
		if mt, ok := m.Config["model_type"].(string); ok {
			if mt = strings.TrimSpace(mt); mt != "" {
				out = append(out, mt)
			}
		}
		switch archs := m.Config["architectures"].(type) {
		case []any:
			for _, a := range archs {
				if s, ok := a.(string); ok {
					if s = strings.TrimSpace(s); s != "" {
						out = append(out, s)
					}
				}
			}
		case []string:
			for _, s := range archs {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

func clipDesc(s string) string {
	if len(s) > 2000 {
		return s[:2000]
	}
	return s
}

func (c *Client) Search(ctx context.Context, opts SearchOpts) ([]Model, error) {
	if err := normalizeSearchOpts(&opts); err != nil {
		return nil, err
	}
	if opts.Expand && strings.TrimSpace(opts.Query) != "" {
		return c.searchExpanded(ctx, opts)
	}
	return c.searchOnce(ctx, opts)
}

func normalizeSearchOpts(opts *SearchOpts) error {
	if opts.Limit <= 0 {
		opts.Limit = 15
	}
	if opts.Limit > 100 {
		opts.Limit = 100
	}
	if opts.Task == "" {
		opts.Task = "any"
	}
	sortKey, err := NormalizeSort(opts.Sort)
	if err != nil {
		return err
	}
	opts.Sort = sortKey
	return nil
}

func (c *Client) searchOnce(ctx context.Context, opts SearchOpts) ([]Model, error) {
	// likes/downloads: fetch a relevance pool, then re-rank locally.
	// GET /api/models text relevance is the unsorted default (omit sort).
	fetchLimit := opts.Limit
	apiSort := opts.Sort
	if opts.Sort == "likes" || opts.Sort == "downloads" {
		fetchLimit = 100
		if opts.Limit > fetchLimit {
			fetchLimit = opts.Limit
		}
		apiSort = "relevance"
	}

	q := url.Values{}
	if opts.Query != "" {
		q.Set("search", opts.Query)
	}
	if opts.Author != "" {
		q.Set("author", opts.Author)
	}
	if opts.Task != "" && opts.Task != "any" {
		q.Set("pipeline_tag", opts.Task)
	}
	if opts.Library != "" {
		q.Set("library", opts.Library)
	}
	for _, f := range searchFilters(opts) {
		q.Add("filter", f)
	}
	if apiSort != "relevance" {
		q.Set("sort", apiSort)
		q.Set("direction", "-1")
	}
	q.Set("limit", strconv.Itoa(fetchLimit))
	if opts.Full {
		q.Set("full", "true")
		// expand replaces the default field set — include stats, not only cardData.
		for _, field := range []string{
			"cardData", "likes", "downloads", "tags", "pipeline_tag",
			"library_name", "siblings", "gated", "safetensors", "author",
		} {
			q.Add("expand", field)
		}
	}

	var models []Model
	if err := c.get(ctx, "/api/models?"+q.Encode(), &models); err != nil {
		return nil, err
	}
	models = filterByEngine(models, opts.Engine)
	models = filterByLicense(models, opts.License)
	if opts.Sort == "likes" || opts.Sort == "downloads" {
		SortModels(models, opts.Sort)
	}
	if len(models) > opts.Limit {
		models = models[:opts.Limit]
	}
	return models, nil
}

func (c *Client) searchExpanded(ctx context.Context, opts SearchOpts) ([]Model, error) {
	sortKey := opts.Sort
	plan := HubQueryPlan(opts.Query, opts.Task)
	seen := map[string]Model{}
	add := func(batch []Model) {
		for _, m := range batch {
			id := m.RepoID()
			if id == "" {
				continue
			}
			if existing, ok := seen[id]; ok {
				if existing.CardDescription() == "" && m.CardDescription() != "" {
					seen[id] = mergeModelMeta(existing, m)
				}
				continue
			}
			seen[id] = m
		}
	}

	for i, call := range plan {
		one := opts
		one.Expand = false
		one.Full = true
		one.Query = call.Search
		one.Task = call.Task
		one.Sort = "relevance"
		one.Limit = 100
		one.ExtraFilter = call.Filter
		batch, err := c.searchOnce(ctx, one)
		if err != nil {
			if i == 0 {
				return nil, err
			}
			continue
		}
		add(batch)
	}

	pool := make([]Model, 0, len(seen))
	for _, m := range seen {
		pool = append(pool, m)
	}
	c.hydrateDescriptions(ctx, pool, RelevanceTerms(opts.Query), maxCardGets)
	ScoreRelevance(pool, opts.Query)
	if len(pool) > 100 {
		pool = pool[:100]
	}
	if sortKey == "likes" || sortKey == "downloads" {
		SortModels(pool, sortKey)
	}
	pool = filterByEngine(pool, opts.Engine)
	pool = filterByLicense(pool, opts.License)
	if len(pool) > opts.Limit {
		pool = pool[:opts.Limit]
	}
	return pool, nil
}

func mergeModelMeta(dst, src Model) Model {
	if dst.Description == "" {
		dst.Description = strings.TrimSpace(src.Description)
	}
	if dst.CardData == nil && src.CardData != nil {
		dst.CardData = src.CardData
	}
	if dst.Description == "" {
		dst.Description = src.CardDescription()
	}
	if dst.PipelineTag == "" {
		dst.PipelineTag = src.PipelineTag
	}
	dst.Tags = unionTags(dst.Tags, src.Tags)
	return dst
}

func unionTags(a, b []string) []string {
	if len(b) == 0 {
		return a
	}
	seen := map[string]bool{}
	var out []string
	for _, t := range a {
		k := strings.ToLower(t)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, t)
	}
	for _, t := range b {
		k := strings.ToLower(t)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, t)
	}
	return out
}

func (c *Client) hydrateDescriptions(ctx context.Context, models []Model, terms []string, maxGets int) {
	if maxGets <= 0 {
		maxGets = maxCardGets
	}
	type cand struct {
		i       int
		likes   int
		noMatch bool
	}
	var need []cand
	for i, m := range models {
		if m.RepoID() == "" || strings.TrimSpace(m.CardDescription()) != "" {
			continue
		}
		need = append(need, cand{
			i:       i,
			likes:   m.Likes,
			noMatch: !matchesTerms(SearchableText(m), terms),
		})
	}
	sort.SliceStable(need, func(i, j int) bool {
		if need[i].noMatch != need[j].noMatch {
			return need[i].noMatch
		}
		return need[i].likes > need[j].likes
	})
	if len(need) > maxGets {
		need = need[:maxGets]
	}
	if len(need) == 0 {
		return
	}

	sem := make(chan struct{}, cardGetConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, n := range need {
		if err := ctx.Err(); err != nil {
			break
		}
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			full, err := c.Get(ctx, id)
			if err != nil || full == nil {
				return
			}
			mu.Lock()
			models[i] = mergeModelMeta(models[i], *full)
			mu.Unlock()
		}(n.i, models[n.i].RepoID())
	}
	wg.Wait()
}

func (c *Client) Get(ctx context.Context, repoID string) (*Model, error) {
	repoID = strings.TrimSpace(strings.TrimPrefix(repoID, "https://huggingface.co/"))
	repoID = strings.Trim(repoID, "/")
	if repoID == "" {
		return nil, fmt.Errorf("model id is required (org/name)")
	}
	var m Model
	if err := c.get(ctx, "/api/models/"+repoID, &m); err != nil {
		return nil, err
	}
	if m.ID == "" {
		m.ID = repoID
	}
	return &m, nil
}

// GenerationConfig fetches resolve/main/generation_config.json. A missing file
// (404) returns an empty map, not an error.
func (c *Client) GenerationConfig(ctx context.Context, repoID string) (map[string]any, error) {
	repoID = strings.TrimSpace(strings.TrimPrefix(repoID, "https://huggingface.co/"))
	repoID = strings.Trim(repoID, "/")
	if repoID == "" {
		return nil, fmt.Errorf("model id is required (org/name)")
	}
	base := c.BaseURL
	if base == "" {
		base = BaseURL
	}
	u := strings.TrimRight(base, "/") + "/" + repoID + "/resolve/main/generation_config.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", version.Name+"/"+version.Version)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return map[string]any{}, nil
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("generation_config.json: HTTP %d: %s", res.StatusCode, trimBody(body))
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("generation_config.json: %w", err)
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

// Whoami verifies a token via GET /api/whoami-v2 and returns the username (never the token).
func (c *Client) Whoami(ctx context.Context) (string, error) {
	var info struct {
		Name     string `json:"name"`
		FullName string `json:"fullname"`
		Type     string `json:"type"`
	}
	if err := c.get(ctx, "/api/whoami-v2", &info); err != nil {
		return "", err
	}
	if info.Name != "" {
		return info.Name, nil
	}
	if info.FullName != "" {
		return info.FullName, nil
	}
	return "", nil
}

func (c *Client) get(ctx context.Context, path string, dest any) error {
	base := c.BaseURL
	if base == "" {
		base = BaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", version.Name+"/"+version.Version)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return err
	}
	if res.StatusCode == http.StatusNotFound {
		return fmt.Errorf("huggingface: model not found")
	}
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return fmt.Errorf("huggingface: HTTP %d (set HF_TOKEN for gated or private models)", res.StatusCode)
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("huggingface: HTTP %d: %s", res.StatusCode, trimBody(body))
	}
	if dest == nil {
		return nil
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("huggingface: decode: %w", err)
	}
	return nil
}

func trimBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}
