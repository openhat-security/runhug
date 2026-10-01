package gpudb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/packs"
	"github.com/adamsiwiec1/runhug/internal/version"
)

const (
	releaseAssetNVIDIA = "gpudb-nvidia.json"
	releaseAssetAMD    = "gpudb-amd.json"
	releaseAssetGCP    = "gpudb-gcp.json"
)

// UpdateOptions controls gpu update.
type UpdateOptions struct {
	NVIDIA bool
	AMD    bool
	GCP    bool
	// PreferRelease tries GitHub Release assets first (like model packs).
	PreferRelease bool
}

// UpdateResult summarizes what was written.
type UpdateResult struct {
	NVIDIACount int
	AMDCount    int
	GCPCount    int
	NVIDIAFrom  string
	AMDFrom     string
	GCPFrom     string
	CacheDir    string
	NVIDIAAdded []string
	AMDAdded    []string
	GCPAdded    []string
}

// Update refreshes cached catalogs under CacheDir.
func Update(ctx context.Context, opts UpdateOptions) (UpdateResult, error) {
	if !opts.NVIDIA && !opts.AMD && !opts.GCP {
		opts.NVIDIA, opts.AMD, opts.GCP = true, true, true
	}
	dir, err := CacheDir()
	if err != nil {
		return UpdateResult{}, err
	}
	res := UpdateResult{CacheDir: dir}

	if opts.NVIDIA {
		n, src, added, err := updateVendor(ctx, updateVendorOpts{
			PreferRelease: opts.PreferRelease,
			ReleaseAsset:  releaseAssetNVIDIA,
			FetchURL:      NVIDIAFetchURL,
			CacheName:     nvidiaCache,
			Embed:         nvidiaJSON,
			Vendor:        "nvidia",
			Trim:          TrimNVIDIA,
			Label:         "huggingface:Jr23xd23/gpu-database/nvidia",
		})
		if err != nil {
			return res, err
		}
		res.NVIDIACount, res.NVIDIAFrom, res.NVIDIAAdded = n, src, added
	}
	if opts.AMD {
		n, src, added, err := updateVendor(ctx, updateVendorOpts{
			PreferRelease: opts.PreferRelease,
			ReleaseAsset:  releaseAssetAMD,
			FetchURL:      AMDFetchURL,
			CacheName:     amdCache,
			Embed:         amdJSON,
			Vendor:        "amd",
			Trim:          TrimAMD,
			Label:         "huggingface:Jr23xd23/gpu-database/amd",
		})
		if err != nil {
			return res, err
		}
		res.AMDCount, res.AMDFrom, res.AMDAdded = n, src, added
	}
	if opts.GCP {
		n, src, added, err := updateGCP(ctx, opts.PreferRelease)
		if err != nil {
			return res, err
		}
		res.GCPCount, res.GCPFrom, res.GCPAdded = n, src, added
	}
	return res, nil
}

type updateVendorOpts struct {
	PreferRelease bool
	ReleaseAsset  string
	FetchURL      string
	CacheName     string
	Embed         []byte
	Vendor        string
	Trim          func([]Spec) []Spec
	Label         string
}

func updateVendor(ctx context.Context, o updateVendorOpts) (int, string, []string, error) {
	prev := loadSpecNames(o.CacheName, o.Embed)
	var raw []byte
	var src string
	if o.PreferRelease {
		if b, err := fetchReleaseAsset(ctx, o.ReleaseAsset); err == nil && len(b) > 0 {
			raw, src = b, "github-release:"+o.ReleaseAsset
		}
	}
	if raw == nil {
		b, err := httpGet(ctx, o.FetchURL)
		if err != nil {
			return 0, "", nil, fmt.Errorf("fetch %s index: %w", o.Vendor, err)
		}
		raw, src = b, o.Label
	}
	var full []Spec
	if err := json.Unmarshal(raw, &full); err != nil {
		return 0, "", nil, fmt.Errorf("decode %s JSON: %w", o.Vendor, err)
	}
	trimmed := o.Trim(full)
	if len(trimmed) == 0 {
		trimmed = full
		for i := range trimmed {
			if trimmed[i].Vendor == "" {
				trimmed[i].Vendor = o.Vendor
			}
		}
	}
	sort.Slice(trimmed, func(i, j int) bool {
		if trimmed[i].FP16 != trimmed[j].FP16 {
			return trimmed[i].FP16 > trimmed[j].FP16
		}
		return trimmed[i].Name < trimmed[j].Name
	})
	out, err := json.Marshal(trimmed)
	if err != nil {
		return 0, "", nil, err
	}
	out = append(out, '\n')
	if err := WriteCache(o.CacheName, out); err != nil {
		return 0, "", nil, err
	}
	return len(trimmed), src, diffNewNames(prev, specNames(trimmed)), nil
}

