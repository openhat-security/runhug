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

func TestDefaultLlamaImage(t *testing.T) {
	got := DefaultLlamaImage("otw-portal-dev", "us-central1")
	want := "us-central1-docker.pkg.dev/otw-portal-dev/runhug/llama-server:cuda"
	if got != want {
		t.Fatalf("%s", got)
	}
	if DefaultLlamaImage("", "us-central1") != "" {
		t.Fatal("empty project")
	}
	if DefaultLlamaImage("p", "") != "us-central1-docker.pkg.dev/p/runhug/llama-server:cuda" {
		t.Fatalf("%s", DefaultLlamaImage("p", ""))
	}
}

func TestGPUCountFromMachine(t *testing.T) {
	if GPUCountFromMachine("g2-standard-4") != 1 {
		t.Fatal("L4")
	}
	if GPUCountFromMachine("a3-highgpu-1g") != 1 || GPUCountFromMachine("a3-megagpu-8g") != 8 {
		t.Fatal("H100")
	}
	if PlausibleGPUMachine("a4x") || PlausibleGPUMachine("a4x-max") || !PlausibleGPUMachine("a4-highgpu-8g") {
		t.Fatal("a4x")
	}
}
