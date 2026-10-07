package cli

import (
	"io"
	"strings"
	"testing"

	"github.com/adamsiwiec1/runhug/internal/gcp"
)

func TestNextContextWindow(t *testing.T) {
	if got := nextContextWindow(8192, 42000); got != 65536 {
		t.Fatalf("got %d", got)
	}
	if got := nextContextWindow(8192, 100); got != 16384 {
		t.Fatalf("small used → %d", got)
	}
}

func TestFormatContextMeter(t *testing.T) {
	s := &runSession{Ctx: tokenUsage{Prompt: 2000, Completion: 500, Total: 2500}, CtxLimit: 8192}
	got := formatContextMeter(s)
	if !strings.Contains(got, "ctx") || !strings.Contains(got, "30%") || !strings.Contains(got, "2.5k") {
		t.Fatalf("%s", got)
	}
	if contextPct(2500, 8192) != 30 {
		t.Fatalf("pct %d", contextPct(2500, 8192))
	}
}

func TestReadSSEChatUsageChunk(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"Hi"}}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}}`,
		`data: [DONE]`,
		"",
	}, "\n")
	text, u, err := readSSEChat(strings.NewReader(body), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if text != "Hi" || u.Total != 120 || u.Prompt != 100 {
		t.Fatalf("text=%q usage=%+v", text, u)
	}
}

func TestNextLargerGPU(t *testing.T) {
	cands := []gcp.GPUTarget{
		{Name: "T4", MachineType: "n1-standard-4"},
		{Name: "L4", MachineType: "g2-standard-8"},
		{Name: "A100", MachineType: "a2-highgpu-1g"},
	}
	got, err := nextLargerGPU("L4", cands)
	if err != nil || got.Name != "A100" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestHourlyForGCPTarget(t *testing.T) {
	gcp.SeedSpotQuotesForTest([]gcp.SpotGPUQuote{
		{Key: "l4", PerGPUUSD: 0.3192},
		{Key: "a100-40", PerGPUUSD: 1.7603},
		{Key: "h100-80", PerGPUUSD: 5.57},
	})
	defer gcp.SeedSpotQuotesForTest(nil)
	h := hourlyForGCPTarget(gcp.GPUTarget{Name: "L4", MachineType: gcp.MachineTypeL4})
	if h < 0.31 || h > 0.32 {
		t.Fatalf("L4 %v", h)
	}
	a := hourlyForGCPTarget(gcp.GPUTarget{Name: "A100 SXM4 40 GB", MachineType: "a2-highgpu-1g"})
	if a < 1.7 || a > 1.8 {
		t.Fatalf("A100 %v", a)
	}
	if hourlyForGCPTarget(gcp.GPUTarget{Name: "B300", MachineType: "a4x-max"}) != 0 {
		t.Fatal("B300 must not inherit L4 $0.35")
	}
	mega := hourlyForGCPTarget(gcp.GPUTarget{Name: "H100 SXM5 80 GB", MachineType: "a3-megagpu-8g"})
	if mega != 0 {
		t.Fatalf("mega has no Spot SKU, got %v", mega)
	}
}

func TestPrintGPUResizeTable(t *testing.T) {
	gcp.SeedSpotQuotesForTest([]gcp.SpotGPUQuote{
		{Key: "l4", PerGPUUSD: 0.3192},
		{Key: "h100-80", PerGPUUSD: 5.57},
	})
	defer gcp.SeedSpotQuotesForTest(nil)
	var b strings.Builder
	printGPUResizeTable(&b, []gcp.GPUTarget{
		{Name: "L4", MachineType: gcp.MachineTypeL4},
		{Name: "H100", MachineType: "a3-highgpu-1g"},
	})
	got := b.String()
	for _, want := range []string{"GPU", "MACHINE", "$/HR", "/DAY", "/MO", "L4", "H100", "$0.3192", "$5.57"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
}
