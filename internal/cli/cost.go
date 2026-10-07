package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/gcp"
	"github.com/adamsiwiec1/runhug/internal/gpudb"
	"github.com/adamsiwiec1/runhug/internal/jobs"
	"github.com/adamsiwiec1/runhug/internal/runpod"
	"github.com/adamsiwiec1/runhug/internal/store"
)

type costRow struct {
	Key     string  `json:"key"`
	Model   string  `json:"model"`
	Backend string  `json:"backend"`
	GPU     string  `json:"gpu,omitempty"`
	State   string  `json:"state"`
	Up      bool    `json:"up"`
	Hourly  float64 `json:"hourly_usd"`
	Now     float64 `json:"now_usd"`
	Note    string  `json:"note,omitempty"`
}

type costTotals struct {
	UpN       int     `json:"up_n"`
	DownN     int     `json:"down_n"`
	NowHourly float64 `json:"now_hourly_usd"`
	AllHourly float64 `json:"all_up_hourly_usd"`
}

func cmdCost(args []string) error {
	if showCmdHelp(args, "runhug cost", printCostHelp, printCostHelpFull) {
		return nil
	}
	fs := newFlagSet("cost")
	backend := fs.String("backend", "", "gcp | runpod | local (default all)")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	reg, path, err := store.Load()
	if err != nil {
		return err
	}
	if len(reg.Models) == 0 {
		if *asJSON {
			return writeJSON(map[string]any{"rows": []costRow{}, "registry": path})
		}
		heading(os.Stdout, "Cost")
		fmt.Fprintf(os.Stdout, "%s  %s\n", dim("registry"), path)
		commands(os.Stdout, "Empty:", "runhug gcp deploy <gguf>", "runhug deploy <model>")
		return nil
	}

	filter := strings.ToLower(strings.TrimSpace(*backend))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if hasGCPModels(reg, filter) {
		_ = gcp.NewClient().RefreshSpotQuotes(ctx)
	}
	rows := collectCostRows(ctx, reg, filter)
	tot := sumCost(rows)
	by := sumCostByBackend(rows)

	if *asJSON {
		return writeJSON(map[string]any{
			"rows":       rows,
			"now":        tot,
			"by_backend": by,
			"note":       "now = compute while UP. all_up = if every cloud row ran 24/7. GPU SKU list; vCPU/RAM/disk extra. Spot changes daily.",
		})
	}

	heading(os.Stdout, "Cost")
	fmt.Fprintln(os.Stdout, dim("Google Spot GPU SKU / stored RunPod rate. vCPU/RAM/disk extra. Down Spot compute ≈ $0."))
	printCostTable(os.Stdout, rows)
	fmt.Fprintln(os.Stdout)
	printCostTotals(os.Stdout, tot, by)
	return nil
}

func hasGCPModels(reg *store.Registry, filter string) bool {
	if filter != "" && filter != store.BackendGCP {
		return false
	}
	for _, m := range reg.Models {
		if m.Kind() == store.BackendGCP {
			return true
		}
	}
	return false
}

func collectCostRows(ctx context.Context, reg *store.Registry, filter string) []costRow {
	keys := make([]string, 0, len(reg.Models))
	for k := range reg.Models {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]costRow, 0, len(keys))
	for _, k := range keys {
		m := reg.Models[k]
		kind := m.Kind()
		if filter != "" && kind != filter {
			continue
		}
		row := costRow{
			Key:     k,
			Model:   m.HFRepo,
			Backend: kind,
			GPU:     strings.TrimSpace(m.GPUPool),
			Hourly:  catalogHourly(m),
		}
		row.State, row.Up, row.Note = liveState(ctx, m)
		if row.Up {
			row.Now = row.Hourly
		}
		out = append(out, row)
	}
	return out
}

func catalogHourly(m store.Model) float64 {
	switch m.Kind() {
	case store.BackendGCP:
		if h := gcpHourlyFromModel(m); h > 0 {
			return h
		}
	case store.BackendRunpod:
		if m.HourlyUSD > 0 {
			return m.HourlyUSD
		}
	}
	return 0
}

