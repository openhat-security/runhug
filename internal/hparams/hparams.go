package hparams

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/adamsiwiec1/runhug/internal/hf"
)

// Sampling is a recommended (or user-chosen) set of generation defaults.
// Pointer fields distinguish "unset" from zero.
type Sampling struct {
	Temperature       *float64 `json:"temperature,omitempty"`
	TopP              *float64 `json:"top_p,omitempty"`
	TopK              *int     `json:"top_k,omitempty"`
	MinP              *float64 `json:"min_p,omitempty"`
	RepetitionPenalty *float64 `json:"repetition_penalty,omitempty"`
	MaxTokens         *int     `json:"max_tokens,omitempty"`
	Stop              []string `json:"stop,omitempty"`
	// Source is a short label, e.g. "qwen2.5+q4_k_m" or "generation_config.json".
	Source string `json:"source,omitempty"`
}

type QuantTier int

const (
	QuantFull QuantTier = iota
	QuantLight
	QuantModerate
	QuantHeavy
)

func (t QuantTier) String() string {
	switch t {
	case QuantHeavy:
		return "heavy"
	case QuantModerate:
		return "moderate"
	case QuantLight:
		return "light"
	default:
		return "full"
	}
}

type familyPreset struct {
	Name              string
	Temperature       float64
	TopP              float64
	TopK              int
	MinP              float64
	RepetitionPenalty float64
}

var presets = []struct {
	keys   []string
	preset familyPreset
}{
	{
		keys: []string{"qwen3", "qwen3_5", "qwen3.5", "qwythos"},
		preset: familyPreset{
			Name: "qwen3", Temperature: 0.6, TopP: 0.95, TopK: 20, MinP: 0,
		},
	},
	{
		keys: []string{"qwen"},
		preset: familyPreset{
			Name: "qwen2.5", Temperature: 0.7, TopP: 0.8, TopK: 20, MinP: 0,
		},
	},
	{
		keys: []string{"deepseek-r1", "deepseek_r1", "deepseek-ai/deepseek-r1"},
		preset: familyPreset{
			Name: "deepseek-r1", Temperature: 0.6, TopP: 0.95, TopK: 0, MinP: 0,
		},
	},
	{
		keys: []string{"llama-3", "llama3", "meta-llama/llama-3"},
		preset: familyPreset{
			Name: "llama3", Temperature: 0.6, TopP: 0.9, TopK: 0, MinP: 0,
		},
	},
	{
		keys: []string{"ministral", "mistral"},
		preset: familyPreset{
			Name: "mistral", Temperature: 0.15, TopP: 0.9, TopK: 0, MinP: 0,
		},
	},
}

var generic = familyPreset{
	Name: "generic", Temperature: 0.7, TopP: 0.9, TopK: 0, MinP: 0,
}

// Recommend builds sampling defaults: family baseline → quant-aware nudge →
// generation_config.json overlay (explicit model values always win).
func Recommend(m hf.Model, format hf.Format, gen map[string]any) Sampling {
	parts := make([]string, 0, 1+len(m.FamilyHints()))
	parts = append(parts, m.RepoID())
	parts = append(parts, m.FamilyHints()...)
	preset := matchFamily(parts...)
	s := fromPreset(preset)
	tier := QuantSeverity(format.Quant)
	applyQuantNudge(&s, tier)
	src := preset.Name
	if format.Quant != "" {
		src += "+" + strings.ToLower(format.Quant)
	} else if tier != QuantFull {
		src += "+" + tier.String()
	}
	s.Source = src
	if overlay := fromGenerationConfig(gen); !overlay.Empty() {
		s = overlayOnto(s, overlay)
		s.Source = src + "+generation_config"
	}
	return s
}

func matchFamily(parts ...string) familyPreset {
	id := strings.ToLower(strings.Join(parts, " "))
	for _, row := range presets {
		for _, k := range row.keys {
			if strings.Contains(id, k) {
				return row.preset
			}
		}
	}
	return generic
}

