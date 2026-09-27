package gcp

import "testing"

func TestPickTarget(t *testing.T) {
	l4, err := PickTarget("")
	if err != nil || l4.Name != GPUTypeL4 {
		t.Fatalf("%v %#v", err, l4)
	}
	t4, err := PickTarget("t4")
	if err != nil || t4.Name != GPUTypeT4 {
		t.Fatalf("%v %#v", err, t4)
	}
	if _, err := PickTarget("H100"); err == nil {
		t.Fatal("expected error")
	}
}

func TestZoneFromRegion(t *testing.T) {
	r, z := ZoneFromRegion("", "")
	if r != DefaultRegion || z != DefaultZone {
		t.Fatalf("%s %s", r, z)
	}
	r, z = ZoneFromRegion("europe-west4", "")
	if r != "europe-west4" || z != "europe-west4-a" {
		t.Fatalf("%s %s", r, z)
	}
}
