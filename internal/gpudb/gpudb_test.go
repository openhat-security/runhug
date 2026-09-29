package gpudb_test

import (
	"strings"
	"testing"

	"github.com/adamsiwiec1/runhug/internal/gpudb"
)

func TestLoad(t *testing.T) {
	specs, err := gpudb.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) < 50 {
		t.Fatalf("expected trimmed index, got %d", len(specs))
	}
}

func TestByName(t *testing.T) {
	cases := []struct {
		in   string
		want string // substring of matched name
	}{
		{"GeForce RTX 4090", "4090"},
		{"RTX 4090", "4090"},
		{"H100 SXM5 80 GB", "H100"},
		{"L4", "L4"},
		{"Tesla T4", "T4"},
		{"L40S", "L40S"},
		{"MI300X", "MI300X"},
		{"Radeon RX 7900 XTX", "7900"},
	}
	for _, tc := range cases {
		s, ok := gpudb.ByName(tc.in)
		if !ok {
			t.Fatalf("%q: no match", tc.in)
		}
		if !strings.Contains(strings.ToUpper(s.Name), strings.ToUpper(tc.want)) {
			t.Fatalf("%q -> %q, want contains %q", tc.in, s.Name, tc.want)
		}
		if tc.in == "L4" && strings.Contains(strings.ToUpper(s.Name), "L40") {
			t.Fatalf("L4 matched %q", s.Name)
		}
	}
}

func TestLoadIncludesAMD(t *testing.T) {
	specs, err := gpudb.Load()
	if err != nil {
		t.Fatal(err)
	}
	amd, nv := 0, 0
	for _, s := range specs {
		switch s.Vendor {
		case "amd":
			amd++
		case "nvidia", "":
			nv++
		}
	}
	if amd < 20 {
		t.Fatalf("expected AMD rows, got %d (nvidia-ish %d, total %d)", amd, nv, len(specs))
	}
}

func TestSearch(t *testing.T) {
	specs, err := gpudb.Load()
	if err != nil {
		t.Fatal(err)
	}
	got := gpudb.Search(specs, "4090")
	if len(got) == 0 {
		t.Fatal("expected 4090 hits")
	}
}
