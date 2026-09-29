package gcp

import (
	"fmt"
	"strings"
)

// Approximate Spot $/hr for Phase 1 machine shapes in us-central1.
// Not a Google quote — list/Spot prices move; labeled as estimates in the CLI.
const (
	SpotHourlyT4 = 0.14 // n1-standard-4 + 1× T4 Spot (approx)
	SpotHourlyL4 = 0.35 // g2-standard-4 (1× L4) Spot (approx)
)

// SpotCost is a rough GCP Spot VM cost projection for Phase 1.
type SpotCost struct {
	GPU           string
	MachineType   string
	HourlyUSD     float64
	IdleSeconds   int
	KeepUp        bool
	ColdStartMinM int // minutes
	ColdStartMaxM int
	IdleHoldUSD   float64 // cost of one idle linger then stop
	HourUSD       float64 // same as HourlyUSD (alias for clarity)
	Day8hUSD      float64 // 8h kept up
	Assumptions   []string
}

// EstimateSpotCost builds an approximate Spot bill model.
// weightGB informs cold-start range (docker pull + GGUF download + load).
func EstimateSpotCost(target GPUTarget, idleSeconds int, keepUp bool, weightGB float64) SpotCost {
	hourly := SpotHourlyL4
	if target.Name == GPUTypeT4 {
		hourly = SpotHourlyT4
	}
	if idleSeconds < 0 {
		idleSeconds = 600
	}
	if keepUp {
		idleSeconds = 0
	}
	coldMin, coldMax := spotColdStartMinutes(weightGB)
	idleHold := 0.0
	if idleSeconds > 0 {
		idleHold = hourly * float64(idleSeconds) / 3600
	}
	assumptions := []string{
		"Spot VM $/hr while RUNNING (GPU + machine); TERMINATED ≈ $0 compute (disk may still bill).",
		"Prices are approximate us-central1 Spot list rates — not a Google quote; stock/preemption vary.",
		fmt.Sprintf("Cold start ≈ docker install/pull + GGUF (~%.1f GB) + load → ~%d–%d min first boot.", weightGB, coldMin, coldMax),
	}
	if keepUp {
		assumptions = append(assumptions, "keep-up: no stop-on-idle — bills until runhug gcp stop / delete.")
	} else {
		assumptions = append(assumptions,
			fmt.Sprintf("After last tunnel client, stop-on-idle ~%ds ≈ %s then GCE Stop (disk retained).", idleSeconds, formatUSD(idleHold)),
		)
	}
	return SpotCost{
		GPU:           target.Name,
		MachineType:   target.MachineType,
		HourlyUSD:     hourly,
		HourUSD:       hourly,
		IdleSeconds:   idleSeconds,
		KeepUp:        keepUp,
		ColdStartMinM: coldMin,
		ColdStartMaxM: coldMax,
		IdleHoldUSD:   idleHold,
		Day8hUSD:      hourly * 8,
		Assumptions:   assumptions,
	}
}

func spotColdStartMinutes(weightGB float64) (int, int) {
	// Empirically ~5–15 min for ~7GB GGUF on first Spot boot (docker + pull + HF).
	switch {
	case weightGB <= 0:
		return 5, 12
	case weightGB < 4:
		return 4, 10
	case weightGB < 12:
		return 5, 15
	case weightGB < 25:
		return 8, 20
	default:
		return 12, 30
	}
}

// CompactLine is a one-line plan hint.
func (c SpotCost) CompactLine() string {
	idle := "keep-up (no auto-stop)"
	if !c.KeepUp && c.IdleSeconds > 0 {
		idle = fmt.Sprintf("idle-stop %ds ≈ %s", c.IdleSeconds, formatUSD(c.IdleHoldUSD))
	}
	return fmt.Sprintf("~%s/hr Spot %s · cold start ~%d–%d min · %s",
		formatUSD(c.HourlyUSD), c.GPU, c.ColdStartMinM, c.ColdStartMaxM, idle)
}

// FormatBlock returns a multi-line approximate cost block.
// When verbose is false, omits the assumptions list (pass --verbose for those).
func (c SpotCost) FormatBlock(verbose bool) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Cost estimate · Spot %s (%s)\n", c.GPU, c.MachineType))
	b.WriteString(fmt.Sprintf("  $/hr while up     %s\n", formatUSD(c.HourlyUSD)))
	b.WriteString(fmt.Sprintf("  est. cold start  ~%d–%d min (first boot)\n", c.ColdStartMinM, c.ColdStartMaxM))
	if c.KeepUp {
		b.WriteString("  stop-on-idle     OFF (keep-up)\n")
		b.WriteString(fmt.Sprintf("  if left 8h       ≈ %s\n", formatUSD(c.Day8hUSD)))
	} else {
		b.WriteString(fmt.Sprintf("  idle then stop   ~%ds ≈ %s (disk kept)\n", c.IdleSeconds, formatUSD(c.IdleHoldUSD)))
		b.WriteString(fmt.Sprintf("  if left 8h       ≈ %s (keep-up only)\n", formatUSD(c.Day8hUSD)))
	}
	if verbose && len(c.Assumptions) > 0 {
		b.WriteString("  assumptions\n")
		for _, a := range c.Assumptions {
			b.WriteString("    · " + a + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatUSD(v float64) string {
	if v < 0.01 && v > 0 {
		return fmt.Sprintf("$%.4f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}
