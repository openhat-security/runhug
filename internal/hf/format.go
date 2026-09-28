package hf

import "strings"

type Engine string

const (
	EngineVLLM    Engine = "vllm"
	EngineGGUF    Engine = "gguf"
	EngineUnknown Engine = "unknown"
)

type Format struct {
	Engine         Engine
	HasSafetensors bool
	HasGGUF        bool
	Quant          string
}

func DetectFormat(m Model) Format {
	f := Format{Engine: EngineUnknown}
	for _, tag := range m.Tags {
		switch strings.ToLower(tag) {
		case "safetensors":
			f.HasSafetensors = true
		case "gguf":
			f.HasGGUF = true
		case "awq":
			f.Quant = "awq"
		case "gptq":
			f.Quant = "gptq"
		case "squeezellm":
			f.Quant = "squeezellm"
		}
	}
	if strings.EqualFold(m.LibraryName, "gguf") {
		f.HasGGUF = true
	}
	for _, s := range m.Siblings {
		name := strings.ToLower(s.RFilename)
		if strings.HasSuffix(name, ".safetensors") {
			f.HasSafetensors = true
		}
		if strings.HasSuffix(name, ".gguf") {
			f.HasGGUF = true
			if f.Quant == "" {
				if q := QuantFromFilename(name); q != "" {
					f.Quant = q
				}
			}
		}
	}
	if m.Safetensors != nil && m.Safetensors.Total > 0 {
		f.HasSafetensors = true
	}
	switch {
	case f.HasGGUF && !f.HasSafetensors:
		f.Engine = EngineGGUF
	case f.HasSafetensors:
		f.Engine = EngineVLLM
	case f.HasGGUF:
		f.Engine = EngineGGUF
	}
	return f
}

// QuantFromFilename extracts a GGUF quant tag (q4_k_m, iq4_xs, …) from a sibling name.
func QuantFromFilename(name string) string {
	low := strings.ToLower(name)
	// Prefer longer / more specific labels first.
	for _, q := range []string{
		"iq4_xs", "iq4_nl", "iq3_m", "iq3_xxs", "iq2_xxs", "iq2_xs", "iq1_s",
		"q4_k_m", "q4_k_s", "q5_k_m", "q5_k_s", "q3_k_m", "q3_k_s", "q3_k_l",
		"q6_k", "q8_0", "q4_0", "q5_0", "q2_k", "q3_k", "q4_k", "q5_k",
		"fp16", "bf16", "f16", "f32",
	} {
		if strings.Contains(low, q) {
			return q
		}
	}
	return ""
}