func gcpHourlyFromModel(m store.Model) float64 {
	name := strings.TrimSpace(m.GPUPool)
	if name == "" {
		name = gcp.GPUTypeL4
	}
	if list, err := gpudb.LoadGCP(); err == nil {
		for _, a := range list {
			if strings.EqualFold(a.HFName, name) || strings.EqualFold(a.Name, name) {
				t := gcp.GPUTarget{Name: a.HFName, MachineType: a.MachineHint}
				if t.Name == "" {
					t.Name = a.Name
				}
				if h := gcp.SpotQuoteHourly(t); h > 0 {
					return h
				}
			}
		}
	}
	if h := gcp.SpotQuoteHourly(gcp.GPUTarget{Name: name}); h > 0 {
		return h
	}
	return m.HourlyUSD
}

func liveState(ctx context.Context, m store.Model) (state string, up bool, note string) {
	switch m.Kind() {
	case store.BackendLocal:
		return "local", false, "not billed"
	case store.BackendGCP:
		return gcpLiveState(ctx, m)
	default:
		return runpodLiveState(ctx, m)
	}
}

func gcpLiveState(ctx context.Context, m store.Model) (string, bool, string) {
	project := strings.TrimSpace(m.EndpointID)
	zone := strings.TrimSpace(m.EndpointType)
	name := strings.TrimSpace(m.PodID)
	if name == "" {
		name = gcp.InstanceName(m.HFRepo)
	}
	if project == "" || zone == "" || name == "" {
		return "unknown", false, "missing project/zone/instance"
	}
	st, err := gcp.NewClient().DescribeInstance(ctx, project, zone, name)
	if err != nil {
		return "unknown", false, "describe failed"
	}
	u := strings.ToUpper(strings.TrimSpace(st.Status))
	switch u {
	case "RUNNING":
		return "up", true, ""
	case "STAGING", "PROVISIONING", "STARTING":
		return "starting", true, u
	case "STOPPING":
		return "stopping", true, u
	case "TERMINATED", "STOPPED", "SUSPENDED":
		return "down", false, u
	default:
		if u == "" {
			return "unknown", false, ""
		}
		return strings.ToLower(u), false, u
	}
}

func runpodLiveState(ctx context.Context, m store.Model) (string, bool, string) {
	env := config.Load()
	if m.PodID != "" && m.EndpointID == "" {
		if env.RunpodAPIKey == "" {
			return "unknown", false, "not connected"
		}
		pod, err := runpod.New(env.RunpodAPIKey).GetPod(ctx, m.PodID)
		if err != nil || pod == nil {
			return "unknown", false, "pod lookup failed"
		}
		st := strings.ToUpper(pod.Status)
		if st == runpod.PodStatusRunning || st == runpod.PodStatusStarting || st == runpod.PodStatusProvisioning {
			return "up", true, st
		}
		return "down", false, st
	}
	if m.EndpointID == "" {
		return "unknown", false, "no endpoint"
	}
	if env.RunpodAPIKey == "" {
		return "unknown", false, "not connected"
	}
	if h, err := jobs.Health(env.RunpodAPIKey, m.EndpointID); err == nil && h != nil && h.Workers != nil {
		wr := h.Workers
		ready := ptrInt(wr.Ready)
		running := ptrInt(wr.Running)
		idle := ptrInt(wr.Idle)
		init := ptrInt(wr.Initializing)
		note := fmt.Sprintf("ready=%d run=%d idle=%d init=%d", ready, running, idle, init)
		if ready > 0 || running > 0 || idle > 0 {
			return "up", true, note
		}
		if init > 0 {
			return "starting", true, note
		}
		return "down", false, note
	}
	ep, err := runpod.New(env.RunpodAPIKey).GetEndpoint(ctx, m.EndpointID)
	if err == nil && ep != nil && ep.Workers != nil && ep.Workers.Min > 0 {
		return "up", true, fmt.Sprintf("min workers %d", ep.Workers.Min)
	}
	return "down", false, "scale-to-zero"
}

func sumCost(rows []costRow) costTotals {
	var t costTotals
	for _, r := range rows {
		if r.Backend == store.BackendLocal {
			continue
		}
		t.AllHourly += r.Hourly
		if r.Up {
			t.UpN++
			t.NowHourly += r.Now
		} else {
			t.DownN++
		}
	}
	return t
}

