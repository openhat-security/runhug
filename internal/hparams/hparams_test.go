package hparams

import (
	"math"
	"testing"

	"github.com/adamsiwiec1/runhug/internal/hf"
)

func fval(p *float64) float64 {
	if p == nil {
		return math.NaN()
	}
	return *p
}

func TestMatchFamilyQwen(t *testing.T) {
	m := hf.Model{ID: "Qwen/Qwen2.5-7B-Instruct"}
	s := Recommend(m, hf.Format{Engine: hf.EngineVLLM}, nil)
	if s.Source != "qwen2.5" {
		t.Fatalf("source %q", s.Source)
	}
	if fval(s.Temperature) != 0.7 || fval(s.TopP) != 0.8 {
		t.Fatalf("qwen2.5 %+v temp=%v top_p=%v", s, fval(s.Temperature), fval(s.TopP))
	}
}

func TestMatchFamilyQwen3(t *testing.T) {
	m := hf.Model{ID: "Qwen/Qwen3-8B"}
	s := Recommend(m, hf.Format{}, nil)
	if s.Source != "qwen3" {
		t.Fatalf("source %q", s.Source)
	}
	if fval(s.Temperature) != 0.6 {
		t.Fatalf("temp %v", fval(s.Temperature))
	}
}

func TestQuantNudgeGGUF(t *testing.T) {
	m := hf.Model{ID: "Qwen/Qwen2.5-7B-Instruct-GGUF"}
	full := Recommend(m, hf.Format{Engine: hf.EngineGGUF, Quant: "Q8_0"}, nil)
	heavy := Recommend(m, hf.Format{Engine: hf.EngineGGUF, Quant: "Q3_K_M"}, nil)
	if QuantSeverity("Q8_0") != QuantFull {
		t.Fatalf("q8 tier %s", QuantSeverity("Q8_0"))
	}
	if QuantSeverity("Q3_K_M") != QuantHeavy {
		t.Fatalf("q3 tier %s", QuantSeverity("Q3_K_M"))
	}
	if fval(heavy.Temperature) >= fval(full.Temperature) {
		t.Fatalf("heavy temp %v should be < full %v", fval(heavy.Temperature), fval(full.Temperature))
	}
	if heavy.MinP == nil || *heavy.MinP < 0.05 {
		t.Fatalf("heavy min_p %+v", heavy.MinP)
	}
	if heavy.RepetitionPenalty == nil || *heavy.RepetitionPenalty < 1.08 {
		t.Fatalf("heavy rep %+v", heavy.RepetitionPenalty)
	}
	if fval(heavy.Temperature) < 0.05 {
		t.Fatal("temp floor")
	}
}

func TestQuantTiers(t *testing.T) {
	cases := []struct {
		q    string
		want QuantTier
	}{
		{"", QuantFull},
		{"Q8_0", QuantFull},
		{"q5_k_m", QuantLight},
		{"Q4_K_M", QuantModerate},
		{"iq4_xs", QuantModerate},
		{"Q2_K", QuantHeavy},
		{"awq", QuantModerate},
		{"gptq", QuantModerate},
		{"fp8", QuantLight},
	}
	for _, c := range cases {
		if got := QuantSeverity(c.q); got != c.want {
			t.Fatalf("%s: got %s want %s", c.q, got, c.want)
		}
	}
}

func TestGenerationConfigWins(t *testing.T) {
	m := hf.Model{ID: "Qwen/Qwen2.5-7B-Instruct"}
	gen := map[string]any{"temperature": 0.2, "top_p": 0.99}
	s := Recommend(m, hf.Format{Quant: "awq"}, gen)
	if fval(s.Temperature) != 0.2 || fval(s.TopP) != 0.99 {
		t.Fatalf("overlay lost: temp=%v top_p=%v", fval(s.Temperature), fval(s.TopP))
	}
	if s.Source != "qwen2.5+awq+generation_config" && s.Source != "generation_config.json" {
		// overlayOnto keeps base source then we append +generation_config
		if s.Source != "qwen2.5+awq+generation_config" {
			t.Fatalf("source %q", s.Source)
		}
	}
}

func TestParseAndFormat(t *testing.T) {
	s, err := Parse([]string{"temp=0.4", "top_p=0.9", "top_k=40"})
	if err != nil {
		t.Fatal(err)
	}
	if fval(s.Temperature) != 0.4 || fval(s.TopP) != 0.9 || s.TopK == nil || *s.TopK != 40 {
		t.Fatalf("%+v", s)
	}
	block := s.FormatBlock()
	if block == "" {
		t.Fatal("empty format")
	}
	if _, err := Parse([]string{"nope"}); err == nil {
		t.Fatal("want error")
	}
}

func TestEmptyNone(t *testing.T) {
	var s Sampling
	if !s.Empty() {
		t.Fatal("zero should be empty")
	}
	if s.FormatBlock() != "none (engine defaults)" {
		t.Fatalf("%q", s.FormatBlock())
	}
}
