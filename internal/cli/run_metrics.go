package cli

import (
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/gcp"
	"github.com/adamsiwiec1/runhug/internal/jobs"
	"github.com/adamsiwiec1/runhug/internal/store"
	"golang.org/x/term"
)

type gpuSnap struct {
	Index    int
	Name     string
	Util     float64
	MemUtil  float64
	MemUsed  float64 // MiB
	MemTot   float64 // MiB
	Temp     float64
	PowerW   float64
	PowerMax float64
}

type dockerSnap struct {
	Name, CPU, Mem, MemPct string
}

type hostMetrics struct {
	Source  string
	Fetched time.Time
	Err     string

	GPUs []gpuSnap

	MemTotalKB, MemAvailKB, MemFreeKB uint64
	BuffersKB, CachedKB               uint64
	SwapTotalKB, SwapFreeKB           uint64

	Load1, Load5, Load15 float64
	Nproc                int

	DiskTotalKB, DiskUsedKB, DiskAvailKB uint64
	DiskPct, DiskMount                   string

	UptimeSec uint64
	Docker    []dockerSnap
}

func handleMetricsSlash(s *runSession, args []string) (handled, exit bool, err error) {
	if s == nil {
		return true, false, fmt.Errorf("no session")
	}
	arg := ""
	if len(args) > 0 {
		arg = strings.ToLower(strings.TrimSpace(args[0]))
	}
	switch arg {
	case "", "show", "snap", "snapshot":
		return true, false, printMetricsSnapshot(s)
	case "on", "mini", "min", "strip":
		return true, false, enableMetricsMini(s)
	case "off", "hide":
		s.stopMetricsPump()
		s.MetricsOn = false
		fmt.Fprintln(s.out(), dim("metrics strip off  ·  /metrics on to pin  ·  /metrics full for live"))
		return true, false, nil
	case "full", "live", "fs", "fullscreen":
		err := runLiveMetrics(s)
		return true, false, err
	case "help", "-h", "--help":
		printMetricsSlashHelp(s.out())
		return true, false, nil
	default:
		return true, false, fmt.Errorf("usage: /metrics [on|off|mini|full]")
	}
}

func printMetricsSlashHelp(w io.Writer) {
	fmt.Fprintln(w, bold("Metrics"))
	fmt.Fprintf(w, "  %s  %s\n", cyan(padRight("/metrics", 18)), dim("detailed snapshot"))
	fmt.Fprintf(w, "  %s  %s\n", cyan(padRight("/metrics on", 18)), dim("pin compact strip above the prompt"))
	fmt.Fprintf(w, "  %s  %s\n", cyan(padRight("/metrics off", 18)), dim("hide the strip"))
	fmt.Fprintf(w, "  %s  %s\n", cyan(padRight("/metrics mini", 18)), dim("same as on (compact)"))
	fmt.Fprintf(w, "  %s  %s\n", cyan(padRight("/metrics full", 18)), dim("live fullscreen in this terminal (q to leave)"))
	fmt.Fprintf(w, "  %s  %s\n", cyan(padRight("runhug metrics", 18)), dim("same live view — run in a second pane"))
	fmt.Fprintln(w)
}

func printMetricsSnapshot(s *runSession) error {
	m, err := fetchHostMetrics(s)
	s.setMetricsCache(m)
	w := s.out()
	printMetricsFull(w, m, metricsTermWidth())
	fmt.Fprintln(w, dim("pin strip: /metrics on   ·   live here: /metrics full   ·   other pane: runhug metrics"))
	fmt.Fprintln(w)
	return err
}

func enableMetricsMini(s *runSession) error {
	err := pinMetricsMini(s)
	if m := s.cachedMetrics(); m != nil {
		printMetricsMini(s.out(), *m)
	}
	fmt.Fprintln(s.out(), dim("strip on  ·  /metrics off  ·  /metrics full  ·  runhug metrics"))
	return err
}

func pinMetricsMini(s *runSession) error {
	s.MetricsOn = true
	m, err := fetchHostMetrics(s)
	s.setMetricsCache(m)
	s.startMetricsPump()
	return err
}

