package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/gpudb"
	"github.com/adamsiwiec1/runhug/internal/hostgpu"
	"github.com/adamsiwiec1/runhug/internal/runpod"
)

// gpuRow is one display/selection row for `runhug gpu list|set`.
type gpuRow struct {
	Name        string  `json:"name"`
	Provider    string  `json:"provider,omitempty"` // local | runpod | gcp | "" (index-only)
	Key         string  `json:"key,omitempty"`
	MemoryGB    float64 `json:"memory_gb"`
	FP16        float64 `json:"fp16,omitempty"`
	TensorCores int     `json:"tensor_cores,omitempty"`
	CUDA        string  `json:"cuda,omitempty"`
	Arch        string  `json:"architecture,omitempty"`
	PricePerHr  float64 `json:"price_per_hour,omitempty"`
	Stock       string  `json:"stock,omitempty"`
	InStock     bool    `json:"in_stock,omitempty"`
	Yours       bool    `json:"yours,omitempty"` // matches a GPU on this machine
}

func buildGPURows(filter, query string) ([]gpuRow, error) {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		filter = "all"
	}
	specs, err := gpudb.Load()
	if err != nil {
		return nil, err
	}
	if q := strings.TrimSpace(query); q != "" {
		specs = gpudb.Search(specs, q)
	}

	switch filter {
	case "all":
		return markYoursInRows(enrichIndexRows(specs)), nil
	case "local":
		return markYoursInRows(localCatalogRows(specs)), nil
	case "amd", "nvidia":
		specs = filterSpecsByVendor(specs, filter)
		return enrichIndexRows(specs), nil
	case "runpod":
		return runpodGPURows(specs, query)
	case "gcp":
		return gcpGPURows(specs, query)
	default:
		return nil, fmt.Errorf("unknown --filter %q (want all|local|amd|nvidia|runpod|gcp)", filter)
	}
}

func filterSpecsByVendor(specs []gpudb.Spec, vendor string) []gpudb.Spec {
	vendor = strings.ToLower(vendor)
	out := make([]gpudb.Spec, 0, len(specs))
	for _, s := range specs {
		v := strings.ToLower(s.Vendor)
		if v == "" {
			v = "nvidia" // legacy rows
		}
		if v == vendor {
			out = append(out, s)
		}
	}
	return out
}

func enrichIndexRows(specs []gpudb.Spec) []gpuRow {
	rpByName, gcpByName := cloudJoinMaps()
	out := make([]gpuRow, 0, len(specs))
	for _, s := range specs {
		r := rowFromSpec(s)
		key := normalizeJoinKey(s.Name)
		if p, ok := rpByName[key]; ok {
			r.Provider = "runpod"
			r.Key = p.ID
			r.PricePerHr = p.PricePerHour
			r.Stock = p.Availability
			r.InStock = p.InStock
			if p.MemoryGB > 0 {
				r.MemoryGB = p.MemoryGB
			}
		} else if g, ok := gcpByName[key]; ok {
			r.Provider = "gcp"
			r.Key = g.Name
			r.PricePerHr = g.SpotHourlyApprox
			r.Stock = g.Series
			r.InStock = true
			if g.MemoryGB > 0 {
				r.MemoryGB = g.MemoryGB
			}
		}
		out = append(out, r)
	}
	return out
}

func rowFromSpec(s gpudb.Spec) gpuRow {
	return gpuRow{
		Name:        s.Name,
		MemoryGB:    s.MemorySize,
		FP16:        s.FP16,
		TensorCores: s.TensorCores,
		CUDA:        s.CUDA,
		Arch:        s.Architecture,
	}
}

