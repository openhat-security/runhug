package cli

import (
	"strings"
	"testing"
)

func TestPrintTable(t *testing.T) {
	var b strings.Builder
	printTable(&b, []tableCol{
		{Title: "#", Min: 1, Max: 3, Right: true},
		{Title: "GPU", Min: 3, Max: 12},
		{Title: "$/HR", Min: 4, Max: 8, Right: true},
	}, [][]tableCell{
		{styledCell("1", cyan), cell("L4"), cell("$0.3192")},
		{styledCell("2", cyan), cell("H100"), cell("$5.57")},
	})
	got := b.String()
	for _, want := range []string{"#", "GPU", "$/HR", "L4", "H100", "$0.3192", "$5.57"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want header+2 rows, got %d\n%s", len(lines), got)
	}
}