func (s *runSession) setMetricsCache(m hostMetrics) {
	if s == nil {
		return
	}
	cp := m
	s.metricsMu.Lock()
	s.metricsCache = &cp
	s.metricsMu.Unlock()
}

func (s *runSession) cachedMetrics() *hostMetrics {
	if s == nil {
		return nil
	}
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	return s.metricsCache
}

func (s *runSession) startMetricsPump() {
	if s == nil || s.metricsStop != nil {
		return
	}
	stop := make(chan struct{})
	s.metricsStop = stop
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				m, _ := fetchHostMetrics(s)
				s.setMetricsCache(m)
				if s.ChatFull {
					s.refreshFullChrome()
				}
			}
		}
	}()
}

func (s *runSession) stopMetricsPump() {
	if s == nil || s.metricsStop == nil {
		return
	}
	close(s.metricsStop)
	s.metricsStop = nil
}

func fetchHostMetrics(s *runSession) (hostMetrics, error) {
	m := hostMetrics{Fetched: time.Now()}
	if s == nil {
		m.Err = "no session"
		return m, fmt.Errorf("no session")
	}
	switch {
	case s.Model.Kind() == store.BackendGCP && s.Model.PodID != "":
		m.Source = "gcp " + s.Model.PodID
		blob, err := gcpGuest(s, gcp.GuestMetricsCommand())
		if err != nil {
			m.Err = err.Error()
			return m, err
		}
		parseHostMetrics(&m, blob)
		return m, nil
	case s.Model.Kind() == store.BackendLocal || strings.HasPrefix(s.Target.Source, "local"):
		m.Source = "local"
		collectLocalMetrics(&m)
		return m, nil
	case s.Model.Kind() == store.BackendRunpod || strings.HasPrefix(s.Target.Source, "runpod"):
		m.Source = "runpod"
		env := config.Load()
		if env.RunpodAPIKey != "" && s.Model.EndpointID != "" {
			h, err := jobs.Health(env.RunpodAPIKey, s.Model.EndpointID)
			if err != nil {
				m.Err = err.Error()
			} else if h != nil && h.Workers != nil {
				m.Err = fmt.Sprintf("workers ready=%s running=%s initializing=%s  (GPU stats: Runpod dashboard)",
					itoaPtr(h.Workers.Ready), itoaPtr(h.Workers.Running), itoaPtr(h.Workers.Initializing))
			}
		} else {
			m.Err = "Runpod serverless — guest GPU stats are not available; use the Runpod dashboard"
		}
		return m, nil
	default:
		m.Source = "local"
		collectLocalMetrics(&m)
		return m, nil
	}
}

func collectLocalMetrics(m *hostMetrics) {
	if m.Source == "" {
		m.Source = "local"
	}
	m.Nproc = runtime.NumCPU()
	if out, err := exec.Command("nvidia-smi",
		"--query-gpu=index,name,utilization.gpu,utilization.memory,memory.used,memory.total,temperature.gpu,power.draw,power.limit",
		"--format=csv,noheader,nounits").CombinedOutput(); err == nil {
		parseGPULines(m, string(out))
	}
	if runtime.GOOS == "linux" {
		if raw, err := os.ReadFile("/proc/meminfo"); err == nil {
			parseMemKV(m, meminfoToKV(string(raw)))
		}
		if raw, err := os.ReadFile("/proc/loadavg"); err == nil {
			parseLoadLine(m, string(raw))
		}
		if raw, err := os.ReadFile("/proc/uptime"); err == nil {
			parseUptime(m, string(raw))
		}
	} else if runtime.GOOS == "darwin" {
		if out, err := exec.Command("sysctl", "-n", "hw.memsize").Output(); err == nil {
			if n, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64); err == nil {
				m.MemTotalKB = n / 1024
			}
		}
		if out, err := exec.Command("sysctl", "-n", "vm.loadavg").Output(); err == nil {
			parseLoadLine(m, strings.Trim(strings.TrimSpace(string(out)), "{}"))
		}
	}
	if out, err := exec.Command("df", "-kP", "/").Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) >= 2 {
			parseDiskKV(m, dfToKV(lines[1]))
		}
	}
}