func fromPreset(p familyPreset) Sampling {
	s := Sampling{Source: p.Name}
	s.Temperature = f64(p.Temperature)
	s.TopP = f64(p.TopP)
	if p.TopK > 0 {
		s.TopK = i64(p.TopK)
	}
	s.MinP = f64(p.MinP)
	if p.RepetitionPenalty > 0 {
		s.RepetitionPenalty = f64(p.RepetitionPenalty)
	}
	return s
}

// QuantSeverity maps a DetectFormat / GGUF quant label to a noise tier.
func QuantSeverity(quant string) QuantTier {
	q := strings.ToLower(strings.TrimSpace(quant))
	q = strings.ReplaceAll(q, "-", "_")
	if q == "" || q == "none" || q == "unknown" {
		return QuantFull
	}
	switch q {
	case "awq", "gptq", "squeezellm":
		return QuantModerate
	case "fp8", "f8":
		return QuantLight
	case "fp16", "bf16", "f16", "f32", "fp32":
		return QuantFull
	}
	// GGUF / llama.cpp labels.
	switch {
	case strings.Contains(q, "iq1") || strings.Contains(q, "q2") || strings.HasPrefix(q, "iq2"):
		return QuantHeavy
	case strings.Contains(q, "q3") || strings.HasPrefix(q, "iq3"):
		return QuantHeavy
	case strings.Contains(q, "iq4") || strings.Contains(q, "q4"):
		return QuantModerate
	case strings.Contains(q, "q5") || strings.Contains(q, "q6"):
		return QuantLight
	case strings.Contains(q, "q8"):
		return QuantFull
	}
	return QuantFull
}

func applyQuantNudge(s *Sampling, tier QuantTier) {
	if s == nil || tier == QuantFull {
		return
	}
	var dTemp, minP, rep float64
	switch tier {
	case QuantHeavy:
		dTemp, minP, rep = -0.15, 0.05, 1.08
	case QuantModerate:
		dTemp, minP, rep = -0.08, 0.03, 1.05
	case QuantLight:
		dTemp, minP, rep = -0.05, 0.02, 1.02
	}
	if s.Temperature != nil {
		v := math.Max(0.05, *s.Temperature+dTemp)
		s.Temperature = f64(v)
	}
	if s.MinP == nil || *s.MinP < minP {
		s.MinP = f64(minP)
	}
	if s.RepetitionPenalty == nil || *s.RepetitionPenalty < rep {
		s.RepetitionPenalty = f64(rep)
	}
}

func fromGenerationConfig(gen map[string]any) Sampling {
	if len(gen) == 0 {
		return Sampling{}
	}
	s := Sampling{Source: "generation_config.json"}
	s.Temperature = numPtr(gen, "temperature")
	s.TopP = numPtr(gen, "top_p")
	s.MinP = numPtr(gen, "min_p")
	s.RepetitionPenalty = numPtr(gen, "repetition_penalty")
	if s.RepetitionPenalty == nil {
		s.RepetitionPenalty = numPtr(gen, "repeat_penalty")
	}
	s.TopK = intPtr(gen, "top_k")
	s.MaxTokens = intPtr(gen, "max_new_tokens")
	if s.MaxTokens == nil {
		s.MaxTokens = intPtr(gen, "max_length")
	}
	if stops := stringSlice(gen["stop"]); len(stops) > 0 {
		s.Stop = stops
	} else if stops = stringSlice(gen["eos_token_id"]); len(stops) > 0 {
		s.Stop = stops
	}
	return s
}

func overlayOnto(base, over Sampling) Sampling {
	out := base
	if over.Temperature != nil {
		out.Temperature = over.Temperature
	}
	if over.TopP != nil {
		out.TopP = over.TopP
	}
	if over.TopK != nil {
		out.TopK = over.TopK
	}
	if over.MinP != nil {
		out.MinP = over.MinP
	}
	if over.RepetitionPenalty != nil {
		out.RepetitionPenalty = over.RepetitionPenalty
	}
	if over.MaxTokens != nil {
		out.MaxTokens = over.MaxTokens
	}
	if len(over.Stop) > 0 {
		out.Stop = append([]string(nil), over.Stop...)
	}
	return out
}

