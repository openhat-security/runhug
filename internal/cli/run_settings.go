package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/gcp"
	"github.com/adamsiwiec1/runhug/internal/gpudb"
	"github.com/adamsiwiec1/runhug/internal/runpod"
	"github.com/adamsiwiec1/runhug/internal/store"
)

func runSettingsMenu(s *runSession) error {
	w := s.out()
	for {
		heading(w, "Settings")
		mode := "agent"
		if s.Plan {
			mode = "plan"
		}
		printKV(w, "1 mode", mode)
		printKV(w, "2 perm", normalizeRunPerm(s.Perm))
		tools := "off"
		if s.Tools {
			tools = "on"
		}
		printKV(w, "3 tools", tools)
		printKV(w, "4 model", s.Target.Model)
		gpu := s.Model.GPUPool
		if gpu == "" {
			gpu = "(unset)"
		}
		printKV(w, "5 gpu", gpu)
		printKV(w, "6", dim("done"))
		fmt.Fprintln(w)
		line, err := readLine("Pick setting 1-6: ")
		if err != nil {
			return err
		}
		switch strings.TrimSpace(line) {
		case "1":
			if err := settingsPickMode(s); err != nil {
				return err
			}
		case "2":
			if err := settingsPickPerm(s); err != nil {
				return err
			}
		case "3":
			if err := settingsPickTools(s); err != nil {
				return err
			}
		case "4":
			if err := settingsSwitchModel(s); err != nil {
				fmt.Fprintln(s.errw(), FormatError(err.Error()))
			}
		case "5":
			if err := settingsUpsizeGPU(s); err != nil {
				fmt.Fprintln(s.errw(), FormatError(err.Error()))
			}
		case "6", "q", "done", "":
			return nil
		default:
			fmt.Fprintln(w, dim("expected 1-6"))
		}
		saveRunModeSettings(s)
		_ = saveRunPersist(s)
	}
}

func settingsPickMode(s *runSession) error {
	opts := []choiceOpt{
		{ID: "agent", Label: "Agent", Shortcut: 'a'},
		{ID: "plan", Label: "Plan", Shortcut: 'p'},
	}
	sel := 0
	if s.Plan {
		sel = 1
	}
	id, err := promptChoice(s.tty(), nil, dim("mode"), opts, sel)
	if err != nil {
		return err
	}
	s.Plan = id == "plan"
	printKV(s.out(), "mode", bold(id))
	return nil
}

func settingsPickPerm(s *runSession) error {
	opts := []choiceOpt{
		{ID: permAsk, Label: "Ask", Shortcut: 'k'},
		{ID: permAllow, Label: "Allow", Shortcut: 'a'},
		{ID: permDeny, Label: "Deny", Shortcut: 'd'},
	}
	sel := 0
	switch normalizeRunPerm(s.Perm) {
	case permAllow:
		sel = 1
	case permDeny:
		sel = 2
	}
	id, err := promptChoice(s.tty(), nil, dim("permissions"), opts, sel)
	if err != nil {
		return err
	}
	s.Perm = normalizeRunPerm(id)
	printKV(s.out(), "perm", bold(s.Perm))
	return nil
}

func settingsPickTools(s *runSession) error {
	opts := []choiceOpt{
		{ID: "on", Label: "On", Shortcut: 'y'},
		{ID: "off", Label: "Off", Shortcut: 'n'},
	}
	sel := 0
	if !s.Tools {
		sel = 1
	}
	id, err := promptChoice(s.tty(), nil, dim("tools"), opts, sel)
	if err != nil {
		return err
	}
	s.Tools = id == "on"
	printRunToolsHelp(s.out(), s)
	return nil
}

