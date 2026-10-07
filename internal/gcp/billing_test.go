package gcp

import (
	"testing"
	"time"
)

func TestParseSpotGPUQuotes(t *testing.T) {
	skus := []billingSKU{
		gpuSKU("T4-SPOT", "Nvidia Tesla T4 GPU attached to Spot Preemptible VMs running in Americas", "Preemptible", "americas", "0", 209000000),
		gpuSKU("T4-OD", "Nvidia Tesla T4 GPU running in Americas", "OnDemand", "americas", "0", 350000000),
		gpuSKU("L4-SPOT", "Nvidia L4 GPU attached to Spot Preemptible VMs running in Americas", "Preemptible", "americas", "0", 319200000),
		gpuSKU("A100-SPOT", "Nvidia Tesla A100 GPU attached to Spot Preemptible VMs running in Americas", "Preemptible", "americas", "1", 760300000),
		gpuSKU("B200-SPOT", "Spot Preemptible A4 Nvidia B200 (1 gpu slice) running in Americas", "OnDemand", "americas", "4", 954200000),
		gpuSKU("B200-OD", "A4 Nvidia B200 (1 gpu slice) running in Americas", "OnDemand", "americas", "16", 110000000),
		gpuSKU("H100-MEGA-DWS", "Nvidia H100 Mega 80GB GPU attached to DWS Defined Duration VMs running in Americas", "OnDemand", "americas", "4", 0),
	}
	q := parseSpotGPUQuotes(skus, time.Unix(0, 0).UTC())
	got := map[string]float64{}
	for _, row := range q {
		got[row.Key] = row.PerGPUUSD
	}
	if got["t4"] < 0.20 || got["t4"] > 0.21 {
		t.Fatalf("t4=%v", got["t4"])
	}
	if got["l4"] < 0.31 || got["l4"] > 0.32 {
		t.Fatalf("l4=%v", got["l4"])
	}
	if got["a100-40"] < 1.7 || got["a100-40"] > 1.8 {
		t.Fatalf("a100=%v", got["a100-40"])
	}
	if got["b200"] < 4.9 || got["b200"] > 5.0 {
		t.Fatalf("b200=%v want spot ~4.95 not on-demand 16", got["b200"])
	}
	if _, ok := got["h100-mega"]; ok {
		t.Fatal("DWS mega must not count as Spot")
	}
	if _, ok := got["b300"]; ok {
		t.Fatal("no B300 SKU")
	}
}

func gpuSKU(id, desc, usage, region, units string, nanos int64) billingSKU {
	var s billingSKU
	s.SKUId = id
	s.Description = desc
	s.ServiceRegions = []string{region}
	s.Category.ResourceGroup = "GPU"
	s.Category.UsageType = usage
	s.PricingInfo = []struct {
		PricingExpression struct {
			UsageUnitDescription string `json:"usageUnitDescription"`
			TieredRates          []struct {
				UnitPrice struct {
					Units string `json:"units"`
					Nanos int64  `json:"nanos"`
				} `json:"unitPrice"`
			} `json:"tieredRates"`
		} `json:"pricingExpression"`
	}{{}}
	s.PricingInfo[0].PricingExpression.TieredRates = []struct {
		UnitPrice struct {
			Units string `json:"units"`
			Nanos int64  `json:"nanos"`
		} `json:"unitPrice"`
	}{{UnitPrice: struct {
		Units string `json:"units"`
		Nanos int64  `json:"nanos"`
	}{Units: units, Nanos: nanos}}}
	return s
}

func TestSpotQuoteHourlyUsesCache(t *testing.T) {
	SeedSpotQuotesForTest([]SpotGPUQuote{
		{Key: "l4", PerGPUUSD: 0.3192},
		{Key: "h100-80", PerGPUUSD: 5.57},
		{Key: "b200", PerGPUUSD: 4.9542},
	})
	defer SeedSpotQuotesForTest(nil)
	l4 := SpotQuoteHourly(GPUTarget{Name: "L4", MachineType: MachineTypeL4})
	if l4 < 0.31 || l4 > 0.32 {
		t.Fatalf("L4 %v", l4)
	}
	h := SpotQuoteHourly(GPUTarget{Name: "H100", MachineType: "a3-highgpu-1g"})
	if h < 5.5 || h > 5.6 {
		t.Fatalf("H100 1g %v", h)
	}
	eight := SpotQuoteHourly(GPUTarget{Name: "H100", MachineType: "a3-megagpu-8g"})
	if eight != 0 {
		t.Fatalf("mega has no Spot SKU in this cache, got %v", eight)
	}
	b := SpotQuoteHourly(GPUTarget{Name: "B200", MachineType: "a4-highgpu-8g"})
	want := 4.9542 * 8
	if b < want-0.01 || b > want+0.01 {
		t.Fatalf("8× B200 SKU %v want %v", b, want)
	}
	if SpotQuoteHourly(GPUTarget{Name: "B300", MachineType: "a4x-max"}) != 0 {
		t.Fatal("B300")
	}
}
