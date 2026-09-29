package recommend

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/adamsiwiec1/runhug/internal/hf"
	"github.com/adamsiwiec1/runhug/internal/sizing"
)

// Alternative is a cheaper / smaller fit suggested from inspect.
type Alternative struct {
	RepoID     string  `json:"repo_id"`
	Quant      string  `json:"quant,omitempty"`
	Engine     string  `json:"engine"`
	WeightGB   float64 `json:"weight_gb"`
	RequiredGB float64 `json:"required_gb"`
	Why        string  `json:"why"`
	GGUFFile   string  `json:"gguf_file,omitempty"`
}

// HubSearcher is satisfied by *hf.Client; tests can stub Search.
type HubSearcher interface {
	Search(ctx context.Context, opts hf.SearchOpts) ([]hf.Model, error)
}

var (
	instructNoiseRe = regexp.MustCompile(`(?i)(-Instruct|-Chat|-it|-GGUF)+$`)
	sizeTokenRe     = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)[bB]`)
)

// SuggestAlternatives returns up to limit cheaper fits: same-repo lighter GGUF
// quants, plus related Hub repos when the inspected model is large or BF16/vLLM.
func SuggestAlternatives(ctx context.Context, client HubSearcher, model hf.Model, format hf.Format, est sizing.Estimate, limit int) []Alternative {
	if limit <= 0 {
		limit = 3
	}
	var out []Alternative
	seen := map[string]bool{}

	add := func(a Alternative) {
		if a.RepoID == "" {
			return
		}
		key := strings.ToLower(a.RepoID) + "|" + strings.ToLower(a.Quant) + "|" + strings.ToLower(a.GGUFFile)
		if seen[key] {
			return
		}
		if est.RequiredGB > 0 && a.RequiredGB > 0 && a.RequiredGB >= est.RequiredGB {
			return
		}
		seen[key] = true
		out = append(out, a)
	}

	for _, a := range sameRepoQuants(model, format, est) {
		add(a)
	}

	if client != nil && wantRelatedSearch(format, est) {
		for _, a := range relatedHubAlts(ctx, client, model, format, est) {
			add(a)
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RequiredGB != out[j].RequiredGB {
			if out[i].RequiredGB <= 0 {
				return false
			}
			if out[j].RequiredGB <= 0 {
				return true
			}
			return out[i].RequiredGB < out[j].RequiredGB
		}
		return publisherBoost(out[i].RepoID, model) > publisherBoost(out[j].RepoID, model)
	})

	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func wantRelatedSearch(format hf.Format, est sizing.Estimate) bool {
	if est.RequiredGB >= 24 {
		return true
	}
	return format.Engine == hf.EngineVLLM || format.Quant == "" || isFullPrecision(format.Quant)
}

func isFullPrecision(q string) bool {
	switch strings.ToLower(q) {
	case "fp16", "bf16", "f16", "f32", "float16", "bfloat16", "float32":
		return true
	default:
		return false
	}
}

func sameRepoQuants(model hf.Model, format hf.Format, est sizing.Estimate) []Alternative {
	files := hf.GGUFSiblings(model)
	if len(files) == 0 {
		return nil
	}
	curBPP := est.BytesPerParam
	if format.Quant != "" {
		if b := BytesPerQuant(format.Quant); b > 0 {
			curBPP = b
		}
	}
	repo := model.RepoID()
	var out []Alternative
	seenQ := map[string]bool{}
	for _, f := range files {
		q := hf.QuantFromFilename(f)
		if q == "" {
			continue
		}
		ql := strings.ToLower(q)
		if format.Quant != "" && strings.EqualFold(q, format.Quant) {
			continue
		}
		bpp := BytesPerQuant(q)
		if bpp <= 0 || bpp >= curBPP {
			continue
		}
		if seenQ[ql] {
			continue
		}
		seenQ[ql] = true
		w, req := estimateQuantGB(est.Params, q)
		if w <= 0 {
			continue
		}
		out = append(out, Alternative{
			RepoID:     repo,
			Quant:      ql,
			Engine:     string(hf.EngineGGUF),
			WeightGB:   w,
			RequiredGB: req,
			Why:        "lower quant",
			GGUFFile:   f,
		})
	}
	return out
}

func relatedHubAlts(ctx context.Context, client HubSearcher, model hf.Model, format hf.Format, est sizing.Estimate) []Alternative {
	base := baseModelToken(model.RepoID())
	if base == "" {
		return nil
	}
	author := model.Author
	if author == "" {
		if i := strings.Index(model.RepoID(), "/"); i > 0 {
			author = model.RepoID()[:i]
		}
	}

	queries := []string{base + " GGUF"}
	if author != "" {
		queries = append(queries, author+" "+base+" GGUF")
	}
	for _, sib := range smallerSizeHints(model.RepoID()) {
		queries = append(queries, author+" "+sib)
	}

	var hits []hf.Model
	seenID := map[string]bool{strings.ToLower(model.RepoID()): true}
	for _, q := range queries {
		if len(hits) >= 15 {
			break
		}
		models, err := client.Search(ctx, hf.SearchOpts{
			Query:  q,
			Limit:  15,
			Sort:   "relevance",
			Expand: false,
		})
		if err != nil {
			continue
		}
		for _, m := range models {
			id := strings.ToLower(m.RepoID())
			if id == "" || seenID[id] {
				continue
			}
			seenID[id] = true
			hits = append(hits, m)
			if len(hits) >= 15 {
				break
			}
		}
	}

	var out []Alternative
	for _, m := range hits {
		f := hf.DetectFormat(m)
		e := sizing.EstimateModel(m, f, 8192)
		if e.RequiredGB <= 0 {
			continue
		}
		if est.RequiredGB > 0 && e.RequiredGB >= est.RequiredGB {
			continue
		}
		why := "related Hub"
		idLow := strings.ToLower(m.RepoID())
		switch {
		case strings.HasSuffix(idLow, "-gguf") || f.Engine == hf.EngineGGUF:
			why = "GGUF port"
		case sameOrg(m.RepoID(), model.RepoID()) && smallerThan(m.RepoID(), model.RepoID()):
			why = "smaller sibling"
		case publisherBoost(m.RepoID(), model) >= 2:
			why = "quant publisher"
		}
		quant := f.Quant
		if f.Engine == hf.EngineGGUF && quant == "" {
			if _, q, ok := hf.PickGGUF(m); ok {
				quant = strings.ToLower(q)
			}
		}
		out = append(out, Alternative{
			RepoID:     m.RepoID(),
			Quant:      strings.ToLower(quant),
			Engine:     string(f.Engine),
			WeightGB:   e.WeightGB,
			RequiredGB: e.RequiredGB,
			Why:        why,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		bi, bj := publisherBoost(out[i].RepoID, model), publisherBoost(out[j].RepoID, model)
		if bi != bj {
			return bi > bj
		}
		return out[i].RequiredGB < out[j].RequiredGB
	})
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

func baseModelToken(repoID string) string {
	name := repoID
	if i := strings.LastIndex(repoID, "/"); i >= 0 {
		name = repoID[i+1:]
	}
	name = instructNoiseRe.ReplaceAllString(name, "")
	return strings.TrimSpace(name)
}

func smallerSizeHints(repoID string) []string {
	name := baseModelToken(repoID)
	m := sizeTokenRe.FindStringSubmatch(name)
	if len(m) < 2 {
		return nil
	}
	cur, err := strconv.ParseFloat(m[1], 64)
	if err != nil || cur <= 0 {
		return nil
	}
	candidates := []float64{14, 9, 8, 7, 4, 3, 1.5, 1}
	var out []string
	for _, c := range candidates {
		if c >= cur {
			continue
		}
		label := strings.TrimRight(strings.TrimRight(strconv.FormatFloat(c, 'f', 1, 64), "0"), ".")
		hint := sizeTokenRe.ReplaceAllString(name, label+"B")
		if hint != name {
			out = append(out, hint)
		}
	}
	return out
}

func sameOrg(a, b string) bool {
	oa, ob := orgOf(a), orgOf(b)
	return oa != "" && strings.EqualFold(oa, ob)
}

func orgOf(repoID string) string {
	if i := strings.Index(repoID, "/"); i > 0 {
		return repoID[:i]
	}
	return ""
}

func smallerThan(candidate, current string) bool {
	cb := sizeFromID(candidate)
	cur := sizeFromID(current)
	return cb > 0 && cur > 0 && cb < cur
}

func sizeFromID(repoID string) float64 {
	m := sizeTokenRe.FindAllStringSubmatch(repoID, -1)
	if len(m) == 0 {
		return 0
	}
	last := m[len(m)-1]
	v, _ := strconv.ParseFloat(last[1], 64)
	return v
}

func publisherBoost(repoID string, src hf.Model) int {
	low := strings.ToLower(repoID)
	score := 0
	if sameOrg(repoID, src.RepoID()) {
		score += 3
	}
	if strings.HasPrefix(low, "unsloth/") || strings.HasPrefix(low, "bartowski/") {
		score += 2
	}
	if strings.HasSuffix(low, "-gguf") {
		score += 1
	}
	return score
}

// BytesPerQuant approximates bytes/param for a GGUF quant tag.
func BytesPerQuant(q string) float64 {
	switch strings.ToLower(strings.TrimSpace(q)) {
	case "iq1_s":
		return 0.25
	case "iq2_xxs", "iq2_xs", "q2_k":
		return 0.3
	case "iq3_xxs", "iq3_m", "q3_k_s", "q3_k_m", "q3_k_l", "q3_k":
		return 0.4
	case "iq4_xs", "iq4_nl", "q4_0", "q4_k_s", "q4_k_m", "q4_k", "awq", "gptq", "squeezellm":
		return 0.5
	case "q5_0", "q5_k_s", "q5_k_m", "q5_k":
		return 0.625
	case "q6_k":
		return 0.75
	case "q8_0", "int8":
		return 1.0
	case "fp16", "bf16", "f16", "float16", "bfloat16":
		return 2.0
	case "f32", "float32", "fp32":
		return 4.0
	default:
		return 0
	}
}

func estimateQuantGB(params int64, quant string) (weightGB, requiredGB float64) {
	bpp := BytesPerQuant(quant)
	if params <= 0 || bpp <= 0 {
		return 0, 0
	}
	w := float64(params) * bpp / 1e9
	return w, w * 1.25
}
