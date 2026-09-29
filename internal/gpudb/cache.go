package gpudb

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/adamsiwiec1/runhug/internal/config"
)

//go:embed data/gcp.json
var gcpJSON []byte

// GCPAccelerator is one Compute Engine GPU offering (accelerator type or A/G-series chip).
type GCPAccelerator struct {
	ID               string  `json:"id"` // e.g. nvidia-tesla-t4, nvidia-l4
	Name             string  `json:"name"`
	HFName           string  `json:"hfName,omitempty"` // gpudb Spec name for join
	Series           string  `json:"series,omitempty"` // N1 | G2 | A2 | A3 | A4 | G4
	MemoryGB         float64 `json:"memoryGB"`
	MachineHint      string  `json:"machineHint,omitempty"`
	Attach           string  `json:"attach,omitempty"` // accelerator | machine
	SpotHourlyApprox float64 `json:"spotHourlyApprox,omitempty"`
}

const (
	cacheSubdir    = "gpudb"
	nvidiaCache    = "nvidia.json"
	amdCache       = "amd.json"
	gcpCache       = "gcp.json"
	NVIDIAFetchURL = "https://huggingface.co/datasets/Jr23xd23/gpu-database/resolve/main/data/nvidia/all.json"
	AMDFetchURL    = "https://huggingface.co/datasets/Jr23xd23/gpu-database/resolve/main/data/amd/all.json"
	// GCPCatalogURL is the shipped catalog on the public repo (same path as embed).
	GCPCatalogURL = "https://raw.githubusercontent.com/openhat-security/runhug/main/internal/gpudb/data/gcp.json"
)

var (
	loadOnce sync.Once
	allSpecs []Spec
	loadErr  error

	gcpOnce sync.Once
	gcpAll  []GCPAccelerator
	gcpErr  error
	specMu  sync.Mutex
)

// CacheDir is ~/.config/runhug/gpudb (or RUNHUG_CONFIG parent).
func CacheDir() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, cacheSubdir), nil
}

// Invalidate clears in-memory caches so the next Load* re-reads embed/disk.
func Invalidate() {
	specMu.Lock()
	defer specMu.Unlock()
	loadOnce = sync.Once{}
	allSpecs = nil
	loadErr = nil
	gcpOnce = sync.Once{}
	gcpAll = nil
	gcpErr = nil
}

// Load returns NVIDIA + AMD specs, preferring updated caches over embeds.
func Load() ([]Spec, error) {
	specMu.Lock()
	defer specMu.Unlock()
	loadOnce.Do(func() {
		nv, err := loadVendorSpecs(nvidiaCache, nvidiaJSON, "nvidia")
		if err != nil {
			loadErr = err
			return
		}
		amd, err := loadVendorSpecs(amdCache, amdJSON, "amd")
		if err != nil {
			loadErr = err
			return
		}
		allSpecs = append(nv, amd...)
	})
	if loadErr != nil {
		return nil, loadErr
	}
	out := make([]Spec, len(allSpecs))
	copy(out, allSpecs)
	return out, nil
}

func loadVendorSpecs(cacheName string, embed []byte, vendor string) ([]Spec, error) {
	var specs []Spec
	if raw, err := readCache(cacheName); err == nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, &specs); err != nil {
			return nil, fmt.Errorf("gpudb: decode cached %s: %w", cacheName, err)
		}
	} else {
		if err := json.Unmarshal(embed, &specs); err != nil {
			return nil, fmt.Errorf("gpudb: decode %s: %w", cacheName, err)
		}
	}
	for i := range specs {
		if specs[i].Vendor == "" {
			specs[i].Vendor = vendor
		}
	}
	return specs, nil
}

// LoadGCP returns GCP accelerator catalog (cache over embed).
func LoadGCP() ([]GCPAccelerator, error) {
	specMu.Lock()
	defer specMu.Unlock()
	gcpOnce.Do(func() {
		if raw, err := readCache(gcpCache); err == nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, &gcpAll); err != nil {
				gcpErr = fmt.Errorf("gpudb: decode cached gcp.json: %w", err)
				return
			}
			return
		}
		if err := json.Unmarshal(gcpJSON, &gcpAll); err != nil {
			gcpErr = fmt.Errorf("gpudb: decode gcp.json: %w", err)
			return
		}
	})
	if gcpErr != nil {
		return nil, gcpErr
	}
	out := make([]GCPAccelerator, len(gcpAll))
	copy(out, gcpAll)
	return out, nil
}

// SourceLabel reports whether data is from cache or the shipped embed.
func SourceLabel(kind string) string {
	name := nvidiaCache
	switch kind {
	case "gcp":
		name = gcpCache
	case "amd":
		name = amdCache
	}
	if _, err := readCache(name); err == nil {
		return "cache (~/.config/runhug/gpudb)"
	}
	return "bundled"
}

func readCache(name string) ([]byte, error) {
	dir, err := CacheDir()
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(dir, name))
}

// WriteCache writes a catalog file under CacheDir and invalidates memory.
func WriteCache(name string, raw []byte) error {
	dir, err := CacheDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return err
	}
	Invalidate()
	return nil
}

// TrimNVIDIA filters full TechPowerUp dump to CUDA≥7.5 VRAM≥8GB compact rows.
func TrimNVIDIA(full []Spec) []Spec {
	out := make([]Spec, 0, len(full))
	for _, g := range full {
		if g.MemorySize < 8 {
			continue
		}
		cuda := 0.0
		if g.CUDA != "" {
			fmt.Sscanf(g.CUDA, "%f", &cuda)
		}
		if cuda < 7.5 {
			continue
		}
		g.Vendor = "nvidia"
		out = append(out, g)
	}
	return out
}

// TrimAMD keeps VRAM≥8GB Radeon / Instinct (and rows with FP16/FP32).
func TrimAMD(full []Spec) []Spec {
	out := make([]Spec, 0, len(full))
	for _, g := range full {
		if g.MemorySize < 8 {
			continue
		}
		low := strings.ToLower(g.Name)
		keep := g.FP16 > 0 || g.FP32 > 0
		for _, p := range []string{"instinct", "radeon rx", "radeon pro", "radeon vii", "firepro"} {
			if strings.Contains(low, p) {
				keep = true
				break
			}
		}
		if !keep {
			continue
		}
		g.Vendor = "amd"
		out = append(out, g)
	}
	return out
}