func meminfoToKV(raw string) string {
	get := func(key string) uint64 {
		for _, line := range strings.Split(raw, "\n") {
			if strings.HasPrefix(line, key) {
				f := strings.Fields(line)
				if len(f) >= 2 {
					n, _ := strconv.ParseUint(f[1], 10, 64)
					return n
				}
			}
		}
		return 0
	}
	return fmt.Sprintf("total_kb=%d avail_kb=%d free_kb=%d buffers_kb=%d cached_kb=%d swap_total_kb=%d swap_free_kb=%d",
		get("MemTotal:"), get("MemAvailable:"), get("MemFree:"), get("Buffers:"), get("Cached:"), get("SwapTotal:"), get("SwapFree:"))
}

func dfToKV(line string) string {
	f := strings.Fields(line)
	if len(f) < 6 {
		return ""
	}
	return fmt.Sprintf("total_kb=%s used_kb=%s avail_kb=%s pct=%s mount=%s", f[1], f[2], f[3], f[4], f[5])
}

func parseHostMetrics(m *hostMetrics, blob string) {
	sec := ""
	var buf []string
	flush := func() {
		body := strings.TrimSpace(strings.Join(buf, "\n"))
		switch sec {
		case "RH_GPU":
			parseGPULines(m, body)
		case "RH_MEM":
			parseMemKV(m, body)
		case "RH_LOAD":
			parseLoadLine(m, body)
		case "RH_NPROC":
			if n, err := strconv.Atoi(strings.TrimSpace(body)); err == nil {
				m.Nproc = n
			}
		case "RH_DISK":
			parseDiskKV(m, body)
		case "RH_UP":
			parseUptime(m, body)
		case "RH_DOCKER":
			parseDockerLines(m, body)
		}
		buf = buf[:0]
	}
	for _, line := range strings.Split(blob, "\n") {
		t := strings.TrimSpace(line)
		switch t {
		case "RH_GPU", "RH_MEM", "RH_LOAD", "RH_NPROC", "RH_DISK", "RH_UP", "RH_DOCKER":
			flush()
			sec = t
			continue
		}
		buf = append(buf, line)
	}
	flush()
}

func parseGPULines(m *hostMetrics, body string) {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, "none") || strings.HasPrefix(strings.ToLower(line), "gpu:") {
			continue
		}
		parts := splitCSV(line)
		if len(parts) < 6 {
			continue
		}
		g := gpuSnap{Name: strings.TrimSpace(parts[1])}
		g.Index = atoiDef(parts[0], 0)
		g.Util = atofDef(parts[2], 0)
		g.MemUtil = atofDef(parts[3], 0)
		g.MemUsed = atofDef(parts[4], 0)
		g.MemTot = atofDef(parts[5], 0)
		if len(parts) > 6 {
			g.Temp = atofDef(parts[6], 0)
		}
		if len(parts) > 7 {
			g.PowerW = atofDef(parts[7], 0)
		}
		if len(parts) > 8 {
			g.PowerMax = atofDef(parts[8], 0)
		}
		m.GPUs = append(m.GPUs, g)
	}
}

