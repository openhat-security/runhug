package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/adamsiwiec1/runhug/internal/store"
	"github.com/adamsiwiec1/runhug/internal/version"
	"golang.org/x/term"
)

func fullChatPrompt() string {
	return cyan("▌") + " " + green("you> ")
}

func (s *runSession) drawFullChrome() {
	if s == nil {
		return
	}
	w := s.tty()
	width, height := termWH()
	s.fullW, s.fullH = width, height
	if !s.altScreen {
		fmt.Fprint(w, "\033[?1049h")
		s.altScreen = true
	}
	s.ChatFull = true
	s.startMetricsPump()
	fmt.Fprint(w, "\033[?25h\033[?1007l\033[r\033[H\033[2J")
	header := writeFullHeader(w, s, width)
	scrollTop := header + 1
	scrollBot := height - 1
	if scrollBot <= scrollTop {
		scrollBot = scrollTop + 1
	}
	s.fullPromptRow = scrollTop
	fmt.Fprintf(w, "\033[%d;%dr", scrollTop, scrollBot)
	writeFullFooter(w, s, width, height)
	fmt.Fprintf(w, "\033[%d;1H", scrollTop)
	s.replayTranscript(w)
	go func() {
		m, _ := fetchHostMetrics(s)
		s.setMetricsCache(m)
		if s.ChatFull {
			s.refreshFullChrome()
		}
	}()
}

func writeFullHeader(w io.Writer, s *runSession, width int) int {
	row := 0
	fmt.Fprintln(w)
	row++
	if s != nil {
		s.fullBannerRow = row + 1
	}
	for _, line := range fullBannerLines(s, width) {
		fmt.Fprintln(w, line)
		row++
	}
	fmt.Fprintln(w)
	row++

	cwd, model, inst := fullHeaderBits(s, width)
	if s != nil {
		s.fullCwdRow = row + 1
		s.fullModelRow = row + 2
		s.fullInstRow = row + 3
	}
	fmt.Fprintln(w, cwd)
	fmt.Fprintln(w, model)
	fmt.Fprintln(w, inst)
	row += 3
	fmt.Fprintln(w)
	row++
	return row
}

func fullBannerLines(s *runSession, width int) []string {
	keys := []string{cyan("/help"), cyan("/mini"), dim("Ctrl-D exit")}
	keyW := 0
	for _, k := range keys {
		if n := visibleLen(k); n > keyW {
			keyW = n
		}
	}
	banner := strings.Split(bannerASCII, "\n")
	artW := 0
	for _, line := range banner {
		if l := visibleLen(line); l > artW {
			artW = l
		}
	}
	if width < artW+keyW+8 {
		width = artW + keyW + 8
	}
	artStart := (width - artW) / 2
	keysAt := width - keyW
	if keysAt < artStart+artW+1 {
		keysAt = artStart + artW + 1
		width = keysAt + keyW
	}
	metricsW := artStart - 1
	if metricsW < 8 {
		metricsW = 8
	}
	metrics := fullMetricsCol(s, metricsW)
	n := len(banner)
	if n < len(keys) {
		n = len(keys)
	}
	if n < len(metrics) {
		n = len(metrics)
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		metric := ""
		if i < len(metrics) {
			metric = metrics[i]
		}
		art := padVisible("", artW)
		if i < len(banner) {
			padded := padVisible(banner[i], artW)
			if i < 2 {
				art = dim(padded)
			} else {
				art = boldCyan(padded)
			}
		}
		line := fitVisible(metric, artStart) + art
		if i < len(keys) {
			line = padVisible(line, keysAt) + padVisible(keys[i], keyW)
		}
		out[i] = line
	}
	return out
}

func fullMetricsCol(s *runSession, width int) []string {
	if s == nil {
		return []string{dim("metrics"), dim("…")}
	}
	barW := minInt(10, maxInt(4, width-6))
	ctxLines := []string{
		dim("ctx") + "  " + formatTokenCount(s.ctxUsed()) + "/" + formatTokenCount(s.ctxWindow()),
		contextBar(s.ctxUsed(), s.ctxWindow(), barW) + "  " + fmt.Sprintf("%d%%", contextPct(s.ctxUsed(), s.ctxWindow())),
	}
	m := s.cachedMetrics()
	if m == nil {
		return ctxLines
	}
	if m.Err != "" && len(m.GPUs) == 0 && m.MemTotalKB == 0 {
		return append(ctxLines, yellow(truncateRunes(m.Err, width)))
	}
	var lines []string
	lines = append(lines, ctxLines...)
	if len(m.GPUs) > 0 {
		g := m.GPUs[0]
		lines = append(lines, dim("gpu")+"  "+cyan(truncateRunes(g.Name, maxInt(6, width-10)))+"  "+heatPct(g.Util))
		if g.MemTot > 0 {
			lines = append(lines, dim("vram")+"  "+fmt.Sprintf("%s/%s", fmtMiB(g.MemUsed), fmtMiB(g.MemTot)))
		}
		if g.Temp > 0 {
			lines = append(lines, dim("temp")+"  "+tempLabel(g.Temp))
		}
	}
	if m.MemTotalKB > 0 {
		lines = append(lines, dim("ram")+"  "+heatPct(memUsedPct(*m)))
	}
	if m.Nproc > 0 {
		lines = append(lines, dim("load")+"  "+fmt.Sprintf("%.2f/%d", m.Load1, m.Nproc))
	}
	if len(lines) == 0 {
		return ctxLines
	}
	for i, l := range lines {
		if visibleLen(l) > width {
			lines[i] = truncateRunes(l, width)
		}
	}
	return lines
}