func settingsSwitchModel(s *runSession) error {
	key, serve, err := pickStartModel("", "", "", "", false)
	if err != nil {
		return err
	}
	target, cleanup, err := resolveReadyEndpoint(key, "", "", serve, false)
	if err != nil {
		return err
	}
	if s.Cleanup != nil {
		s.Cleanup()
	}
	s.Cleanup = cleanup
	s.Target = target
	s.RegistryKey = key
	if key != "" {
		if reg, _, err := store.Load(); err == nil {
			if m, ok := reg.Lookup(key); ok {
				s.Model = m
			}
		}
	}
	printKV(s.out(), "model", bold(s.Target.Model))
	printKV(s.out(), "base_url", cyan(s.Target.BaseURL))
	return nil
}

func settingsUpsizeGPU(s *runSession) error {
	switch s.Model.Kind() {
	case store.BackendLocal:
		fmt.Fprintln(s.out(), dim("local runtime uses the host GPU — `runhug gpu set` for deploy preference"))
		return nil
	case store.BackendGCP:
		return upsizeGCP(s)
	default:
		return upsizeRunpod(s)
	}
}

func handleGPUSlash(s *runSession, fields []string) error {
	arg := ""
	if len(fields) > 1 {
		arg = strings.TrimSpace(strings.Join(fields[1:], " "))
	}
	if arg == "" {
		return settingsUpsizeGPU(s)
	}
	low := strings.ToLower(arg)
	if low == "up" || low == "increase" || low == "+" || low == "more" {
		return upsizeGPUNext(s)
	}
	return upsizeGPUNamed(s, arg)
}

func gpuVRAMGB(t gcp.GPUTarget) int {
	n := strings.ToUpper(t.Name + " " + t.Reason)
	switch {
	case strings.Contains(n, "H200"):
		return 141
	case strings.Contains(n, "H100"):
		return 80
	case strings.Contains(n, "A100") && strings.Contains(n, "80"):
		return 80
	case strings.Contains(n, "A100"):
		return 40
	case strings.Contains(n, "L4"):
		return 24
	case strings.Contains(n, "A10"):
		return 24
	case strings.Contains(n, "T4"):
		return 16
	}
	return 0
}

func nextLargerGPU(cur string, cands []gcp.GPUTarget) (gcp.GPUTarget, error) {
	curV := gpuVRAMGB(gcp.GPUTarget{Name: cur})
	if curV == 0 {
		curV = 24
	}
	var best gcp.GPUTarget
	bestV := 1 << 30
	for _, t := range cands {
		v := gpuVRAMGB(t)
		if v > curV && v < bestV {
			best, bestV = t, v
		}
	}
	if best.MachineType == "" {
		return gcp.GPUTarget{}, fmt.Errorf("no larger GPU than %s in catalog — /gpu to pick", dash(cur))
	}
	return best, nil
}

func upsizeGPUNext(s *runSession) error {
	if s.Model.Kind() != store.BackendGCP {
		return settingsUpsizeGPU(s)
	}
	refreshSpotQuotes(s)
	cur := strings.TrimSpace(s.Model.GPUPool)
	if cur == "" {
		cur = "L4"
	}
	t, err := nextLargerGPU(cur, quotedResizeCandidates())
	if err != nil {
		return err
	}
	printKV(s.out(), "gpu", fmt.Sprintf("%s → %s (%s)", cur, t.Name, t.MachineType))
	return upsizeGCPTarget(s, t)
}

func upsizeGPUNamed(s *runSession, name string) error {
	if s.Model.Kind() != store.BackendGCP {
		return settingsUpsizeGPU(s)
	}
	refreshSpotQuotes(s)
	for _, t := range quotedResizeCandidates() {
		if strings.EqualFold(t.Name, name) || strings.EqualFold(t.MachineType, name) {
			printKV(s.out(), "gpu", t.Name+"  "+t.MachineType)
			return upsizeGCPTarget(s, t)
		}
	}
	return fmt.Errorf("unknown GPU %q — /gpu to pick from the list", name)
}