func sumCostByBackend(rows []costRow) map[string]costTotals {
	out := map[string]costTotals{}
	grouped := map[string][]costRow{}
	for _, r := range rows {
		grouped[r.Backend] = append(grouped[r.Backend], r)
	}
	for k, g := range grouped {
		out[k] = sumCost(g)
	}
	return out
}

func printCostTable(w io.Writer, rows []costRow) {
	cols := []tableCol{
		{Title: "#", Min: 2, Max: 3, Right: true},
		{Title: "MODEL", Min: 8, Max: 28},
		{Title: "KIND", Min: 6, Max: 8},
		{Title: "GPU", Min: 4, Max: 16},
		{Title: "STATE", Min: 5, Max: 10},
		{Title: "$/HR", Min: 6, Max: 10, Right: true},
		{Title: "NOW", Min: 6, Max: 10, Right: true},
		{Title: "/DAY", Min: 7, Max: 10, Right: true},
		{Title: "/MO", Min: 8, Max: 12, Right: true},
	}
	out := make([][]tableCell, 0, len(rows))
	for i, r := range rows {
		stStyle := dim
		switch r.State {
		case "up", "starting", "stopping":
			stStyle = green
		case "unknown":
			stStyle = yellow
		}
		gpu := r.GPU
		if gpu == "" {
			gpu = "—"
		}
		out = append(out, []tableCell{
			styledCell(strconv.Itoa(i+1), cyan),
			styledCell(r.Model, bold),
			cell(r.Backend),
			styledCell(gpu, dim),
			styledCell(r.State, stStyle),
			cell(dashRate(r.Hourly)),
			cell(dashRate(r.Now)),
			cell(tableUSD2(gcp.KeepUpDayUSD(r.Hourly))),
			cell(tableUSD2(gcp.KeepUpMonthUSD(r.Hourly))),
		})
	}
	printTable(w, cols, out)
}

func dashRate(v float64) string {
	if v <= 0 {
		return "—"
	}
	return tableUSD(v)
}

func printCostTotals(w io.Writer, tot costTotals, by map[string]costTotals) {
	fmt.Fprintln(w, bold("Now (instances up)"))
	printKV(w, "up", fmt.Sprintf("%d   %s", tot.UpN, formatKeepUpOrZero(tot.NowHourly)))
	printKV(w, "down", fmt.Sprintf("%d   compute ≈ $0 (disk may still bill)", tot.DownN))
	fmt.Fprintln(w)
	fmt.Fprintln(w, bold("If every cloud row stayed up 24/7"))
	printKV(w, "all", formatKeepUpOrZero(tot.AllHourly))
	keys := make([]string, 0, len(by))
	for k := range by {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t := by[k]
		if k == store.BackendLocal {
			printKV(w, k, "not billed")
			continue
		}
		printKV(w, k, fmt.Sprintf("now %s  ·  all-up %s", tableUSD(t.NowHourly)+"/hr", tableUSD(t.AllHourly)+"/hr"))
	}
}

func formatKeepUpOrZero(h float64) string {
	if h <= 0 {
		return "$0/hr · $0/day · $0/mo"
	}
	return gcp.FormatKeepUp(h)
}

func printCostHelp(w io.Writer) {
	helpUsage(w, "runhug cost")
	fmt.Fprintln(w, dim("Registry instances: up/down, GPU SKU $/hr, now vs 24/7 totals."))
	helpSection(w, "flags")
	helpFlag(w, "--backend", "gcp | runpod | local")
	helpFlag(w, "--json", "machine-readable rows + sums")
}

func printCostHelpFull(w io.Writer) {
	helpUsage(w, "runhug cost")
	fmt.Fprintln(w, dim("Short list:"), cyan("runhug cost --help"))
	fmt.Fprintln(w)
	helpFullSection(w, "overview",
		helpFullEntry{
			Cmd:  "cost",
			What: "Live status + Google Spot GPU SKU / stored RunPod rate for every registry row, with now and 24/7 sums.",
			When: "See what is burning money vs stopped, and the bill if everything stayed up.",
			More: "NOW = $/hr while UP. /DAY /MO = that row’s rate × 24h / 30d (kept up).\n  Down GCP Spot compute ≈ $0 (boot disk can still bill). vCPU/RAM extra on GCP.",
		},
	)
}
