package hostgpu_test

import (
	"runtime"
	"testing"

	"github.com/adamsiwiec1/runhug/internal/hostgpu"
)

func TestDetectAppleHasName(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("Apple Silicon only")
	}
	devs, err := hostgpu.Detect()
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 1 {
		t.Fatalf("got %d devices", len(devs))
	}
	d := devs[0]
	if d.Vendor != "apple" {
		t.Fatalf("vendor=%s", d.Vendor)
	}
	if d.Name == "" || d.Name == "Apple Silicon" {
		// brand_string should resolve on real Macs
		t.Logf("chip name=%q (may be generic in CI)", d.Name)
	}
	if d.FP16 <= 0 && d.Name != "Apple Silicon" {
		t.Fatalf("expected FP16 estimate for %q", d.Name)
	}
}
