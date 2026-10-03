package sizing

import (
	"fmt"
	"math"
	"strings"
)

// Default warm active GPU time per chat-style request (seconds). Ranges keep
// estimates honest — real latency depends on tokens, concurrency, and hardware.
const (
	WarmServeSecMin = 2.0
	WarmServeSecMax = 10.0
	WarmServeSecMid = 5.0
)

// ServerlessCost is an approximate bill model for Runpod-style serverless:
// $/hr only while a worker is up (min workers 0 → idle ≈ $0).
type ServerlessCost struct {
	HourlyUSD float64
	WeightGB  float64
	GPUCount  int

	ColdStartSecMin int
	ColdStartSecMax int
	FlashbootNote   string

	WarmRequestUSDMin float64
	WarmRequestUSDMax float64
	ColdRequestUSDMin float64
	ColdRequestUSDMax float64

	IdleTimeoutSec int
	IdleHoldUSD    float64 // cost of one idleTimeout linger after scale-up

	Assumptions []string
}

// EstimateServerlessCost builds a rough cost model from weight size and pool $/hr.
// idleTimeoutSec <= 0 defaults to 5 (deploy default). gpuCount <= 0 defaults to 1.
func EstimateServerlessCost(hourlyUSD, weightGB float64, gpuCount, idleTimeoutSec int, flashboot bool) ServerlessCost {
	if gpuCount <= 0 {
		gpuCount = 1
	}
	if idleTimeoutSec <= 0 {
		idleTimeoutSec = 5
	}
	rate := hourlyUSD
	if rate < 0 {
		rate = 0
	}

	coldMin, coldMax := coldStartRange(weightGB)
	fbNote := ""
	if flashboot {
		fbNote = "FLASHBOOT may shorten repeat cold starts when a snapshot is warm (not guaranteed)."
		// Slightly optimistic upper bound only — still a range.
		if coldMax > coldMin+30 {
			coldMax = coldMin + (coldMax-coldMin)*2/3
			if coldMax < coldMin {
				coldMax = coldMin
			}
		}
	}

	warmMin := rate * WarmServeSecMin / 3600
	warmMax := rate * WarmServeSecMax / 3600
	coldMinUSD := rate * float64(coldMin+int(WarmServeSecMin)) / 3600
	coldMaxUSD := rate * float64(coldMax+int(WarmServeSecMax)) / 3600
	idleHold := rate * float64(idleTimeoutSec) / 3600

	assumptions := []string{
		"Serverless $/hr only while a worker is up (min workers 0 → idle ≈ $0).",
		fmt.Sprintf("Warm request ≈ %.0f–%.0fs active GPU time (chat-sized; not a token meter).", WarmServeSecMin, WarmServeSecMax),
		fmt.Sprintf("Cold start ≈ model pull+load from ~%.1f GB weights → %s.", weightGB, formatSecRange(coldMin, coldMax)),
		fmt.Sprintf("Cold request ≈ (cold start + warm serve) / 3600 × $%.2f/hr.", rate),
		fmt.Sprintf("After traffic, worker may linger ~%ds (idle timeout) ≈ %s before scale-to-zero.", idleTimeoutSec, formatUSDRange(idleHold, idleHold)),
		"Estimates only — not a Runpod quote. Stock, flashboot, and download speed vary.",
	}
	if fbNote != "" {
		assumptions = append(assumptions, fbNote)
	}

	return ServerlessCost{
		HourlyUSD:         rate,
		WeightGB:          weightGB,
		GPUCount:          gpuCount,
		ColdStartSecMin:   coldMin,
		ColdStartSecMax:   coldMax,
		FlashbootNote:     fbNote,
		WarmRequestUSDMin: warmMin,
		WarmRequestUSDMax: warmMax,
		ColdRequestUSDMin: coldMinUSD,
		ColdRequestUSDMax: coldMaxUSD,
		IdleTimeoutSec:    idleTimeoutSec,
		IdleHoldUSD:       idleHold,
		Assumptions:       assumptions,
	}
}

func coldStartRange(weightGB float64) (int, int) {
	switch {
	case weightGB <= 0:
		return 45, 120
	case weightGB < 4:
		return 30, 90
	case weightGB < 12:
		return 60, 180
	case weightGB < 25:
		return 120, 300 // ~2–5 min for ~20GB-class weights
	case weightGB < 50:
		return 180, 480
	default:
		return 300, 600
	}
}

// ColdStartSeconds returns an approximate serverless cold-start range in seconds.
func ColdStartSeconds(weightGB float64) (minSec, maxSec int) {
	return coldStartRange(weightGB)
}