func splitCSV(s string) []string {
	raw := strings.Split(s, ",")
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

func parseMemKV(m *hostMetrics, body string) {
	kv := parseKVMap(body)
	m.MemTotalKB = kv["total_kb"]
	m.MemAvailKB = kv["avail_kb"]
	m.MemFreeKB = kv["free_kb"]
	m.BuffersKB = kv["buffers_kb"]
	m.CachedKB = kv["cached_kb"]
	m.SwapTotalKB = kv["swap_total_kb"]
	m.SwapFreeKB = kv["swap_free_kb"]
}

func parseDiskKV(m *hostMetrics, body string) {
	kv := parseKVMap(body)
	m.DiskTotalKB = kv["total_kb"]
	m.DiskUsedKB = kv["used_kb"]
	m.DiskAvailKB = kv["avail_kb"]
	fields := strings.Fields(body)
	for _, f := range fields {
		k, v, ok := strings.Cut(f, "=")
		if !ok {
			continue
		}
		switch k {
		case "pct":
			m.DiskPct = v
		case "mount":
			m.DiskMount = v
		}
	}
}

func parseKVMap(body string) map[string]uint64 {
	out := map[string]uint64{}
	for _, f := range strings.Fields(body) {
		k, v, ok := strings.Cut(f, "=")
		if !ok {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSuffix(v, "%"), 10, 64)
		if err == nil {
			out[k] = n
		}
	}
	return out
}

func parseLoadLine(m *hostMetrics, body string) {
	f := strings.Fields(body)
	if len(f) >= 1 {
		m.Load1 = atofDef(f[0], 0)
	}
	if len(f) >= 2 {
		m.Load5 = atofDef(f[1], 0)
	}
	if len(f) >= 3 {
		m.Load15 = atofDef(f[2], 0)
	}
}

func parseUptime(m *hostMetrics, body string) {
	f := strings.Fields(body)
	if len(f) == 0 {
		return
	}
	sec := strings.Split(f[0], ".")[0]
	n, _ := strconv.ParseUint(sec, 10, 64)
	m.UptimeSec = n
}

func parseDockerLines(m *hostMetrics, body string) {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		p := strings.Split(line, "|")
		if len(p) < 4 {
			continue
		}
		m.Docker = append(m.Docker, dockerSnap{Name: p[0], CPU: p[1], Mem: p[2], MemPct: p[3]})
	}
}

func atoiDef(s string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return n
}

func atofDef(s string, def float64) float64 {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "%")
	s = strings.TrimSuffix(s, "W")
	s = strings.TrimSuffix(s, "C")
	if s == "" || strings.EqualFold(s, "[n/a]") || strings.EqualFold(s, "n/a") {
		return def
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return n
}

func printMetricsMini(w io.Writer, m hostMetrics) {
	var bits []string
	if m.Err != "" && len(m.GPUs) == 0 && m.MemTotalKB == 0 {
		fmt.Fprintf(w, "%s  %s\n", dim("metrics"), yellow(truncateRunes(m.Err, 72)))
		return
	}
	if len(m.GPUs) > 0 {
		g := m.GPUs[0]
		bits = append(bits, fmt.Sprintf("%s %s %s %s",
			cyan("gpu"), truncateRunes(g.Name, 14),
			meterBar(g.Util, 10), heatPct(g.Util)))
		if g.MemTot > 0 {
			vram := 100 * g.MemUsed / g.MemTot
			bits = append(bits, fmt.Sprintf("%s %s %s",
				dim("vram"), meterBar(vram, 8), fmt.Sprintf("%s/%s", fmtMiB(g.MemUsed), fmtMiB(g.MemTot))))
		}
	}
	if m.MemTotalKB > 0 {
		used := memUsedPct(m)
		bits = append(bits, fmt.Sprintf("%s %s %s", dim("ram"), meterBar(used, 8), heatPct(used)))
	}
	if m.Nproc > 0 {
		loadPct := 100 * m.Load1 / float64(m.Nproc)
		bits = append(bits, fmt.Sprintf("%s %.2f/%d", dim("load"), m.Load1, m.Nproc))
		_ = loadPct
	}
	if len(bits) == 0 {
		fmt.Fprintf(w, "%s  %s\n", dim("metrics"), dim("no gpu/ram sample yet"))
		return
	}
	fmt.Fprintf(w, "%s  %s\n", dim("metrics"), strings.Join(bits, "  "+dim("·")+"  "))
}