func updateGCP(ctx context.Context, preferRelease bool) (int, string, []string, error) {
	prev := loadGCPNames()
	var raw []byte
	var src string

	// 1) Live gcloud list merged onto curated metadata (best when authenticated).
	if live, err := fetchGCloudAccelerators(ctx); err == nil && len(live) > 0 {
		merged := mergeGCPLive(live)
		out, err := json.Marshal(merged)
		if err != nil {
			return 0, "", nil, err
		}
		out = append(out, '\n')
		if err := WriteCache(gcpCache, out); err != nil {
			return 0, "", nil, err
		}
		return len(merged), "gcloud+bundled", diffNewNames(prev, gcpNames(merged)), nil
	}

	if preferRelease {
		if b, err := fetchReleaseAsset(ctx, releaseAssetGCP); err == nil && len(b) > 0 {
			raw, src = b, "github-release:"+releaseAssetGCP
		}
	}
	if raw == nil {
		b, err := httpGet(ctx, GCPCatalogURL)
		if err != nil {
			// Fall back to re-writing the embed so cache exists for inspection.
			raw = append([]byte(nil), gcpJSON...)
			src = "bundled"
		} else {
			raw, src = b, "github:openhat-security/runhug/gcp.json"
		}
	}
	var list []GCPAccelerator
	if err := json.Unmarshal(raw, &list); err != nil {
		return 0, "", nil, fmt.Errorf("decode GCP JSON: %w", err)
	}
	out, err := json.Marshal(list)
	if err != nil {
		return 0, "", nil, err
	}
	out = append(out, '\n')
	if err := WriteCache(gcpCache, out); err != nil {
		return 0, "", nil, err
	}
	return len(list), src, diffNewNames(prev, gcpNames(list)), nil
}

func loadSpecNames(cacheName string, embed []byte) map[string]struct{} {
	var specs []Spec
	if raw, err := readCache(cacheName); err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &specs)
	}
	if len(specs) == 0 && len(embed) > 0 {
		_ = json.Unmarshal(embed, &specs)
	}
	return nameSet(specNames(specs))
}

func loadGCPNames() map[string]struct{} {
	list, _ := LoadGCP()
	return nameSet(gcpNames(list))
}

func specNames(specs []Spec) []string {
	out := make([]string, 0, len(specs))
	for _, s := range specs {
		if n := strings.TrimSpace(s.Name); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func gcpNames(list []GCPAccelerator) []string {
	out := make([]string, 0, len(list))
	for _, g := range list {
		n := strings.TrimSpace(g.Name)
		if n == "" {
			n = strings.TrimSpace(g.ID)
		}
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

func nameSet(names []string) map[string]struct{} {
	out := make(map[string]struct{}, len(names))
	for _, n := range names {
		out[n] = struct{}{}
	}
	return out
}

func diffNewNames(prev map[string]struct{}, next []string) []string {
	var added []string
	seen := map[string]struct{}{}
	for _, n := range next {
		if _, ok := prev[n]; ok {
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		added = append(added, n)
	}
	sort.Strings(added)
	return added
}

func fetchReleaseAsset(ctx context.Context, name string) ([]byte, error) {
	rc := packs.NewReleaseClient(packs.ReleaseRepo())
	if tok := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); tok != "" {
		rc.Token = tok
	}
	data, _, err := rc.FetchAssetBytes(ctx, name)
	return data, err
}

func httpGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", version.Name+"/"+version.Version)
	client := &http.Client{Timeout: 2 * time.Minute}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d from %s", res.StatusCode, url)
	}
	return body, nil
}

func fetchGCloudAccelerators(ctx context.Context) ([]string, error) {
	path, err := exec.LookPath("gcloud")
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, "compute", "accelerator-types", "list",
		"--format=value(name)")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var ids []string
	for _, line := range strings.Split(string(out), "\n") {
		id := strings.TrimSpace(line)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func mergeGCPLive(ids []string) []GCPAccelerator {
	curated, _ := LoadGCP()
	byID := map[string]GCPAccelerator{}
	for _, c := range curated {
		byID[c.ID] = c
	}
	// Always include curated machine-series entries even if not in accelerator-types.
	out := make([]GCPAccelerator, 0, len(ids)+len(curated))
	seen := map[string]bool{}
	for _, id := range ids {
		if !strings.HasPrefix(id, "nvidia-") {
			continue // skip TPUs etc.
		}
		if c, ok := byID[id]; ok {
			out = append(out, c)
		} else {
			out = append(out, GCPAccelerator{
				ID:     id,
				Name:   prettyAccelName(id),
				HFName: prettyAccelName(id),
				Series: "N1",
				Attach: "accelerator",
			})
		}
		seen[id] = true
	}
	for _, c := range curated {
		if !seen[c.ID] {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].MemoryGB != out[j].MemoryGB {
			return out[i].MemoryGB > out[j].MemoryGB
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func prettyAccelName(id string) string {
	s := strings.TrimPrefix(id, "nvidia-")
	s = strings.TrimPrefix(s, "tesla-")
	s = strings.ReplaceAll(s, "-", " ")
	if s == "" {
		return id
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
