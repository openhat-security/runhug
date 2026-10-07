package cli

import (
	"strings"
	"testing"

	"github.com/adamsiwiec1/runhug/internal/store"
)

func TestSumCost(t *testing.T) {
	rows := []costRow{
		{Backend: store.BackendGCP, Up: true, Hourly: 0.3192, Now: 0.3192},
		{Backend: store.BackendGCP, Up: false, Hourly: 1.7603, Now: 0},
		{Backend: store.BackendRunpod, Up: true, Hourly: 0.44, Now: 0.44},
		{Backend: store.BackendLocal, Up: false, Hourly: 0, Now: 0},
	}
	tot := sumCost(rows)
	if tot.UpN != 2 || tot.DownN != 1 {
		t.Fatalf("up=%d down=%d", tot.UpN, tot.DownN)
	}
	if tot.NowHourly < 0.75 || tot.NowHourly > 0.76 {
		t.Fatalf("now %v", tot.NowHourly)
	}
	if tot.AllHourly < 2.51 || tot.AllHourly > 2.53 {
		t.Fatalf("all %v", tot.AllHourly)
	}
	by := sumCostByBackend(rows)
	if by[store.BackendGCP].NowHourly < 0.31 || by[store.BackendRunpod].NowHourly < 0.43 {
		t.Fatalf("%+v", by)
	}
	if by[store.BackendLocal].AllHourly != 0 {
		t.Fatal("local billed")
	}
}

func TestPrintCostTable(t *testing.T) {
	var b strings.Builder
	printCostTable(&b, []costRow{
		{Model: "org/m", Backend: "gcp", GPU: "L4", State: "up", Hourly: 0.3192, Now: 0.3192},
		{Model: "org/n", Backend: "gcp", GPU: "A100", State: "down", Hourly: 1.76, Now: 0},
	})
	got := b.String()
	for _, want := range []string{"MODEL", "STATE", "$/HR", "NOW", "/DAY", "/MO", "org/m", "up", "down"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
}

func TestCatalogHourlyGCPUsesQuote(t *testing.T) {
	h := catalogHourly(store.Model{Backend: store.BackendLocal})
	if h != 0 {
		t.Fatalf("%v", h)
	}
	h = catalogHourly(store.Model{Backend: store.BackendRunpod, HourlyUSD: 1.1})
	if h != 1.1 {
		t.Fatalf("%v", h)
	}
}
