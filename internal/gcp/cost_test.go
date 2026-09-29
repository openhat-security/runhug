package gcp

import (
	"strings"
	"testing"
)

func TestEstimateSpotCostT4(t *testing.T) {
	tgt, err := PickTarget(GPUTypeT4)
	if err != nil {
		t.Fatal(err)
	}
	c := EstimateSpotCost(tgt, 600, false, 7)
	if c.HourlyUSD != SpotHourlyT4 {
		t.Fatalf("hourly=%v", c.HourlyUSD)
	}
	if c.KeepUp || c.IdleSeconds != 600 {
		t.Fatalf("idle keepUp=%v sec=%d", c.KeepUp, c.IdleSeconds)
	}
	block := c.FormatBlock(false)
	for _, want := range []string{"Spot T4", "$/hr", "cold start", "600s"} {
		if !strings.Contains(block, want) {
			t.Fatalf("block missing %q:\n%s", want, block)
		}
	}
	if strings.Contains(block, "assumptions") {
		t.Fatalf("default FormatBlock should omit assumptions:\n%s", block)
	}
	if !strings.Contains(c.FormatBlock(true), "assumptions") {
		t.Fatal("verbose should include assumptions")
	}
}

func TestEstimateSpotCostKeepUp(t *testing.T) {
	tgt, _ := PickTarget(GPUTypeL4)
	c := EstimateSpotCost(tgt, 600, true, 20)
	if !c.KeepUp || c.IdleSeconds != 0 {
		t.Fatalf("keepUp=%v idle=%d", c.KeepUp, c.IdleSeconds)
	}
	if !strings.Contains(c.FormatBlock(false), "keep-up") {
		t.Fatal(c.FormatBlock(false))
	}
	if !strings.Contains(c.CompactLine(), "keep-up") {
		t.Fatal(c.CompactLine())
	}
}
