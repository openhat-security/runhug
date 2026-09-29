package recommend

import (
	"context"
	"testing"

	"github.com/adamsiwiec1/runhug/internal/hf"
	"github.com/adamsiwiec1/runhug/internal/sizing"
)

func TestSameRepoQuantsRanksLighter(t *testing.T) {
	m := hf.Model{
		ID: "Org/Demo-7B-GGUF",
		Siblings: []hf.Sibling{
			{RFilename: "demo-Q8_0.gguf"},
			{RFilename: "demo-Q5_K_M.gguf"},
			{RFilename: "demo-Q4_K_M.gguf"},
			{RFilename: "demo-Q3_K_M.gguf"},
		},
		Tags: []string{"gguf"},
	}
	format := hf.DetectFormat(m)
	format.Quant = "q8_0"
	est := sizing.Estimate{
		Params:         7e9,
		BytesPerParam:  1.0,
		RequiredGB:     7 * 1.0 * 1.25,
		WeightGB:       7.0,
		OverheadFactor: 1.25,
	}

	alts := SuggestAlternatives(context.Background(), nil, m, format, est, 3)
	if len(alts) == 0 {
		t.Fatal("expected same-repo lighter quants")
	}
	if len(alts) > 3 {
		t.Fatalf("cap 3: got %d", len(alts))
	}
	for i := 1; i < len(alts); i++ {
		if alts[i].RequiredGB < alts[i-1].RequiredGB {
			t.Fatalf("not ranked by RequiredGB: %+v", alts)
		}
	}
	if alts[0].RequiredGB >= est.RequiredGB {
		t.Fatalf("top alt %v should beat current %.1f", alts[0], est.RequiredGB)
	}
	if alts[0].GGUFFile == "" || alts[0].Why != "lower quant" {
		t.Fatalf("expected same-repo file row, got %+v", alts[0])
	}
	// Lightest among returned should be first.
	if BytesPerQuant(alts[0].Quant) > BytesPerQuant("q4_k_m") {
		t.Fatalf("expected a light quant first, got %s", alts[0].Quant)
	}
}

func TestSameRepoEmptyWithoutSiblings(t *testing.T) {
	m := hf.Model{
		ID:   "Org/Demo-7B",
		Tags: []string{"safetensors"},
		Safetensors: &hf.Safetensors{
			Total:      7e9,
			Parameters: map[string]int64{"BF16": 7e9},
		},
	}
	format := hf.DetectFormat(m)
	est := sizing.EstimateModel(m, format, 8192)
	alts := SuggestAlternatives(context.Background(), nil, m, format, est, 3)
	if len(alts) != 0 {
		t.Fatalf("BF16 with no GGUF siblings should yield no same-repo alts, got %+v", alts)
	}
}

func TestSuggestAlternativesCapsAt3(t *testing.T) {
	m := hf.Model{
		ID: "Org/Demo-27B-GGUF",
		Siblings: []hf.Sibling{
			{RFilename: "a-Q8_0.gguf"},
			{RFilename: "a-Q6_K.gguf"},
			{RFilename: "a-Q5_K_M.gguf"},
			{RFilename: "a-Q4_K_M.gguf"},
			{RFilename: "a-Q3_K_M.gguf"},
			{RFilename: "a-Q2_K.gguf"},
		},
		Tags: []string{"gguf"},
	}
	format := hf.Format{Engine: hf.EngineGGUF, HasGGUF: true, Quant: "q8_0"}
	est := sizing.Estimate{Params: 27e9, BytesPerParam: 1, RequiredGB: 33.75, WeightGB: 27, OverheadFactor: 1.25}
	alts := SuggestAlternatives(context.Background(), nil, m, format, est, 3)
	if len(alts) != 3 {
		t.Fatalf("want 3, got %d (%+v)", len(alts), alts)
	}
}

func TestRelatedHubGGUFSurvivesFilter(t *testing.T) {
	src := hf.Model{
		ID:     "Qwen/Qwen3.8-27B",
		Author: "Qwen",
		Tags:   []string{"safetensors"},
		Safetensors: &hf.Safetensors{
			Total:      27e9,
			Parameters: map[string]int64{"BF16": 27e9},
		},
	}
	format := hf.DetectFormat(src)
	est := sizing.EstimateModel(src, format, 8192)

	stub := stubSearcher{models: []hf.Model{
		{
			ID:   "unsloth/Qwen3.8-27B-GGUF",
			Tags: []string{"gguf"},
			Siblings: []hf.Sibling{
				{RFilename: "qwen-Q4_K_M.gguf"},
			},
		},
		{
			ID:   "someone/Huge-70B",
			Tags: []string{"safetensors"},
			Safetensors: &hf.Safetensors{
				Total:      70e9,
				Parameters: map[string]int64{"BF16": 70e9},
			},
		},
	}}

	alts := SuggestAlternatives(context.Background(), stub, src, format, est, 3)
	if len(alts) == 0 {
		t.Fatal("expected Hub GGUF alt")
	}
	found := false
	for _, a := range alts {
		if a.RepoID == "unsloth/Qwen3.8-27B-GGUF" {
			found = true
			if a.RequiredGB >= est.RequiredGB {
				t.Fatalf("GGUF alt should be cheaper: %+v vs %.1f", a, est.RequiredGB)
			}
		}
		if a.RepoID == "someone/Huge-70B" {
			t.Fatalf("heavier model should be filtered: %+v", a)
		}
	}
	if !found {
		t.Fatalf("missing unsloth GGUF in %+v", alts)
	}
}

type stubSearcher struct {
	models []hf.Model
}

func (s stubSearcher) Search(ctx context.Context, opts hf.SearchOpts) ([]hf.Model, error) {
	return s.models, nil
}

func TestBytesPerQuantOrdering(t *testing.T) {
	if BytesPerQuant("q4_k_m") >= BytesPerQuant("q8_0") {
		t.Fatal("q4 should be lighter than q8")
	}
	if BytesPerQuant("q3_k_m") >= BytesPerQuant("q4_k_m") {
		t.Fatal("q3 should be lighter than q4")
	}
}