func printMetricsFull(w io.Writer, m hostMetrics, width int) {
	if width < 48 {
		width = 48
	}
	inner := width - 4
	if inner < 40 {
		inner = 40
	}
	barW := inner - 22
	if barW > 28 {
		barW = 28
	}
	if barW < 12 {
		barW = 12
	}

	fmt.Fprintln(w, dim("  "+strings.Repeat("━", minInt(inner, 64))))
	fmt.Fprintf(w, "  %s  %s\n", boldCyan("metrics"), dim(m.Fetched.Format("15:04:05")))
	if m.Source != "" {
		fmt.Fprintf(w, "  %s\n", dim(truncateRunes(m.Source, inner)))
	}
	fmt.Fprintln(w, dim("  "+strings.Repeat("─", minInt(inner, 56))))
	fmt.Fprintln(w)

	if m.Err != "" && len(m.GPUs) == 0 && m.MemTotalKB == 0 {
		fmt.Fprintf(w, "  %s\n\n", yellow(m.Err))
		printMetricsFooter(w)
		return
	}
	if len(m.GPUs) == 0 {
		metricsRow(w, "GPU", dim("nvidia-smi unavailable"))
		fmt.Fprintln(w)
	}
	for _, g := range m.GPUs {
		metricsRow(w, "GPU", fmt.Sprintf("%s  %s", bold(fmt.Sprintf("%d", g.Index)), cyan(g.Name)))
		metricsRow(w, "util", meterBar(g.Util, barW)+"  "+heatPct(g.Util))
		vram := 0.0
		if g.MemTot > 0 {
			vram = 100 * g.MemUsed / g.MemTot
		}
		metricsRow(w, "vram", fmt.Sprintf("%s  %s  %s / %s", meterBar(vram, barW), heatPct(vram), fmtMiB(g.MemUsed), fmtMiB(g.MemTot)))
		bits := []string{}
		if g.Temp > 0 {
			bits = append(bits, tempLabel(g.Temp))
		}
		if g.PowerW > 0 {
			pw := fmt.Sprintf("%.0f W", g.PowerW)
			if g.PowerMax > 0 {
				pw = fmt.Sprintf("%.0f / %.0f W", g.PowerW, g.PowerMax)
			}
			bits = append(bits, pw)
		}
		if len(bits) > 0 {
			metricsRow(w, "temp", strings.Join(bits, "    "))
		}
		fmt.Fprintln(w)
	}
	if m.Nproc > 0 || m.Load1 > 0 {
		loadPct := 0.0
		if m.Nproc > 0 {
			loadPct = 100 * m.Load1 / float64(m.Nproc)
		}
		metricsRow(w, "CPU", fmt.Sprintf("%s  %s", bold(fmt.Sprintf("%d cores", m.Nproc)), dim(fmt.Sprintf("%.2f  %.2f  %.2f", m.Load1, m.Load5, m.Load15))))
		metricsRow(w, "load", meterBar(loadPct, barW)+"  "+heatPct(loadPct))
		fmt.Fprintln(w)
	}
	if m.MemTotalKB > 0 {
		usedPct := memUsedPct(m)
		used := m.MemTotalKB - m.MemAvailKB
		if m.MemAvailKB == 0 && m.MemFreeKB > 0 {
			used = m.MemTotalKB - m.MemFreeKB
		}
		metricsRow(w, "RAM", fmt.Sprintf("%s  %s  %s used  ·  %s avail", meterBar(usedPct, barW), heatPct(usedPct), fmtKiB(used), fmtKiB(m.MemAvailKB)))
		if m.CachedKB+m.BuffersKB > 0 {
			metricsRow(w, "cache", fmt.Sprintf("%s  ·  %s free", fmtKiB(m.CachedKB+m.BuffersKB), fmtKiB(m.MemFreeKB)))
		}
		if m.SwapTotalKB > 0 {
			swUsed := 100 * float64(m.SwapTotalKB-m.SwapFreeKB) / float64(m.SwapTotalKB)
			metricsRow(w, "swap", fmt.Sprintf("%s  %s  %s / %s", meterBar(swUsed, barW/2), heatPct(swUsed), fmtKiB(m.SwapTotalKB-m.SwapFreeKB), fmtKiB(m.SwapTotalKB)))
		}
		fmt.Fprintln(w)
	}
	if m.DiskTotalKB > 0 {
		dp := atofDef(strings.TrimSuffix(m.DiskPct, "%"), 0)
		mnt := m.DiskMount
		if mnt == "" {
			mnt = "/"
		}
		metricsRow(w, "disk", fmt.Sprintf("%s  %s  %s / %s  %s", meterBar(dp, barW/2), heatPct(dp), fmtKiB(m.DiskUsedKB), fmtKiB(m.DiskTotalKB), dim(mnt)))
	}
	if m.UptimeSec > 0 {
		metricsRow(w, "up", dim(fmtDuration(m.UptimeSec)))
	}
	if len(m.Docker) > 0 {
		fmt.Fprintln(w)
		metricsRow(w, "ctnr", "")
		for _, d := range m.Docker {
			metricsRow(w, "", fmt.Sprintf("%s  cpu %s  mem %s (%s)", cyan(d.Name), d.CPU, d.Mem, d.MemPct))
		}
	}
	fmt.Fprintln(w)
	printMetricsFooter(w)
}

