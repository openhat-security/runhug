package gpudb

import "testing"

func TestDiffNewNames(t *testing.T) {
	prev := nameSet([]string{"A", "B"})
	got := diffNewNames(prev, []string{"B", "C", "A", "C", "D"})
	want := []string{"C", "D"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if n := diffNewNames(prev, []string{"A", "B"}); len(n) != 0 {
		t.Fatalf("expected no new, got %v", n)
	}
}