// DailyScenarioUSD estimates a day of N requests with coldFrac in [0,1].
// Uses midpoints of cold/warm request ranges (honestly labeled elsewhere as approx).
func (c ServerlessCost) DailyScenarioUSD(requests int, coldFrac float64) float64 {
	if requests <= 0 || c.HourlyUSD <= 0 {
		return 0
	}
	if coldFrac < 0 {
		coldFrac = 0
	}
	if coldFrac > 1 {
		coldFrac = 1
	}
	warmMid := (c.WarmRequestUSDMin + c.WarmRequestUSDMax) / 2
	coldMid := (c.ColdRequestUSDMin + c.ColdRequestUSDMax) / 2
	coldN := int(math.Round(float64(requests) * coldFrac))
	if coldN > requests {
		coldN = requests
	}
	warmN := requests - coldN
	// One idle linger amortized per cold start (scale-up); warm-only days still pay ~1 linger if any traffic.
	lingers := coldN
	if lingers == 0 && warmN > 0 {
		lingers = 1
	}
	return float64(warmN)*warmMid + float64(coldN)*coldMid + float64(lingers)*c.IdleHoldUSD
}

func formatSecRange(minSec, maxSec int) string {
	if minSec == maxSec {
		return formatDuration(minSec)
	}
	return formatDuration(minSec) + "–" + formatDuration(maxSec)
}

func formatDuration(sec int) string {
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	min := sec / 60
	rem := sec % 60
	if rem == 0 {
		return fmt.Sprintf("%dm", min)
	}
	return fmt.Sprintf("%dm%ds", min, rem)
}

func formatUSDRange(lo, hi float64) string {
	if almostEqual(lo, hi) {
		return formatUSD(lo)
	}
	return formatUSD(lo) + "–" + formatUSD(hi)
}

func formatUSD(v float64) string {
	if v <= 0 {
		return "$0"
	}
	if v < 0.0001 {
		return fmt.Sprintf("$%.6f", v)
	}
	if v < 0.01 {
		return fmt.Sprintf("$%.4f", v)
	}
	if v < 1 {
		return fmt.Sprintf("$%.3f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-12
}

// CompactLine is a one-line cost hint for option lists.
func (c ServerlessCost) CompactLine() string {
	return fmt.Sprintf("~%s/hr · cold req %s · warm req %s · cold start %s",
		formatUSD(c.HourlyUSD),
		formatUSDRange(c.ColdRequestUSDMin, c.ColdRequestUSDMax),
		formatUSDRange(c.WarmRequestUSDMin, c.WarmRequestUSDMax),
		formatSecRange(c.ColdStartSecMin, c.ColdStartSecMax),
	)
}

// FormatBlock returns a multi-line approximate cost block (no trailing newline).
// When verbose is false, omits assumptions and flashboot notes (pass --verbose for those).
func (c ServerlessCost) FormatBlock(poolID string, verbose bool) string {
	var b strings.Builder
	title := "Cost estimate"
	if poolID != "" {
		title = fmt.Sprintf("Cost estimate · %s", poolID)
	}
	b.WriteString(title)
	b.WriteByte('\n')
	b.WriteString(fmt.Sprintf("  $/hr while up     %s", formatUSD(c.HourlyUSD)))
	if c.GPUCount > 1 {
		b.WriteString(fmt.Sprintf("  (×%d GPUs)", c.GPUCount))
	}
	b.WriteByte('\n')
	wlabel := "~unknown weights"
	if c.WeightGB > 0 {
		wlabel = fmt.Sprintf("~%.1f GB weights", c.WeightGB)
	}
	b.WriteString(fmt.Sprintf("  est. cold start  %s  (%s)\n", formatSecRange(c.ColdStartSecMin, c.ColdStartSecMax), wlabel))
	if verbose && c.FlashbootNote != "" {
		b.WriteString("  flashboot        may shorten repeat cold starts when snapshot is warm\n")
	}
	b.WriteString(fmt.Sprintf("  est. $/cold req  %s\n", formatUSDRange(c.ColdRequestUSDMin, c.ColdRequestUSDMax)))
	b.WriteString(fmt.Sprintf("  est. $/warm req  %s\n", formatUSDRange(c.WarmRequestUSDMin, c.WarmRequestUSDMax)))
	b.WriteString(fmt.Sprintf("  idle linger      ~%ds after last req ≈ %s\n",
		c.IdleTimeoutSec, formatUSD(c.IdleHoldUSD)))
	b.WriteString("  daily scenarios\n")
	for _, n := range []int{10, 100, 1000} {
		allWarm := c.DailyScenarioUSD(n, 0)
		mixed := c.DailyScenarioUSD(n, 0.10)
		allCold := c.DailyScenarioUSD(n, 1)
		b.WriteString(fmt.Sprintf("    %4d req/day   all-warm ≈ %s · 10%% cold ≈ %s · all-cold ≈ %s\n",
			n, formatUSD(allWarm), formatUSD(mixed), formatUSD(allCold)))
	}
	if verbose && len(c.Assumptions) > 0 {
		b.WriteString("  assumptions\n")
		for _, a := range c.Assumptions {
			b.WriteString("    · " + a + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
