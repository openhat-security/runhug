// Package hostgpu probes the machine for discrete GPUs (NVIDIA) or Apple Silicon.
package hostgpu

import (
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/adamsiwiec1/runhug/internal/recommend"
)

// Device is one detected accelerator on this host.
type Device struct {
	Name     string  // marketing name (e.g. "GeForce RTX 4090", "Apple M3 Max")
	MemoryGB float64 // VRAM or unified memory (Mac)
	Vendor   string  // nvidia | apple | unknown
	Source   string  // nvidia-smi | apple-silicon
	FP16     float64 // estimated vector FP16 TFLOPS when known (0 = unknown)
	GPUCores int     // Apple GPU core count when known
	Note     string  // e.g. "FP16≈2×FP32 (public estimates)"
}

// Detect returns local GPUs. Empty slice means none found (not an error).
func Detect() ([]Device, error) {
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		return []Device{detectApple()}, nil
	}
	var out []Device
	if nv, err := nvidiaSMI(); err == nil {
		out = append(out, nv...)
	}
	if amd, err := rocmSMI(); err == nil {
		out = append(out, amd...)
	}
	return out, nil
}

func detectApple() Device {
	ram := recommend.RAMGB()
	chip := appleChipName()
	cores := appleGPUCores()
	fp16, note := appleFP16(chip, cores)
	name := chip
	if name == "" {
		name = "Apple Silicon"
	}
	return Device{
		Name:     name,
		MemoryGB: ram,
		Vendor:   "apple",
		Source:   "apple-silicon",
		FP16:     fp16,
		GPUCores: cores,
		Note:     note,
	}
}

func appleChipName() string {
	out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output()
	if err == nil {
		s := strings.TrimSpace(string(out))
		if strings.HasPrefix(s, "Apple ") {
			return s
		}
	}
	// Fallback: system_profiler Chip line
	out, err = exec.Command("system_profiler", "SPHardwareDataType").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Chip:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Chip:"))
		}
	}
	return ""
}

var coresRe = regexp.MustCompile(`(?i)Total Number of Cores:\s*(\d+)`)

func appleGPUCores() int {
	out, err := exec.Command("system_profiler", "SPDisplaysDataType").Output()
	if err != nil {
		return 0
	}
	// Prefer the first "Total Number of Cores" under the Apple GPU block.
	m := coresRe.FindStringSubmatch(string(out))
	if len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// appleFP16 returns estimated GPU FP16 TFLOPS for ranking against TechPowerUp vector FP16.
// Sources: Flopper.io FP32 (and M4 FP16), JD Hodges / public charts using FP16≈2×FP32.
// Not Neural Engine TOPS. Values are approximate peaks for ranking only.
func appleFP16(chip string, cores int) (float64, string) {
	key := normalizeAppleChip(chip)
	spec, ok := appleBaseFP16[key]
	if !ok {
		return 0, "FP16 unknown for this Apple chip"
	}
	fp16 := spec.fp16
	if cores > 0 && spec.maxCores > 0 && cores < spec.maxCores {
		fp16 = spec.fp16 * float64(cores) / float64(spec.maxCores)
	}
	return fp16, "FP16 est. ≈2× public FP32 (Flopper/charts); ranking only"
}

type appleSpec struct {
	fp16     float64
	maxCores int
}

// Peak configs; lower core bins are scaled by detected GPU core count.
var appleBaseFP16 = map[string]appleSpec{
	"apple m1":       {5.2, 8},
	"apple m1 pro":   {10.4, 16},
	"apple m1 max":   {21.2, 32},
	"apple m1 ultra": {42.4, 64},
	"apple m2":       {7.2, 10},
	"apple m2 pro":   {13.6, 19},
	"apple m2 max":   {27.2, 38},
	"apple m2 ultra": {54.4, 76},
	"apple m3":       {7.1, 10},
	"apple m3 pro":   {14.2, 18},
	"apple m3 max":   {28.4, 40}, // 40-core peak; 30-core MacBook Pro scales down
	"apple m3 ultra": {56.8, 80},
	"apple m4":       {8.5, 10}, // Flopper FP16
	"apple m4 pro":   {18.4, 20},
	"apple m4 max":   {36.8, 40},
	"apple m4 ultra": {73.6, 80},
	"apple m5":       {9.0, 10},
	"apple m5 pro":   {20.0, 20},
	"apple m5 max":   {33.2, 40},
}

func normalizeAppleChip(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "  ", " ")
	return s
}

func nvidiaSMI() ([]Device, error) {
	path, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return nil, nil
	}
	out, err := exec.Command(path, "--query-gpu=name,memory.total", "--format=csv,noheader,nounits").CombinedOutput()
	if err != nil {
		return nil, nil
	}
	var devices []Device
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ",", 2)
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		memStr := strings.TrimSpace(parts[1])
		mb, _ := strconv.ParseFloat(memStr, 64)
		gb := mb / 1024
		if gb <= 0 {
			continue
		}
		devices = append(devices, Device{
			Name:     name,
			MemoryGB: gb,
			Vendor:   "nvidia",
			Source:   "nvidia-smi",
		})
	}
	return devices, nil
}

// rocmSMI parses `rocm-smi --showproductname --showmeminfo vram` when present.
func rocmSMI() ([]Device, error) {
	path, err := exec.LookPath("rocm-smi")
	if err != nil {
		return nil, nil
	}
	out, err := exec.Command(path, "--showproductname", "--showmeminfo", "vram", "--csv").CombinedOutput()
	if err != nil {
		// Fallback: plain text product name only
		out2, err2 := exec.Command(path, "--showproductname").CombinedOutput()
		if err2 != nil {
			return nil, nil
		}
		name := parseROCmProductName(string(out2))
		if name == "" {
			return nil, nil
		}
		return []Device{{Name: name, Vendor: "amd", Source: "rocm-smi"}}, nil
	}
	return parseROCmCSV(string(out)), nil
}

func parseROCmProductName(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		low := strings.ToLower(line)
		if strings.Contains(low, "card series") || strings.Contains(low, "card model") || strings.Contains(low, "device name") {
			if i := strings.Index(line, ":"); i >= 0 {
				return strings.TrimSpace(line[i+1:])
			}
		}
	}
	return ""
}

func parseROCmCSV(s string) []Device {
	// Best-effort: look for lines with a card name and a memory number.
	var devices []Device
	name := ""
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(strings.ToLower(line), "device") {
			continue
		}
		parts := strings.Split(line, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			low := strings.ToLower(p)
			if strings.Contains(low, "radeon") || strings.Contains(low, "instinct") || strings.Contains(low, "amd") {
				name = p
			}
		}
		if name == "" {
			continue
		}
		memGB := 0.0
		for _, p := range parts {
			p = strings.TrimSpace(strings.TrimSuffix(strings.ToLower(p), " mb"))
			p = strings.TrimSuffix(p, "miB")
			if n, err := strconv.ParseFloat(strings.TrimSpace(p), 64); err == nil && n > 256 {
				memGB = n / 1024
			}
		}
		devices = append(devices, Device{
			Name:     name,
			MemoryGB: memGB,
			Vendor:   "amd",
			Source:   "rocm-smi",
		})
		name = ""
	}
	return devices
}
