package gcp

import (
	"strings"
	"testing"
)

func TestEstimateSpotCostT4(t *testing.T) {
	SeedSpotQuotesForTest([]SpotGPUQuote{{Key: "t4", PerGPUUSD: 0.209}})
	defer SeedSpotQuotesForTest(nil)
	tgt, err := PickTarget(GPUTypeT4)
	if err != nil {
		t.Fatal(err)
	}
	c := EstimateSpotCost(tgt, 600, false, 7)
	if c.HourlyUSD < 0.20 || c.HourlyUSD > 0.21 {
		t.Fatalf("hourly=%v", c.HourlyUSD)
	}
	if c.KeepUp || c.IdleSeconds != 600 {
		t.Fatalf("idle keepUp=%v sec=%d", c.KeepUp, c.IdleSeconds)
	}
	block := c.FormatBlock(false)
	for _, want := range []string{"Spot T4", "$/hr", "24/7 day", "24/7 month", "600s"} {
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
	SeedSpotQuotesForTest([]SpotGPUQuote{{Key: "l4", PerGPUUSD: 0.3192}})
	defer SeedSpotQuotesForTest(nil)
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

func TestSpotHourlyAndKeepUp(t *testing.T) {
	SeedSpotQuotesForTest([]SpotGPUQuote{
		{Key: "l4", PerGPUUSD: 0.3192},
		{Key: "h100-80", PerGPUUSD: 5.57},
	})
	defer SeedSpotQuotesForTest(nil)
	if SpotHourlyUSD(GPUTarget{Name: GPUTypeL4, MachineType: MachineTypeL4}) < 0.31 {
		t.Fatal("L4 SKU")
	}
	if SpotHourlyUSD(GPUTarget{Name: "H100", MachineType: "a3-megagpu-8g"}) != 0 {
		t.Fatal("mega unpublished Spot SKU")
	}
	if SpotHourlyUSD(GPUTarget{Name: "B300", MachineType: "a4x-max"}) != 0 {
		t.Fatal("B300 has no catalog rate")
	}
	day, month := KeepUpDayUSD(0.35), KeepUpMonthUSD(0.35)
	if day < 8.39 || day > 8.41 || month < 251 || month > 253 {
		t.Fatalf("day=%v month=%v", day, month)
	}
	s := FormatKeepUp(0.35)
	for _, want := range []string{"$0.35/hr", "$8.40/day", "/mo if 24/7"} {
		if !strings.Contains(s, want) {
			t.Fatalf("%q missing %q", s, want)
		}
	}
	chg := FormatCostChange("L4", "A100", 0.35, 1.57)
	for _, want := range []string{"now", "after", "delta", "+$1.22/hr", "L4", "A100"} {
		if !strings.Contains(chg, want) {
			t.Fatalf("change missing %q:\n%s", want, chg)
		}
	}
}