// Merge copies src onto dst; src non-nil fields win.
func Merge(dst, src Sampling) Sampling {
	return overlayOnto(dst, src)
}

func (s Sampling) Empty() bool {
	return s.Temperature == nil && s.TopP == nil && s.TopK == nil &&
		s.MinP == nil && s.RepetitionPenalty == nil && s.MaxTokens == nil &&
		len(s.Stop) == 0
}

// FormatBlock is a compact one-line summary for CLI plans.
func (s Sampling) FormatBlock() string {
	if s.Empty() {
		return "none (engine defaults)"
	}
	var parts []string
	if s.Temperature != nil {
		parts = append(parts, fmt.Sprintf("temp=%.2f", *s.Temperature))
	}
	if s.TopP != nil {
		parts = append(parts, fmt.Sprintf("top_p=%.2f", *s.TopP))
	}
	if s.TopK != nil {
		parts = append(parts, fmt.Sprintf("top_k=%d", *s.TopK))
	}
	if s.MinP != nil {
		parts = append(parts, fmt.Sprintf("min_p=%.2f", *s.MinP))
	}
	if s.RepetitionPenalty != nil {
		parts = append(parts, fmt.Sprintf("rep=%.2f", *s.RepetitionPenalty))
	}
	if s.MaxTokens != nil {
		parts = append(parts, fmt.Sprintf("max_tokens=%d", *s.MaxTokens))
	}
	out := strings.Join(parts, "  ")
	if s.Source != "" {
		out += "  (" + s.Source + ")"
	}
	return out
}

// Parse reads KEY=VALUE pairs (temperature, top_p, top_k, min_p, repetition_penalty, max_tokens).
func Parse(pairs []string) (Sampling, error) {
	s := Sampling{Source: "custom"}
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			return Sampling{}, fmt.Errorf("sampling --set wants KEY=VALUE, got %q", p)
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		switch k {
		case "temperature", "temp":
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return Sampling{}, fmt.Errorf("temperature: %w", err)
			}
			s.Temperature = f64(n)
		case "top_p", "top-p", "topp":
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return Sampling{}, fmt.Errorf("top_p: %w", err)
			}
			s.TopP = f64(n)
		case "top_k", "top-k", "topk":
			n, err := strconv.Atoi(v)
			if err != nil {
				return Sampling{}, fmt.Errorf("top_k: %w", err)
			}
			s.TopK = i64(n)
		case "min_p", "min-p", "minp":
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return Sampling{}, fmt.Errorf("min_p: %w", err)
			}
			s.MinP = f64(n)
		case "repetition_penalty", "repeat_penalty", "rep":
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return Sampling{}, fmt.Errorf("repetition_penalty: %w", err)
			}
			s.RepetitionPenalty = f64(n)
		case "max_tokens", "max-tokens", "n":
			n, err := strconv.Atoi(v)
			if err != nil {
				return Sampling{}, fmt.Errorf("max_tokens: %w", err)
			}
			s.MaxTokens = i64(n)
		default:
			return Sampling{}, fmt.Errorf("unknown sampling key %q", k)
		}
	}
	return s, nil
}

func f64(v float64) *float64 { return &v }
func i64(v int) *int         { return &v }

func numPtr(m map[string]any, key string) *float64 {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	switch t := v.(type) {
	case float64:
		return f64(t)
	case float32:
		return f64(float64(t))
	case int:
		return f64(float64(t))
	case int64:
		return f64(float64(t))
	case string:
		n, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return nil
		}
		return f64(n)
	default:
		return nil
	}
}

func intPtr(m map[string]any, key string) *int {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	switch t := v.(type) {
	case float64:
		n := int(t)
		return &n
	case int:
		return i64(t)
	case int64:
		n := int(t)
		return &n
	case string:
		n, err := strconv.Atoi(t)
		if err != nil {
			return nil
		}
		return i64(n)
	default:
		return nil
	}
}

func stringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if t != "" {
			return []string{t}
		}
	}
	return nil
}
