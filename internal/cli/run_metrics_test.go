package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/adamsiwiec1/runhug/internal/store"
)

func TestParseHostMetrics(t *testing.T) {
	blob := strings.Join([]string{
		"RH_GPU",
		"0, Tesla T4, 7, 12, 6649, 15360, 42, 28.50, 70.00",
		"RH_MEM",
		"total_kb=15032320 avail_kb=10485760 free_kb=2936012 buffers_kb=40960 cached_kb=8912896 swap_total_kb=0 swap_free_kb=0",
		"RH_LOAD",
		"0.15 0.20 0.18 1/123 999",
		"RH_NPROC",
		"4",
		"RH_DISK",
		"total_kb=40960000 used_kb=4915200 avail_kb=36044800 pct=12% mount=/",
		"RH_UP",
		"8040",
		"RH_DOCKER",
		"runhug-llama|1.20%|2.1GiB / 14GiB|14.80%",
	}, "\n")
	var m hostMetrics
	parseHostMetrics(&m, blob)
	if len(m.GPUs) != 1 || m.GPUs[0].Name != "Tesla T4" || m.GPUs[0].Util != 7 {
		t.Fatalf("gpu %+v", m.GPUs)
	}
	if m.GPUs[0].MemUsed != 6649 || m.GPUs[0].MemTot != 15360 {
		t.Fatalf("vram %+v", m.GPUs[0])
	}
	if m.Nproc != 4 || m.Load1 != 0.15 {
		t.Fatalf("cpu nproc=%d load=%v", m.Nproc, m.Load1)
	}
	if m.MemTotalKB == 0 || memUsedPct(m) <= 0 {
		t.Fatalf("mem %+v pct=%v", m, memUsedPct(m))
	}
	if m.DiskMount != "/" || m.UptimeSec != 8040 {
		t.Fatalf("disk/up %+v", m)
	}
	if len(m.Docker) != 1 || m.Docker[0].Name != "runhug-llama" {
		t.Fatalf("docker %+v", m.Docker)
	}
}

func TestPrintMetricsFullContainsBars(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := hostMetrics{
		Source:     "gcp runhug-x",
		Fetched:    time.Date(2026, 10, 3, 6, 0, 0, 0, time.UTC),
		GPUs:       []gpuSnap{{Index: 0, Name: "Tesla T4", Util: 7, MemUsed: 6649, MemTot: 15360, Temp: 42, PowerW: 28, PowerMax: 70}},
		MemTotalKB: 14 * 1024 * 1024, MemAvailKB: 10 * 1024 * 1024, MemFreeKB: 3 * 1024 * 1024,
		Nproc: 4, Load1: 0.15, Load5: 0.2, Load15: 0.18,
		DiskTotalKB: 40 * 1024 * 1024, DiskUsedKB: 5 * 1024 * 1024, DiskPct: "12%", DiskMount: "/",
		UptimeSec: 8040,
	}
	var buf bytes.Buffer
	printMetricsFull(&buf, m, 80)
	out := buf.String()
	for _, want := range []string{"Tesla T4", "GPU", "RAM", "CPU", "4 cores", "disk", "up"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Tesla T4") {
			lead := len(line) - len(strings.TrimLeft(line, " "))
			if lead > 4 {
				t.Fatalf("staggered GPU line (%d spaces): %q", lead, line)
			}
		}
	}
	var mini bytes.Buffer
	printMetricsMini(&mini, m)
	if !strings.Contains(mini.String(), "gpu") || !strings.Contains(mini.String(), "vram") {
		t.Fatalf("mini %s", mini.String())
	}
}

func TestHandleMetricsSlashOffOnHelp(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var out bytes.Buffer
	s := &runSession{
		Target: EndpointTarget{Source: "local"},
		Model:  store.Model{HFRepo: "m", Backend: store.BackendLocal, Runtime: "ollama"},
		Out:    &out,
		Err:    &out,
	}
	handled, exit, err := handleMetricsSlash(s, []string{"help"})
	if err != nil || !handled || exit {
		t.Fatalf("%v %v %v", handled, exit, err)
	}
	if !strings.Contains(out.String(), "/metrics full") || !strings.Contains(out.String(), "runhug metrics") {
		t.Fatalf("%s", out.String())
	}
	out.Reset()
	handled, exit, err = handleMetricsSlash(s, []string{"off"})
	if err != nil || !handled || exit || s.MetricsOn {
		t.Fatalf("off: %v %v %v on=%v", handled, exit, err, s.MetricsOn)
	}
	s.stopMetricsPump()
}

func TestPrintRunStatusStripIncludesMini(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	s := &runSession{
		Target:    EndpointTarget{BaseURL: "http://127.0.0.1:8080/v1", Source: "gcp x"},
		Model:     store.Model{HFRepo: "org/m", Backend: store.BackendGCP, PodID: "runhug-org-m"},
		MetricsOn: true,
	}
	s.setMetricsCache(hostMetrics{
		GPUs: []gpuSnap{{Name: "Tesla T4", Util: 0, MemUsed: 100, MemTot: 15360}},
	})
	printRunStatusStrip(&buf, s)
	got := buf.String()
	if !strings.Contains(got, "metrics") || !strings.Contains(got, "Tesla T4") {
		t.Fatalf("%s", got)
	}
}