// localCatalogRows is the full vendored index tagged as local-runnable SKUs
// (not "what's plugged into this machine"). Detected hardware is separate.
func localCatalogRows(specs []gpudb.Spec) []gpuRow {
	devs, _ := hostgpu.Detect()
	out := make([]gpuRow, 0, len(specs))
	for _, s := range specs {
		r := rowFromSpec(s)
		r.Provider = "local"
		r.Key = s.Name
		for _, d := range devs {
			if d.Vendor == "apple" {
				continue
			}
			if ms, ok := gpudb.Match(specs, d.Name); ok && normalizeJoinKey(ms.Name) == normalizeJoinKey(s.Name) {
				r.Yours = true
				r.Stock = "yours"
				break
			}
		}
		out = append(out, r)
	}
	return out
}

// detectedLocalRows returns GPUs actually present on this host (nvidia-smi / Apple Silicon).
func detectedLocalRows(specs []gpudb.Spec) []gpuRow {
	devs, err := hostgpu.Detect()
	if err != nil || len(devs) == 0 {
		return nil
	}
	out := make([]gpuRow, 0, len(devs))
	for _, d := range devs {
		r := gpuRow{
			Name:     d.Name,
			Provider: "local",
			Key:      d.Name,
			MemoryGB: d.MemoryGB,
			FP16:     d.FP16,
			Yours:    true,
			Stock:    "yours",
		}
		if d.Vendor == "apple" {
			r.Arch = "Apple Silicon"
			if d.Note != "" {
				r.Arch = d.Name // keep chip name; arch column already Apple-ish via name
			}
			out = append(out, r)
			continue
		}
		if s, ok := gpudb.Match(specs, d.Name); ok {
			r.Name = s.Name
			r.Key = s.Name
			if s.FP16 > 0 {
				r.FP16 = s.FP16
			}
			r.TensorCores = s.TensorCores
			r.CUDA = s.CUDA
			r.Arch = s.Architecture
			if s.MemorySize > 0 && d.MemoryGB <= 0 {
				r.MemoryGB = s.MemorySize
			}
		}
		out = append(out, r)
	}
	return out
}

// markYoursInRows flags index/catalog rows that match host GPUs and injects
// Apple Silicon (or unmatched NVIDIA) so --sort best can rank them in the full list.
func markYoursInRows(rows []gpuRow) []gpuRow {
	devs, err := hostgpu.Detect()
	if err != nil || len(devs) == 0 {
		return rows
	}
	specs, _ := gpudb.Load()
	for _, d := range devs {
		matched := false
		if d.Vendor != "apple" {
			for i := range rows {
				if rows[i].Yours {
					continue
				}
				nameMatch := normalizeJoinKey(rows[i].Name) == normalizeJoinKey(d.Name)
				if !nameMatch && len(specs) > 0 {
					if ms, ok := gpudb.Match(specs, d.Name); ok {
						nameMatch = normalizeJoinKey(rows[i].Name) == normalizeJoinKey(ms.Name)
					}
				}
				if nameMatch {
					rows[i].Yours = true
					rows[i].Stock = "yours"
					matched = true
				}
			}
		}
		if matched {
			continue
		}
		// Inject synthetic / unmatched host GPU into the ranking list.
		inj := gpuRow{
			Name:     d.Name,
			Provider: "local",
			Key:      d.Name,
			MemoryGB: d.MemoryGB,
			FP16:     d.FP16,
			Yours:    true,
			Stock:    "yours",
		}
		if d.Vendor == "apple" {
			inj.Arch = "Apple Silicon"
		} else if len(specs) > 0 {
			if s, ok := gpudb.Match(specs, d.Name); ok {
				inj.Name = s.Name
				inj.Key = s.Name
				inj.FP16 = s.FP16
				inj.TensorCores = s.TensorCores
				inj.CUDA = s.CUDA
				inj.Arch = s.Architecture
			}
		}
		// Avoid duplicate inject if already present by key.
		dup := false
		for _, r := range rows {
			if r.Yours && normalizeJoinKey(r.Name) == normalizeJoinKey(inj.Name) {
				dup = true
				break
			}
		}
		if !dup {
			rows = append(rows, inj)
		}
	}
	return rows
}

// yoursRank returns 1-based rank among rows already sorted, or 0 if none marked yours.
func yoursRank(rows []gpuRow) (rank int, total int, row gpuRow) {
	total = len(rows)
	for i, r := range rows {
		if r.Yours {
			return i + 1, total, r
		}
	}
	return 0, total, gpuRow{}
}

