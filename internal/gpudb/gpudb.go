// Package gpudb provides a trimmed NVIDIA GPU specifications index and a GCP
// accelerator catalog.
//
// NVIDIA data is derived from https://huggingface.co/datasets/Jr23xd23/gpu-database
// (Apache-2.0), originally sourced from TechPowerUp via dbgpu / RightNow.
// FP16/FP32 values are TechPowerUp vector TFLOPS, not NVIDIA tensor-core peak.
//
// GCP data is a curated Compute Engine catalog (accelerator types + A/G-series)
// shipped with the CLI; refresh with `runhug gpu update`.
package gpudb

import (
	_ "embed"
	"strings"
)

//go:embed data/nvidia.json
var nvidiaJSON []byte

//go:embed data/amd.json
var amdJSON []byte

// Spec is one GPU row from the vendored NVIDIA/AMD index.
type Spec struct {
	Name            string  `json:"name"`
	Vendor          string  `json:"vendor,omitempty"` // nvidia | amd
	Architecture    string  `json:"architecture,omitempty"`
	Generation      string  `json:"generation,omitempty"`
	MemorySize      float64 `json:"memorySize"`
	MemoryBandwidth float64 `json:"memoryBandwidth,omitempty"`
	MemoryType      string  `json:"memoryType,omitempty"`
	FP16            float64 `json:"fp16,omitempty"`
	FP32            float64 `json:"fp32,omitempty"`
	FP64            float64 `json:"fp64,omitempty"`
	TensorCores     int     `json:"tensorCores,omitempty"`
	CUDA            string  `json:"cuda,omitempty"`
	Shaders         int     `json:"shaders,omitempty"`
	TDP             int     `json:"tdp,omitempty"`
}

// ByName returns the best exact or fuzzy match for a marketing / catalog name.
func ByName(name string) (Spec, bool) {
	specs, err := Load()
	if err != nil || len(specs) == 0 {
		return Spec{}, false
	}
	return Match(specs, name)
}

// Search filters specs whose name/architecture/generation contain query (case-insensitive).
func Search(specs []Spec, query string) []Spec {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return specs
	}
	out := make([]Spec, 0, len(specs))
	for _, s := range specs {
		hay := strings.ToLower(s.Name + " " + s.Architecture + " " + s.Generation)
		if strings.Contains(hay, q) {
			out = append(out, s)
		}
	}
	return out
}

// Match finds the best Spec for a free-form GPU name.
func Match(specs []Spec, name string) (Spec, bool) {
	n := normalizeName(name)
	if n == "" {
		return Spec{}, false
	}
	var exact Spec
	exactOK := false
	for _, s := range specs {
		if normalizeName(s.Name) == n {
			return s, true
		}
		if !exactOK && strings.EqualFold(s.Name, name) {
			exact, exactOK = s, true
		}
	}
	if exactOK {
		return exact, true
	}
	bestScore := 0
	var best Spec
	for _, s := range specs {
		sn := normalizeName(s.Name)
		score := 0
		if sn == n {
			return s, true
		}
		if strings.Contains(sn, n) {
			score = len(n)*2 + len(sn)
		} else if strings.Contains(n, sn) && len(sn) >= 4 {
			score = len(sn)*2 + 1
		} else {
			score = tokenOverlap(n, sn)
		}
		if score > bestScore {
			bestScore = score
			best = s
		}
	}
	if bestScore >= 6 {
		return best, true
	}
	return Spec{}, false
}
