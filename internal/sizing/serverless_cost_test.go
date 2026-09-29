package sizing

import "testing"

func TestColdStartRangeByWeight(t *testing.T) {
	c := EstimateServerlessCost(0.44, 18, 1, 5, true)
	if c.ColdStartSecMin < 100 || c.ColdStartSecMax < c.ColdStartSecMin {
		t.Fatalf("cold start range odd for ~18GB: %d–%d", c.ColdStartSecMin, c.ColdStartSecMax)
	}
	if c.WarmRequestUSDMin <= 0 || c.ColdRequestUSDMin <= c.WarmRequestUSDMin {
		t.Fatalf("cold should cost more than warm: warm=%g–%g cold=%g–%g",
			c.WarmRequestUSDMin, c.WarmRequestUSDMax, c.ColdRequestUSDMin, c.ColdRequestUSDMax)
	}
	block := c.FormatBlock("AMPERE_24", false)
	for _, want := range []string{"Cost estimate", "AMPERE_24", "cold start", "all-warm", "10% cold", "$/hr"} {
		if !contains(block, want) {
			t.Fatalf("missing %q in\n%s", want, block)
		}
	}
	if contains(block, "assumptions") {
		t.Fatalf("default FormatBlock should omit assumptions:\n%s", block)
	}
	verbose := c.FormatBlock("AMPERE_24", true)
	if !contains(verbose, "assumptions") {
		t.Fatalf("verbose FormatBlock should include assumptions:\n%s", verbose)
	}
}

func TestDailyScenarioOrdering(t *testing.T) {
	c := EstimateServerlessCost(1.0, 20, 1, 5, false)
	warm := c.DailyScenarioUSD(100, 0)
	mixed := c.DailyScenarioUSD(100, 0.1)
	cold := c.DailyScenarioUSD(100, 1)
	if !(warm < mixed && mixed < cold) {
		t.Fatalf("expected warm < mixed < cold: %g %g %g", warm, mixed, cold)
	}
}

func TestCompactLine(t *testing.T) {
	c := EstimateServerlessCost(0.5, 8, 1, 5, false)
	s := c.CompactLine()
	if !contains(s, "/hr") || !contains(s, "cold") {
		t.Fatalf("%s", s)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})())
}