type localListPayload struct {
	OnThisMachine []gpuRow `json:"on_this_machine"`
	Catalog       []gpuRow `json:"catalog"`
	YoursRank     int      `json:"yours_rank,omitempty"`
	YoursTotal    int      `json:"yours_total,omitempty"`
}

func runpodGPURows(specs []gpudb.Spec, query string) ([]gpuRow, error) {
	env := config.Load()
	var gpus []runpod.GPU
	if env.RunpodAPIKey != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		list, err := runpod.New(env.RunpodAPIKey).ListGPUs(ctx)
		if err != nil {
			return nil, err
		}
		gpus = list
	} else {
		gpus = runpod.OfflineCatalog()
	}
	pools := runpod.SummarizePools(gpus)
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]gpuRow, 0, len(pools))
	for _, p := range pools {
		if q != "" {
			hay := strings.ToLower(p.ID + " " + p.ExampleGPU)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		r := gpuRow{
			Name:       p.ExampleGPU,
			Provider:   "runpod",
			Key:        p.ID,
			MemoryGB:   p.MemoryGB,
			PricePerHr: p.PricePerHour,
			Stock:      p.Availability,
			InStock:    p.InStock,
		}
		if s, ok := matchCloudName(specs, p.ExampleGPU, p.ID); ok {
			r.Name = s.Name
			r.FP16 = s.FP16
			r.TensorCores = s.TensorCores
			r.CUDA = s.CUDA
			r.Arch = s.Architecture
		}
		out = append(out, r)
	}
	return out, nil
}

func gcpGPURows(specs []gpudb.Spec, query string) ([]gpuRow, error) {
	accels, err := gpudb.LoadGCP()
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]gpuRow, 0, len(accels))
	for _, a := range accels {
		hf := a.HFName
		if hf == "" {
			hf = a.Name
		}
		if q != "" {
			hay := strings.ToLower(a.ID + " " + a.Name + " " + hf + " " + a.Series)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		r := gpuRow{
			Name:       hf,
			Provider:   "gcp",
			Key:        a.Name,
			MemoryGB:   a.MemoryGB,
			PricePerHr: a.SpotHourlyApprox,
			Stock:      a.Series,
			InStock:    true,
		}
		if s, ok := gpudb.Match(specs, hf); ok {
			r.Name = s.Name
			if r.MemoryGB <= 0 {
				r.MemoryGB = s.MemorySize
			}
			r.FP16 = s.FP16
			r.TensorCores = s.TensorCores
			r.CUDA = s.CUDA
			r.Arch = s.Architecture
		}
		out = append(out, r)
	}
	return out, nil
}

func cloudJoinMaps() (map[string]runpod.Pool, map[string]gpudb.GCPAccelerator) {
	rp := map[string]runpod.Pool{}
	env := config.Load()
	var gpus []runpod.GPU
	if env.RunpodAPIKey != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if list, err := runpod.New(env.RunpodAPIKey).ListGPUs(ctx); err == nil {
			gpus = list
		}
	}
	if len(gpus) == 0 {
		gpus = runpod.OfflineCatalog()
	}
	for _, p := range runpod.SummarizePools(gpus) {
		rp[normalizeJoinKey(p.ExampleGPU)] = p
		rp[normalizeJoinKey(p.ID)] = p
		if s, ok := gpudb.ByName(p.ExampleGPU); ok {
			rp[normalizeJoinKey(s.Name)] = p
		}
	}
	gcpMap := map[string]gpudb.GCPAccelerator{}
	if accels, err := gpudb.LoadGCP(); err == nil {
		for _, a := range accels {
			gcpMap[normalizeJoinKey(a.Name)] = a
			if a.HFName != "" {
				gcpMap[normalizeJoinKey(a.HFName)] = a
			}
			gcpMap[normalizeJoinKey(a.ID)] = a
		}
	}
	return rp, gcpMap
}

func normalizeJoinKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	repl := strings.NewReplacer("nvidia ", "", "geforce ", "", " ", "", "-", "", "_", "")
	return repl.Replace(s)
}

func matchCloudName(specs []gpudb.Spec, example, poolID string) (gpudb.Spec, bool) {
	if s, ok := gpudb.Match(specs, example); ok {
		return s, true
	}
	// Pool id hints: ADA_24 → 4090-ish, HOPPER_80 → H100, etc.
	aliases := map[string]string{
		"ADA_24":    "GeForce RTX 4090",
		"ADA_48":    "L40",
		"AMPERE_16": "RTX A4000",
		"AMPERE_48": "A40",
		"AMPERE_80": "A100 SXM4 80 GB",
		"HOPPER_80": "H100 SXM5 80 GB",
	}
	if alias, ok := aliases[strings.ToUpper(poolID)]; ok {
		return gpudb.Match(specs, alias)
	}
	return gpudb.Spec{}, false
}

func sortGPURows(rows []gpuRow, sortKey string) {
	sortKey = strings.ToLower(strings.TrimSpace(sortKey))
	if sortKey == "" {
		sortKey = "best"
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		switch sortKey {
		case "cheapest":
			ap, bp := a.PricePerHr, b.PricePerHr
			if ap <= 0 && bp <= 0 {
				return a.Name < b.Name
			}
			if ap <= 0 {
				return false
			}
			if bp <= 0 {
				return true
			}
			if ap != bp {
				return ap < bp
			}
			return a.Name < b.Name
		case "value":
			av, bv := valueScore(a), valueScore(b)
			if av != bv {
				return av > bv
			}
			return a.Name < b.Name
		case "vram":
			if a.MemoryGB != b.MemoryGB {
				return a.MemoryGB > b.MemoryGB
			}
			return a.Name < b.Name
		case "name":
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		default: // best
			if a.FP16 != b.FP16 {
				return a.FP16 > b.FP16
			}
			if a.MemoryGB != b.MemoryGB {
				return a.MemoryGB > b.MemoryGB
			}
			if a.TensorCores != b.TensorCores {
				return a.TensorCores > b.TensorCores
			}
			return a.Name < b.Name
		}
	})
}

func valueScore(r gpuRow) float64 {
	if r.PricePerHr <= 0 || r.FP16 <= 0 {
		return 0
	}
	return r.FP16 / r.PricePerHr
}

func resolveGPUPreference(filter, want string) (gpuRow, error) {
	rows, err := buildGPURows(filter, "")
	if err != nil {
		return gpuRow{}, err
	}
	sortGPURows(rows, "best")
	want = strings.TrimSpace(want)
	if want == "" {
		return gpuRow{}, fmt.Errorf("empty GPU name")
	}
	wl := strings.ToLower(want)
	for _, r := range rows {
		if strings.EqualFold(r.Key, want) || strings.EqualFold(r.Name, want) {
			return r, nil
		}
	}
	for _, r := range rows {
		if strings.Contains(strings.ToLower(r.Name), wl) || strings.Contains(strings.ToLower(r.Key), wl) {
			return r, nil
		}
	}
	return gpuRow{}, fmt.Errorf("no GPU matching %q under --filter %s (try `runhug gpu list`)", want, filter)
}

func savedGPUPreference() *config.GPUPreference {
	return config.LoadSettings().GPU
}

func applySavedRunpodGPU(flagGPU string) string {
	if strings.TrimSpace(flagGPU) != "" {
		return flagGPU
	}
	p := savedGPUPreference()
	if p == nil || !strings.EqualFold(p.Provider, "runpod") || p.Key == "" {
		return ""
	}
	return p.Key
}

func applySavedGCPGPU(flagGPU string) string {
	if strings.TrimSpace(flagGPU) != "" {
		return flagGPU
	}
	p := savedGPUPreference()
	if p == nil || !strings.EqualFold(p.Provider, "gcp") || p.Key == "" {
		return ""
	}
	return p.Key
}