func padVisible(s string, n int) string {
	w := visibleLen(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

func fitVisible(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if visibleLen(s) > n {
		var b strings.Builder
		vis := 0
		inEsc := false
		for _, r := range s {
			if inEsc {
				b.WriteRune(r)
				if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
					inEsc = false
				}
				continue
			}
			if r == 0x1b {
				inEsc = true
				b.WriteRune(r)
				continue
			}
			if vis >= n {
				break
			}
			b.WriteRune(r)
			vis++
		}
		s = b.String()
	}
	return padVisible(s, n)
}

func fullHeaderBits(s *runSession, width int) (cwdLine, modelLine, instLine string) {
	cwd := ""
	if s != nil {
		cwd = shortHome(s.Cwd)
	}
	if cwd == "" || cwd == "." {
		cwd = "."
	}
	cwdLine = " " + dim(truncateRunes(cwd, maxInt(8, width-4)))

	backend, model, extra := s.fullIdentity()
	inst := s.fullInstance()
	modelLine = " " + dim(padRight("model", 8)) + " " + bold(truncateRunes(model, maxInt(8, width-12)))
	instBits := colorBackend(backend, backend)
	if inst != "" {
		instBits += "  " + cyan(truncateRunes(inst, maxInt(8, width-24)))
	}
	if extra != "" {
		instBits += "  " + dim(extra)
	}
	instLine = " " + dim(padRight("instance", 8)) + " " + instBits
	return cwdLine, modelLine, instLine
}

func writeFullFooter(w io.Writer, s *runSession, width, row int) {
	fmt.Fprintf(w, "\033[%d;1H\033[2K", row)
	left := " " + dim(version.Name+" "+version.Version)
	right := formatContextMeter(s)
	gap := width - visibleLen(left) - visibleLen(right) - 1
	if gap < 1 {
		fmt.Fprint(w, right)
		return
	}
	fmt.Fprint(w, left+strings.Repeat(" ", gap)+right)
}

func (s *runSession) refreshFullChrome() {
	if s == nil || !s.ChatFull || s.fullH < 8 {
		return
	}
	w := s.tty()
	fmt.Fprint(w, "\033[s")
	if s.fullBannerRow > 0 {
		for i, line := range fullBannerLines(s, s.fullW) {
			fmt.Fprintf(w, "\033[%d;1H\033[2K%s", s.fullBannerRow+i, line)
		}
	}
	cwd, model, inst := fullHeaderBits(s, s.fullW)
	if s.fullCwdRow > 0 {
		fmt.Fprintf(w, "\033[%d;1H\033[2K%s", s.fullCwdRow, cwd)
	}
	if s.fullModelRow > 0 {
		fmt.Fprintf(w, "\033[%d;1H\033[2K%s", s.fullModelRow, model)
	}
	if s.fullInstRow > 0 {
		fmt.Fprintf(w, "\033[%d;1H\033[2K%s", s.fullInstRow, inst)
	}
	writeFullFooter(w, s, s.fullW, s.fullH)
	fmt.Fprint(w, "\033[u")
}

func (s *runSession) replayTranscript(w io.Writer) {
	if s == nil || s.Transcript == nil {
		return
	}
	if w == nil {
		w = s.tty()
	}
	width, _ := termWH()
	for _, ln := range s.Transcript.Snapshot() {
		if strings.TrimSpace(ln) == "" {
			fmt.Fprintln(w)
			continue
		}
		for _, row := range wrapANSI(ln, width) {
			fmt.Fprintln(w, row)
		}
	}
}

func (s *runSession) fullIdentity() (backend, model, extra string) {
	backend = "local"
	if s == nil {
		return backend, "model", ""
	}
	model = strings.TrimSpace(s.Target.Model)
	if model == "" {
		model = s.Model.HFRepo
	}
	if model == "" {
		model = "model"
	}
	if s.Model.HFRepo != "" {
		backend = s.Model.Kind()
	} else if strings.HasPrefix(s.Target.Source, "gcp") {
		backend = store.BackendGCP
	} else if strings.HasPrefix(s.Target.Source, "runpod") {
		backend = store.BackendRunpod
	} else if strings.HasPrefix(s.Target.Source, "local") {
		backend = store.BackendLocal
	}
	switch backend {
	case store.BackendGCP:
		extra = "tunnel"
		if p := localPortFromBaseURL(s.Target.BaseURL); p > 0 {
			extra = fmt.Sprintf(":%d", p)
		}
	case store.BackendLocal:
		if inst := s.fullInstance(); inst != "" && inst == s.Model.Runtime {
			extra = ""
		} else {
			extra = s.Model.Runtime
		}
	}
	return backend, model, extra
}

func (s *runSession) fullInstance() string {
	if s == nil {
		return ""
	}
	switch s.Model.Kind() {
	case store.BackendGCP:
		if s.Model.PodID != "" {
			return s.Model.PodID
		}
		return s.Model.EndpointID
	case store.BackendLocal:
		return s.Model.Runtime
	default:
		return s.Model.EndpointID
	}
}

func termWH() (w, h int) {
	w, h = 80, 24
	if stdoutIsTTY() {
		if tw, th, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
			if tw >= 40 {
				w = tw
			}
			if th >= 12 {
				h = th
			}
		}
	}
	return w, h
}

func shortHome(p string) string {
	p = filepath.Clean(p)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if p == home {
			return "~"
		}
		if strings.HasPrefix(p, home+"/") {
			return "~" + p[len(home):]
		}
	}
	return p
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
