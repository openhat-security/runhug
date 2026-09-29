package cli

import (
	"testing"

	"github.com/adamsiwiec1/runhug/internal/gpudb"
)

func TestSortGPURowsBest(t *testing.T) {
	rows := []gpuRow{
		{Name: "slow", FP16: 10, MemoryGB: 24},
		{Name: "fast", FP16: 90, MemoryGB: 24},
		{Name: "mid", FP16: 40, MemoryGB: 48},
	}
	sortGPURows(rows, "best")
	if rows[0].Name != "fast" {
		t.Fatalf("got %s", rows[0].Name)
	}
}

func TestSortGPURowsCheapest(t *testing.T) {
	rows := []gpuRow{
		{Name: "a", PricePerHr: 1.5},
		{Name: "b", PricePerHr: 0.4},
		{Name: "c"}, // no price — sinks
	}
	sortGPURows(rows, "cheapest")
	if rows[0].Name != "b" {
		t.Fatalf("got %s", rows[0].Name)
	}
	if rows[2].Name != "c" {
		t.Fatalf("unpriced should sink, got %s", rows[2].Name)
	}
}

func TestMatchCloudAlias(t *testing.T) {
	specs, err := gpudb.Load()
	if err != nil {
		t.Fatal(err)
	}
	s, ok := matchCloudName(specs, "RTX 4090", "ADA_24")
	if !ok || !containsFoldName(s.Name, "4090") {
		t.Fatalf("ADA_24 -> %+v ok=%v", s, ok)
	}
}

func containsFoldName(s, sub string) bool {
	return len(s) > 0 && (s == sub || len(sub) == 0 ||
		len(s) >= len(sub) && (stringContainsCI(s, sub)))
}

func stringContainsCI(s, sub string) bool {
	return len(gpudb.Search([]gpudb.Spec{{Name: s}}, sub)) == 1
}