func upsizeRunpod(s *runSession) error {
	if s.Model.EndpointID == "" {
		return fmt.Errorf("no RunPod endpoint on this session")
	}
	env := config.Load()
	if env.RunpodAPIKey == "" {
		return fmt.Errorf("not connected — run `runhug connect`")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rp := runpod.New(env.RunpodAPIKey)
	gpus, err := rp.ListGPUs(ctx)
	if err != nil {
		return err
	}
	need := 0.0
	cur := strings.TrimSpace(s.Model.GPUPool)
	opts := runpod.FittingOptions(gpus, need, 8)
	if len(opts) == 0 {
		return fmt.Errorf("no GPU pools in catalog")
	}
	rows := buildGPUPickRows(opts)
	fmt.Fprintf(s.out(), "current pool %s  count %d  ~%s\n", dash(cur), s.Model.GPUCount, gcp.FormatKeepUp(s.Model.HourlyUSD))
	pool, err := pickGPUPoolPlain(s.out(), rows)
	if err != nil {
		return err
	}
	if strings.EqualFold(pool, cur) {
		fmt.Fprintln(s.out(), dim("already on "+pool))
		return nil
	}
	ch, err := runpod.Pick(gpus, need, pool, 1)
	if err != nil {
		return err
	}
	fromH := s.Model.HourlyUSD
	printCostBlock(s.out(), gcp.FormatCostChange(cur, pool, fromH, ch.HourlyUSD))
	title := fmt.Sprintf("%s resize %s → %s — bills while a worker is up", dim("gpu"), cur, pool)
	id, err := promptChoice(s.tty(), nil, title, resizeChoiceOpts(), 1)
	if err != nil {
		return err
	}
	if id != "resize" {
		fmt.Fprintln(s.out(), dim("cancelled"))
		return nil
	}
	count := s.Model.GPUCount
	if count < 1 {
		count = 1
	}
	ep, err := rp.UpdateEndpoint(ctx, s.Model.EndpointID, runpod.UpdateEndpointRequest{
		GPU: &runpod.GPUConfig{Pools: []string{pool}, Count: count},
	})
	if err != nil {
		return err
	}
	_ = ep
	if err := patchRegistryGPU(s.RegistryKey, func(m *store.Model) {
		m.GPUPool = pool
		m.GPUCount = count
		m.HourlyUSD = ch.HourlyUSD
	}); err != nil {
		fmt.Fprintln(s.errw(), yellow("warning:")+" registry "+err.Error())
	}
	s.Model.GPUPool = pool
	s.Model.GPUCount = count
	s.Model.HourlyUSD = ch.HourlyUSD
	fmt.Fprintln(s.out(), green("✓")+"  "+dim("endpoint GPU updated — waiting for workers"))
	h, err := EnsureReady(ctx, s.Model, EnsureOpts{Writer: s.errw()})
	if err != nil {
		return err
	}
	s.Target.BaseURL = h.Target.BaseURL
	return nil
}

func upsizeGCP(s *runSession) error {
	project := strings.TrimSpace(s.Model.EndpointID)
	zone := strings.TrimSpace(s.Model.EndpointType)
	name := strings.TrimSpace(s.Model.PodID)
	if project == "" || zone == "" || name == "" {
		return fmt.Errorf("GCP registry row needs project, zone, instance")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := gcp.NewClient().RefreshSpotQuotes(ctx); err != nil {
		fmt.Fprintln(s.errw(), yellow("warning:")+" "+err.Error())
	}
	cands := quotedResizeCandidates()
	if len(cands) == 0 {
		return fmt.Errorf("no Google Spot GPU SKUs for this region — cannot quote a resize")
	}
	labels := make([]string, len(cands))
	for i, t := range cands {
		labels[i] = fmt.Sprintf("%s  %s", t.Name, t.MachineType)
	}
	fmt.Fprintln(s.out(), dim("Google Cloud Billing Spot GPU SKU (list · Iowa/Americas). vCPU/RAM extra. Spot can change daily."))
	fmt.Fprintf(s.out(), "current %s  %s\n", dash(s.Model.GPUPool), gcp.FormatKeepUp(hourlyForSessionGPU(s)))
	picked, err := pickNumbered("GPU", labels, func(w io.Writer) {
		printGPUResizeTable(w, cands)
	}, false)
	if err != nil {
		return err
	}
	var target gcp.GPUTarget
	for i, l := range labels {
		if l == picked {
			target = cands[i]
			break
		}
	}
	if target.MachineType == "" {
		return fmt.Errorf("unknown GPU pick")
	}
	return upsizeGCPTarget(s, target)
}

func upsizeGCPTarget(s *runSession, target gcp.GPUTarget) error {
	project := strings.TrimSpace(s.Model.EndpointID)
	zone := strings.TrimSpace(s.Model.EndpointType)
	name := strings.TrimSpace(s.Model.PodID)
	if project == "" || zone == "" || name == "" {
		return fmt.Errorf("GCP registry row needs project, zone, instance")
	}
	fromH := hourlyForSessionGPU(s)
	toH := hourlyForGCPTarget(target)
	if toH <= 0 {
		return fmt.Errorf("Google Cloud Billing has no Spot GPU SKU for %s (%s) — not quoting a guess", target.Name, target.MachineType)
	}
	printCostBlock(s.out(), gcp.FormatCostChange(dash(s.Model.GPUPool), target.Name, fromH, toH))
	title := fmt.Sprintf("%s Spot resize → %s (%s) — VM will stop/start; capacity may fail", dim("gpu"), target.Name, target.MachineType)
	id, err := promptChoice(s.tty(), nil, title, resizeChoiceOpts(), 1)
	if err != nil {
		return err
	}
	if id != "resize" {
		fmt.Fprintln(s.out(), dim("cancelled"))
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	client := gcp.NewClient()
	fmt.Fprintln(s.out(), dim("stopping instance…"))
	if err := client.ResizeInstance(ctx, project, zone, name, target); err != nil {
		return fmt.Errorf("gcp resize: %w (Spot capacity? try again or pick another GPU)", err)
	}
	if err := patchRegistryGPU(s.RegistryKey, func(m *store.Model) {
		m.GPUPool = target.Name
		m.HourlyUSD = toH
	}); err != nil {
		fmt.Fprintln(s.errw(), yellow("warning:")+" registry "+err.Error())
	}
	s.Model.GPUPool = target.Name
	s.Model.HourlyUSD = toH
	if s.Cleanup != nil {
		s.Cleanup()
		s.Cleanup = func() {}
	}
	h, err := EnsureReady(ctx, s.Model, EnsureOpts{Writer: s.errw(), GCP: client})
	if err != nil {
		return err
	}
	s.Target = h.Target
	s.Cleanup = h.Cleanup
	fmt.Fprintln(s.out(), green("✓")+"  "+dim("instance resized · tunnel "+s.Target.BaseURL))
	return nil
}

func refreshSpotQuotes(s *runSession) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := gcp.NewClient().RefreshSpotQuotes(ctx); err != nil && s != nil {
		fmt.Fprintln(s.errw(), yellow("warning:")+" "+err.Error())
	}
}

func quotedResizeCandidates() []gcp.GPUTarget {
	var out []gcp.GPUTarget
	for _, t := range gcpResizeCandidates() {
		if gcp.SpotQuoteHourly(t) > 0 {
			out = append(out, t)
		}
	}
	return out
}

func printGPUResizeTable(w io.Writer, cands []gcp.GPUTarget) {
	cols := []tableCol{
		{Title: "#", Min: 2, Max: 3, Right: true},
		{Title: "GPU", Min: 8, Max: 28},
		{Title: "MACHINE", Min: 12, Max: 18},
		{Title: "VRAM", Min: 4, Max: 6, Right: true},
		{Title: "GPUS", Min: 4, Max: 4, Right: true},
		{Title: "$/HR", Min: 7, Max: 10, Right: true},
		{Title: "/DAY", Min: 7, Max: 10, Right: true},
		{Title: "/MO", Min: 8, Max: 12, Right: true},
	}
	rows := make([][]tableCell, 0, len(cands))
	for i, t := range cands {
		hr := gcp.SpotQuoteHourly(t)
		nGPU := gcp.GPUCountFromMachine(t.MachineType)
		vram := "—"
		if gb := gpuVRAMGB(t); gb > 0 {
			vram = fmt.Sprintf("%d", gb)
		}
		gpus := "1"
		if nGPU > 1 {
			gpus = strconv.Itoa(nGPU)
		}
		rows = append(rows, []tableCell{
			styledCell(strconv.Itoa(i+1), cyan),
			styledCell(t.Name, bold),
			styledCell(t.MachineType, dim),
			cell(vram),
			cell(gpus),
			cell(tableUSD(hr)),
			cell(tableUSD2(gcp.KeepUpDayUSD(hr))),
			cell(tableUSD2(gcp.KeepUpMonthUSD(hr))),
		})
	}
	printTable(w, cols, rows)
}

func gcpResizeCandidates() []gcp.GPUTarget {
	seen := map[string]bool{}
	var out []gcp.GPUTarget
	add := func(t gcp.GPUTarget) {
		if t.MachineType == "" || seen[t.Name+"|"+t.MachineType] {
			return
		}
		seen[t.Name+"|"+t.MachineType] = true
		out = append(out, t)
	}
	for _, t := range gcp.DefaultTargets() {
		add(t)
	}
	if list, err := gpudb.LoadGCP(); err == nil {
		for _, a := range list {
			if a.SpotHourlyApprox <= 0 {
				continue
			}
			if strings.Contains(strings.ToUpper(a.Name+" "+a.HFName+" "+a.ID), "VWS") {
				continue
			}
			if !gcp.PlausibleGPUMachine(a.MachineHint) {
				continue
			}
			nGPU := gcp.GPUCountFromMachine(a.MachineHint)
			reason := fmt.Sprintf("%.0f GB · %s", a.MemoryGB, a.Series)
			if nGPU > 1 {
				reason = fmt.Sprintf("%.0f GB · %s · %d× GPU", a.MemoryGB, a.Series, nGPU)
			}
			t := gcp.GPUTarget{
				Name:        a.HFName,
				MachineType: a.MachineHint,
				DiskGB:      200,
				Reason:      reason,
			}
			if t.Name == "" {
				t.Name = a.Name
			}
			if strings.EqualFold(a.Attach, "accelerator") {
				t.Accelerator = a.ID
				t.AcceleratorCnt = 1
				if t.MachineType == "" {
					t.MachineType = gcp.MachineTypeT4
				}
			}
			add(t)
		}
	}
	return out
}

func patchRegistryGPU(key string, mut func(*store.Model)) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("no registry key")
	}
	reg, _, err := store.Load()
	if err != nil {
		return err
	}
	m, ok := reg.Lookup(key)
	if !ok {
		return fmt.Errorf("unknown model %s", key)
	}
	mut(&m)
	reg.Put(m)
	return reg.Save()
}

func hourlyForSessionGPU(s *runSession) float64 {
	if s != nil && s.Model.HourlyUSD > 0 {
		return s.Model.HourlyUSD
	}
	name := ""
	if s != nil {
		name = strings.TrimSpace(s.Model.GPUPool)
	}
	if name == "" {
		name = gcp.GPUTypeL4
	}
	return hourlyForGCPTarget(gcp.GPUTarget{Name: name})
}

func hourlyForGCPTarget(t gcp.GPUTarget) float64 {
	return gcp.SpotQuoteHourly(t)
}
