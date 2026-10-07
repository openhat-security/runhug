package gcp

import (
	"fmt"
	"strings"
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
	Day24hUSD     float64 // 24h kept up
	Month24x7USD  float64 // 30 days × 24h
	Assumptions   []string
}

// EstimateSpotCost builds an approximate Spot bill model.
// weightGB informs cold-start range (docker pull + GGUF download + load).
func EstimateSpotCost(target GPUTarget, idleSeconds int, keepUp bool, weightGB float64) SpotCost {
	hourly := SpotHourlyUSD(target)
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
		"Spot GPU $/hr from Cloud Billing list SKUs (us-central1 / Americas); vCPU/RAM extra. TERMINATED ≈ $0 compute (disk may still bill).",
		"Prices are Google list Spot/Preemptible GPU SKUs — they can change up to daily; not a contract quote.",
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
		Day24hUSD:     hourly * 24,
		Month24x7USD:  hourly * 24 * 30,
		Assumptions:   assumptions,
	}
}

// SpotHourlyUSD is the Cloud Billing Spot GPU SKU × GPU count (0 if unpublished).
func SpotHourlyUSD(target GPUTarget) float64 {
	ensureQuoteCache()
	return SpotQuoteHourly(target)
}

func scaleSpotHourly(machineType string, perGPU float64) float64 {
	if perGPU <= 0 {
		return 0
	}
	n := GPUCountFromMachine(machineType)
	if n > 1 {
		return perGPU * float64(n)
	}
	return perGPU
}

func KeepUpDayUSD(hourly float64) float64 {
	if hourly <= 0 {
		return 0
	}
	return hourly * 24
}

func KeepUpMonthUSD(hourly float64) float64 {
	if hourly <= 0 {
		return 0
	}
	return hourly * 24 * 30
}

func signedUSD(v float64) string {
	if v > 0 {
		return "+" + formatUSD(v)
	}
	if v < 0 {
		return "-" + formatUSD(-v)
	}
	return formatUSD(0)
}

// FormatKeepUp is hourly + 24/7 day + 30-day month.
func FormatKeepUp(hourly float64) string {
	if hourly <= 0 {
		return "no Google Spot GPU SKU"
	}
	return fmt.Sprintf("%s/hr · %s/day · %s/mo if 24/7",
		formatUSDHour(hourly), formatUSD(KeepUpDayUSD(hourly)), formatUSD(KeepUpMonthUSD(hourly)))
}

func formatUSDHour(v float64) string {
	s := fmt.Sprintf("$%.4f", v)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if !strings.Contains(s, ".") {
		return s + ".00"
	}
	return s
}

// FormatCostChange is a confirm-block for GPU resize / keep-up.
func FormatCostChange(fromGPU, toGPU string, fromH, toH float64) string {
	fromGPU = strings.TrimSpace(fromGPU)
	toGPU = strings.TrimSpace(toGPU)
	if fromGPU == "" {
		fromGPU = "current"
	}
	if toGPU == "" {
		toGPU = "new"
	}
	var b strings.Builder
	b.WriteString("Cost if kept up 24/7 (Google Cloud Billing Spot GPU SKU; vCPU/RAM extra)\n")
	b.WriteString(fmt.Sprintf("  now    %s  %s\n", fromGPU, FormatKeepUp(fromH)))
	b.WriteString(fmt.Sprintf("  after  %s  %s\n", toGPU, FormatKeepUp(toH)))
	if fromH > 0 && toH > 0 && fromH != toH {
		d := toH - fromH
		b.WriteString(fmt.Sprintf("  delta  %s/hr · %s/day · %s/mo\n",
			signedUSD(d), signedUSD(KeepUpDayUSD(d)), signedUSD(KeepUpMonthUSD(d))))
	}
	return strings.TrimRight(b.String(), "\n")
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
	b.WriteString(fmt.Sprintf("  24/7 day          ≈ %s\n", formatUSD(c.Day24hUSD)))
	b.WriteString(fmt.Sprintf("  24/7 month (30d)  ≈ %s\n", formatUSD(c.Month24x7USD)))
	b.WriteString(fmt.Sprintf("  est. cold start  ~%d–%d min (first boot)\n", c.ColdStartMinM, c.ColdStartMaxM))
	if c.KeepUp {
		b.WriteString("  stop-on-idle     OFF (keep-up)\n")
	} else {
		b.WriteString(fmt.Sprintf("  idle then stop   ~%ds ≈ %s (disk kept)\n", c.IdleSeconds, formatUSD(c.IdleHoldUSD)))
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