func metricsRow(w io.Writer, key, val string) {
	if key == "" {
		fmt.Fprintf(w, "          %s\n", val)
		return
	}
	fmt.Fprintf(w, "  %s  %s\n", dim(padRight(key, 6)), val)
}

func printMetricsFooter(w io.Writer) {
	fmt.Fprintln(w, dim("  q  back    m  pin strip    /full  chat    /mini  chat"))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func toCRLF(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}

func memUsedPct(m hostMetrics) float64 {
	if m.MemTotalKB == 0 {
		return 0
	}
	avail := m.MemAvailKB
	if avail == 0 {
		avail = m.MemFreeKB
	}
	used := m.MemTotalKB - avail
	return 100 * float64(used) / float64(m.MemTotalKB)
}

func meterBar(pct float64, width int) string {
	if width < 4 {
		width = 4
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	fill := int(math.Round(pct / 100 * float64(width)))
	if fill > width {
		fill = width
	}
	on := strings.Repeat("█", fill)
	off := strings.Repeat("░", width-fill)
	s := on + off
	switch {
	case pct >= 85:
		return red(s)
	case pct >= 60:
		return yellow(s)
	default:
		return green(s)
	}
}

func heatPct(pct float64) string {
	label := fmt.Sprintf("%3.0f%%", pct)
	switch {
	case pct >= 85:
		return red(label)
	case pct >= 60:
		return yellow(label)
	default:
		return green(label)
	}
}

func tempLabel(c float64) string {
	s := fmt.Sprintf("%.0f°C", c)
	switch {
	case c >= 80:
		return red(s)
	case c >= 70:
		return yellow(s)
	default:
		return green(s)
	}
}

func fmtMiB(mib float64) string {
	if mib >= 1024 {
		return fmt.Sprintf("%.1f GiB", mib/1024)
	}
	return fmt.Sprintf("%.0f MiB", mib)
}

func fmtKiB(kb uint64) string {
	if kb >= 1024*1024 {
		return fmt.Sprintf("%.1f GiB", float64(kb)/1024/1024)
	}
	if kb >= 1024 {
		return fmt.Sprintf("%.0f MiB", float64(kb)/1024)
	}
	return fmt.Sprintf("%d KiB", kb)
}

func fmtDuration(sec uint64) string {
	d := time.Duration(sec) * time.Second
	if d >= time.Hour {
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	if d >= time.Minute {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", sec)
}

func metricsTermWidth() int {
	if stdoutIsTTY() {
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w >= 40 {
			return w
		}
	}
	return 80
}

func runLiveMetrics(s *runSession) error {
	w := s.out()
	if !stdoutIsTTY() || !stdinIsTTY() {
		m, err := fetchHostMetrics(s)
		printMetricsFull(w, m, 80)
		return err
	}

	ownedAlt := s == nil || !s.altScreen
	if ownedAlt {
		fmt.Fprint(w, "\033[?1049h")
		if s != nil {
			s.altScreen = true
		}
	}
	fmt.Fprint(w, "\033[?25l")
	defer func() {
		fmt.Fprint(w, "\033[?25h")
		if ownedAlt {
			fmt.Fprint(w, "\033[?1049l")
			if s != nil {
				s.altScreen = false
				s.ChatFull = false
			}
		} else if s != nil && s.ChatFull {
			s.drawFullChrome()
		} else if s != nil {
			fmt.Fprint(w, "\033[H\033[2J")
			printRunStatusStrip(w, s)
			fmt.Fprintln(w, dim("fullscreen chat  ·  /mini to restore scrollback"))
			fmt.Fprintln(w)
		}
	}()

	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		m, ferr := fetchHostMetrics(s)
		printMetricsFull(w, m, metricsTermWidth())
		return ferr
	}
	defer func() { _ = term.Restore(fd, old) }()

	leave := make(chan byte, 1)
	go func() {
		var b [1]byte
		for {
			n, rerr := os.Stdin.Read(b[:])
			if rerr != nil || n == 0 {
				select {
				case leave <- 'q':
				default:
				}
				return
			}
			switch b[0] {
			case 'q', 'Q', 3, 4:
				leave <- 'q'
				return
			case 'm', 'M':
				leave <- 'm'
				return
			}
		}
	}()

	draw := func() {
		m, _ := fetchHostMetrics(s)
		if s != nil {
			s.setMetricsCache(m)
		}
		var b strings.Builder
		printMetricsFull(&b, m, metricsTermWidth())
		fmt.Fprintf(w, "\033[H\033[J%s", toCRLF(b.String()))
	}
	draw()
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		select {
		case k := <-leave:
			if k == 'm' && s != nil {
				_ = pinMetricsMini(s)
			}
			return nil
		case <-tick.C:
			draw()
		}
	}
}

func cmdMetrics(args []string) error {
	if showCmdHelp(args, "metrics", printMetricsHelp, printMetricsHelpFull) {
		return nil
	}
	fs := newFlagSet("metrics")
	baseURL := fs.String("base-url", "", "unused; metrics uses the registry instance")
	apiKeyEnv := fs.String("api-key-env", "", "")
	serveModel := fs.String("model", "", "registry / served model name")
	yes := fs.Bool("yes", false, "auto-pick when exactly one registry model")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	modelKey := ""
	if fs.NArg() > 0 {
		modelKey = fs.Arg(0)
	}
	skipPrompt := *yes || !canPrompt()
	pickedKey, _, err := pickStartModel(modelKey, *baseURL, *serveModel, *apiKeyEnv, skipPrompt)
	if err != nil {
		return err
	}
	sess := &runSession{Out: os.Stdout, Err: os.Stderr}
	if pickedKey != "" {
		if reg, _, err := store.Load(); err == nil {
			if m, ok := reg.Lookup(pickedKey); ok {
				sess.Model = m
			}
		}
	}
	if sess.Model.HFRepo == "" && *serveModel != "" {
		sess.Target.Model = *serveModel
		sess.Target.Source = "local"
	}
	return runLiveMetrics(sess)
}

func printMetricsHelp(w io.Writer) {
	helpUsage(w, "runhug metrics [model]")
	helpSection(w, "commands")
	helpCmd(w, "metrics [model]", "live GPU / CPU / RAM dashboard (fullscreen, q to quit)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, dim("Chat: /metrics snapshot · /metrics on strip · /metrics full overlay"))
}

func printMetricsHelpFull(w io.Writer) {
	helpUsage(w, "runhug metrics [model]")
	fmt.Fprintln(w, dim("Live host metrics. Short list:"), cyan("runhug metrics --help"))
	fmt.Fprintln(w)
	helpFullSection(w, "overview",
		helpFullEntry{
			Cmd:  "metrics [model]",
			What: "Fullscreen live GPU, VRAM, CPU load, RAM, disk, and container stats (GCP guest or this machine).",
			When: "Second terminal next to `runhug run`, or when you want a dashboard without chat.",
			More: "q / Ctrl-C leaves. In chat: /metrics on pins a compact strip; /metrics full takes over this terminal.",
		},
	)
}
