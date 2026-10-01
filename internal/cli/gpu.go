package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/gpudb"
)

func cmdGPU(args []string) error {
	if showCmdHelp(args, "runhug gpu", printGPUHelp, printGPUHelpFull) {
		return nil
	}
	if len(args) == 0 {
		return cmdGPUList(nil)
	}
	switch strings.ToLower(args[0]) {
	case "list", "ls":
		return cmdGPUList(args[1:])
	case "set":
		return cmdGPUSet(args[1:])
	case "clear", "unset":
		return cmdGPUClear(args[1:])
	case "show", "get", "status":
		return cmdGPUShow(args[1:])
	case "update", "refresh":
		return cmdGPUUpdate(args[1:])
	default:
		// Treat unknown first token as list query for convenience: `gpu 4090`
		if strings.HasPrefix(args[0], "-") {
			return cmdGPUList(args)
		}
		return fmt.Errorf("unknown gpu command %q (list|set|clear|show|update)\nRun `runhug gpu --help` or `runhug gpu --help-full`", args[0])
	}
}

func printGPUHelp(w io.Writer) {
	helpUsage(w, "runhug gpu <command>")

	helpSection(w, "commands")
	helpCmd(w, "list", "hardware catalog + RunPod/GCP/local")
	helpCmd(w, "set [name]", "save preference for deploy / local")
	helpCmd(w, "clear", "forget saved preference")
	helpCmd(w, "show", "print saved preference")
	helpCmd(w, "update", "refresh NVIDIA + AMD + GCP catalogs")
	fmt.Fprintln(w)

	helpSection(w, "list flags")
	helpFlag(w, "--filter", "all|local|amd|nvidia|runpod|gcp")
	helpFlag(w, "--sort", "best|cheapest|value|vram|name")
	helpFlag(w, "--query -q", "substring filter")
	fmt.Fprintln(w)

	fmt.Fprintf(w, "%s %s\n", dim("alias:"), cyan("runhug gpus …"))
}

func printGPUHelpFull(w io.Writer) {
	helpUsage(w, "runhug gpu <command>")
	fmt.Fprintln(w, dim("Hardware catalogs for deploy sizing, local picks, and cloud pools."))
	fmt.Fprintln(w, dim("Short list:"), cyan("runhug gpu --help"))
	fmt.Fprintln(w)

	helpFullSection(w, "commands",
		helpFullEntry{
			Cmd:  "list [query]",
			What: "Browse the hardware index, optionally joined with live RunPod/GCP stock and GPUs detected on this machine.",
			When: "Choosing a pool for deploy, comparing VRAM/price, or checking what you can run locally.",
			More: "Alias: runhug gpus …\n  --filter all|local|amd|nvidia|runpod|gcp\n  --sort best|cheapest|value|vram|name\n  --query/-q substring · --min-vram N · --limit N · --json",
		},
		helpFullEntry{
			Cmd:  "set [name|#]",
			What: "Save a GPU preference (provider + key/name/VRAM) used by deploy and local flows.",
			When: "You always want the same pool without retyping it each deploy.",
			More: "Interactive picker if no name given (TTY). --filter / --sort narrow the list.",
		},
		helpFullEntry{
			Cmd:  "clear",
			What: "Remove the saved GPU preference from settings.",
			When: "Switching providers or clearing a shared machine.",
		},
		helpFullEntry{
			Cmd:  "show",
			What: "Print the saved preference (provider, key, name, VRAM).",
			When: "Confirming what deploy will prefer.",
		},
		helpFullEntry{
			Cmd:  "update",
			What: "Refresh NVIDIA, AMD, and GCP catalogs into ~/.config/runhug/gpudb/.",
			When: "Catalog looks stale, or after new SKUs land upstream.",
			More: "Reports how many GPUs were added (or “no new GPUs”).\n  --nvidia / --amd / --gcp refresh one vendor only.\n  --release prefers GitHub Release assets when published.",
		},
	)

	helpSection(w, "examples")
	fmt.Fprintln(w, "  "+cyan(`runhug gpu list --filter local`))
	fmt.Fprintln(w, "  "+cyan(`runhug gpu list --query MI300 --sort vram`))
	fmt.Fprintln(w, "  "+cyan(`runhug gpu set "RTX 4090"`))
	fmt.Fprintln(w, "  "+cyan(`runhug gpu update`))
	fmt.Fprintln(w)
}

func cmdGPUUpdate(args []string) error {
	if showCmdHelp(args, "runhug gpu update", printGPUUpdateHelp, printGPUUpdateHelpFull) {
		return nil
	}
	fs := newFlagSet("gpu update")
	nvidia := fs.Bool("nvidia", false, "refresh NVIDIA hardware index only")
	amd := fs.Bool("amd", false, "refresh AMD hardware index only")
	gcpOnly := fs.Bool("gcp", false, "refresh GCP accelerator catalog only")
	preferRelease := fs.Bool("release", true, "prefer GitHub Release assets when published (gpudb-*.json)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	any := *nvidia || *amd || *gcpOnly
	opts := gpudb.UpdateOptions{
		NVIDIA:        *nvidia || !any,
		AMD:           *amd || !any,
		GCP:           *gcpOnly || !any,
		PreferRelease: *preferRelease,
	}
	if any {
		opts.NVIDIA, opts.AMD, opts.GCP = *nvidia, *amd, *gcpOnly
	}

	heading(os.Stdout, "Update GPU catalogs")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, dim("Shipped with the CLI; updates land in ~/.config/runhug/gpudb/."))
	fmt.Fprintln(os.Stdout)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	res, err := gpudb.Update(ctx, opts)
	if err != nil {
		return err
	}
	if opts.NVIDIA {
		printKV(os.Stdout, "nvidia", fmt.Sprintf("%d SKUs from %s", res.NVIDIACount, res.NVIDIAFrom))
		printGPUDelta(os.Stdout, res.NVIDIAAdded)
	}
	if opts.AMD {
		printKV(os.Stdout, "amd", fmt.Sprintf("%d SKUs from %s", res.AMDCount, res.AMDFrom))
		printGPUDelta(os.Stdout, res.AMDAdded)
	}
	if opts.GCP {
		printKV(os.Stdout, "gcp", fmt.Sprintf("%d accelerators from %s", res.GCPCount, res.GCPFrom))
		printGPUDelta(os.Stdout, res.GCPAdded)
	}
	printKV(os.Stdout, "cache", res.CacheDir)
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, dim("List: runhug gpu list --query MI300   or   --filter local"))
	return nil
}

func printGPUUpdateHelp(w io.Writer) {
	helpUsage(w, "runhug gpu update")
	helpSection(w, "flags")
	helpFlag(w, "--nvidia", "refresh NVIDIA index only")
	helpFlag(w, "--amd", "refresh AMD index only")
	helpFlag(w, "--gcp", "refresh GCP catalog only")
	helpFlag(w, "--release", "prefer GitHub Release assets (default true)")
	fmt.Fprintln(w)
}

func printGPUUpdateHelpFull(w io.Writer) {
	helpUsage(w, "runhug gpu update")
	fmt.Fprintln(w, dim("Refresh hardware catalogs into ~/.config/runhug/gpudb/."))
	fmt.Fprintln(w)
	helpFullSection(w, "behavior",
		helpFullEntry{
			Cmd:  "default",
			What: "Fetches NVIDIA + AMD + GCP catalogs and writes JSON under the gpudb cache dir.",
			When: "After install, or when list looks missing new cards.",
			More: "Compares against the previous cache and prints “N added: …” or “no new GPUs”.",
		},
		helpFullEntry{
			Cmd:  "--nvidia / --amd / --gcp",
			What: "Limit the refresh to one vendor.",
			When: "You only care about one catalog (e.g. GCP after gcloud auth).",
		},
	)
}

func printGPUListHelp(w io.Writer) {
	helpUsage(w, "runhug gpu list [query]")
	helpSection(w, "flags")
	helpFlag(w, "--filter", "all|local|amd|nvidia|runpod|gcp")
	helpFlag(w, "--sort", "best|cheapest|value|vram|name")
	helpFlag(w, "--query -q", "substring filter")
	helpFlag(w, "--min-vram", "minimum VRAM GB")
	helpFlag(w, "--limit", "max rows (0 = all)")
	helpFlag(w, "--json", "print JSON")
	fmt.Fprintln(w)
}

func printGPUListHelpFull(w io.Writer) {
	helpUsage(w, "runhug gpu list [query]")
	fmt.Fprintln(w, dim("List and filter the hardware catalog."))
	fmt.Fprintln(w)
	helpFullSection(w, "flags",
		helpFullEntry{
			Cmd:  "--filter local",
			What: "Show GPUs detected on this machine plus the local-capable catalog.",
			When: "Deciding what you can run without cloud.",
		},
		helpFullEntry{
			Cmd:  "--filter runpod|gcp",
			What: "Cloud pools (live stock when connected).",
			When: "Picking a deploy target.",
		},
		helpFullEntry{
			Cmd:  "--sort / --query / --min-vram",
			What: "Rank and narrow rows.",
			When: "Searching for a class (e.g. MI300) or a VRAM floor.",
		},
	)
}

func printGPUDelta(w io.Writer, added []string) {
	indent := dim(padRight("", 9))
	if len(added) == 0 {
		fmt.Fprintf(w, "  %s  %s\n", indent, dim("no new GPUs"))
		return
	}
	const maxShow = 12
	shown := added
	suffix := ""
	if len(added) > maxShow {
		shown = added[:maxShow]
		suffix = fmt.Sprintf(" (+%d more)", len(added)-maxShow)
	}
	fmt.Fprintf(w, "  %s  %s\n", indent, fmt.Sprintf("%d added: %s%s", len(added), strings.Join(shown, ", "), suffix))
}

func cmdGPUList(args []string) error {
	if showCmdHelp(args, "runhug gpu list", printGPUListHelp, printGPUListHelpFull) {
		return nil
	}
	fs := newFlagSet("gpu list")
	filter := fs.String("filter", "all", "all | local | amd | nvidia | runpod | gcp")
	sortKey := fs.String("sort", "best", "best | cheapest | value | vram | name")
	query := fs.String("query", "", "substring filter on name/pool")
	fs.StringVar(query, "q", "", "alias for --query")
	limit := fs.Int("limit", 0, "max rows (0 = all)")
	minVRAM := fs.Float64("min-vram", 0, "minimum VRAM GB")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 && strings.TrimSpace(*query) == "" {
		*query = strings.Join(fs.Args(), " ")
	}

	isLocal := strings.EqualFold(*filter, "local")
	var onMachine []gpuRow
	if isLocal {
		specs, err := gpudb.Load()
		if err != nil {
			return err
		}
		onMachine = detectedLocalRows(specs)
	}

	rows, err := buildGPURows(*filter, *query)
	if err != nil {
		return err
	}
	if *minVRAM > 0 {
		filtered := rows[:0]
		for _, r := range rows {
			if r.MemoryGB >= *minVRAM {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}
	sortGPURows(rows, *sortKey)
	catalogAll := rows
	yRank, yTotal, yRow := yoursRank(rows)
	if *limit > 0 && len(rows) > *limit {
		// Keep the user's GPU visible even when limiting.
		if yRank > *limit && yRank > 0 {
			kept := append([]gpuRow{}, rows[:*limit-1]...)
			kept = append(kept, rows[yRank-1])
			rows = kept
		} else {
			rows = rows[:*limit]
		}
	}

	if *asJSON {
		if isLocal {
			return writeJSON(localListPayload{
				OnThisMachine: onMachine,
				Catalog:       catalogAll,
				YoursRank:     yRank,
				YoursTotal:    yTotal,
			})
		}
		type allPayload struct {
			GPUs       []gpuRow `json:"gpus"`
			YoursRank  int      `json:"yours_rank,omitempty"`
			YoursTotal int      `json:"yours_total,omitempty"`
			Yours      *gpuRow  `json:"yours,omitempty"`
		}
		p := allPayload{GPUs: catalogAll, YoursRank: yRank, YoursTotal: yTotal}
		if yRank > 0 {
			r := yRow
			p.Yours = &r
		}
		return writeJSON(p)
	}

	heading(os.Stdout, "GPUs")
	printKV(os.Stdout, "filter", *filter)
	printKV(os.Stdout, "sort", *sortKey)
	if pref := savedGPUPreference(); pref != nil {
		printKV(os.Stdout, "preference", fmt.Sprintf("%s %s (%s)", pref.Provider, pref.Key, pref.Name))
	}
	if yRank > 0 {
		extra := ""
		if yRow.FP16 > 0 {
			extra = fmt.Sprintf(" · FP16≈%.0f", yRow.FP16)
		}
		printKV(os.Stdout, "yours", fmt.Sprintf("%s — rank #%d of %d by %s%s",
			yRow.Name, yRank, yTotal, *sortKey, extra))
	}
	fmt.Fprintln(os.Stdout)

	if isLocal {
		fmt.Fprintln(os.Stdout, bold("On this machine"))
		if len(onMachine) == 0 {
			fmt.Fprintln(os.Stdout, dim("  (none detected — install NVIDIA drivers / nvidia-smi, or Apple Silicon for unified memory)"))
		} else {
			printGPUTable(onMachine)
		}
		fmt.Fprintln(os.Stdout)
		fmt.Fprintf(os.Stdout, "%s  %s\n", bold("Local catalog"), dim(fmt.Sprintf("(%d SKUs you can run locally)", len(catalogAll))))
	}

	printGPUTableHeader()
	if len(rows) == 0 {
		fmt.Fprintln(os.Stdout, dim("  (no GPUs matched)"))
		if strings.EqualFold(*filter, "runpod") && config.Load().RunpodAPIKey == "" {
			fmt.Fprintln(os.Stdout, dim("  tip: run `runhug connect` for live stock; showing offline catalog"))
		}
		return nil
	}
	printGPUTableRows(rows)
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, dim("FP16 = TechPowerUp vector TFLOPS (not tensor-peak). TC = tensor core count."))
	if strings.EqualFold(*filter, "all") {
		fmt.Fprintln(os.Stdout, dim("PROV - = in the hardware index only (not currently joined to RunPod or GCP)."))
	}
	if yRank > 0 {
		fmt.Fprintln(os.Stdout, dim("STOCK yours = GPU on this machine (Apple FP16 is an estimate for ranking)."))
	}
	fmt.Fprintln(os.Stdout, dim("Set preference: runhug gpu set <name|pool>   Clear: runhug gpu clear"))
	return nil
}

func printGPUTableHeader() {
	fmt.Fprintf(os.Stdout, "  %s %s %s %s %s %s %s %s %s\n",
		dim(padRight("#", 4)),
		dim(padRight("NAME", 28)),
		dim(padRight("VRAM", 6)),
		dim(padRight("FP16", 7)),
		dim(padRight("TC", 5)),
		dim(padRight("PROV", 7)),
		dim(padRight("KEY", 12)),
		dim(padRight("$/HR", 6)),
		dim("STOCK"),
	)
}

func printGPUTable(rows []gpuRow) {
	printGPUTableHeader()
	printGPUTableRows(rows)
}

func printGPUTableRows(rows []gpuRow) {
	for i, r := range rows {
		stock := r.Stock
		stockOut := padRight(stock, 8)
		switch {
		case r.Yours || stock == "yours":
			stockOut = green(padRight("yours", 8))
		case r.Provider == "runpod" || r.Provider == "gcp":
			if r.InStock {
				stockOut = green(padRight(stock, 8))
			} else if stock != "" {
				stockOut = yellow(padRight(stock, 8))
			}
		}
		price := "-"
		if r.PricePerHr > 0 {
			price = fmt.Sprintf("%.2f", r.PricePerHr)
		}
		fp16 := "-"
		if r.FP16 > 0 {
			fp16 = fmt.Sprintf("%.0f", r.FP16)
		}
		tc := "-"
		if r.TensorCores > 0 {
			tc = strconv.Itoa(r.TensorCores)
		}
		prov := r.Provider
		if prov == "" {
			prov = "-"
		}
		key := r.Key
		if key == "" {
			key = "-"
		}
		name := r.Name
		if r.Yours {
			name = name + " *"
		}
		fmt.Fprintf(os.Stdout, "  %s %s %s %s %s %s %s %s %s\n",
			padRight(strconv.Itoa(i+1), 4),
			bold(padRight(truncateRunes(name, 28), 28)),
			padRight(fmt.Sprintf("%.0f", r.MemoryGB), 6),
			padRight(fp16, 7),
			padRight(tc, 5),
			padRight(prov, 7),
			padRight(truncateRunes(key, 12), 12),
			padRight(price, 6),
			stockOut,
		)
	}
}

func cmdGPUSet(args []string) error {
	if showCmdHelp(args, "runhug gpu", printGPUHelp, printGPUHelpFull) {
		return nil
	}
	fs := newFlagSet("gpu set")
	filter := fs.String("filter", "all", "all | local | amd | nvidia | runpod | gcp")
	sortKey := fs.String("sort", "best", "best | cheapest | value | vram | name")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	want := strings.TrimSpace(strings.Join(fs.Args(), " "))
	rows, err := buildGPURows(*filter, "")
	if err != nil {
		return err
	}
	sortGPURows(rows, *sortKey)
	if len(rows) == 0 {
		return fmt.Errorf("no GPUs for --filter %s", *filter)
	}

	var chosen gpuRow
	if want == "" {
		if !stdinIsTTY() {
			return fmt.Errorf("usage: runhug gpu set <NAME|POOL|L4|T4> [--filter …]")
		}
		// Reuse list display then prompt.
		_ = cmdGPUList([]string{"--filter", *filter, "--sort", *sortKey, "--limit", "40"})
		fmt.Fprint(os.Stderr, "Pick # or name: ")
		sc := bufio.NewScanner(os.Stdin)
		if !sc.Scan() {
			return fmt.Errorf("cancelled")
		}
		want = strings.TrimSpace(sc.Text())
		if want == "" {
			return fmt.Errorf("cancelled")
		}
		if n, err := strconv.Atoi(want); err == nil && n >= 1 && n <= len(rows) {
			chosen = rows[n-1]
		} else {
			chosen, err = resolveGPUPreference(*filter, want)
			if err != nil {
				return err
			}
		}
	} else if n, err := strconv.Atoi(want); err == nil && n >= 1 && n <= len(rows) {
		chosen = rows[n-1]
	} else {
		chosen, err = resolveGPUPreference(*filter, want)
		if err != nil {
			return err
		}
	}

	provider := chosen.Provider
	if provider == "" {
		// Index-only pick: infer provider from filter when possible.
		switch strings.ToLower(*filter) {
		case "local", "runpod", "gcp":
			provider = strings.ToLower(*filter)
		default:
			provider = "runpod" // deploy default; user can re-set with --filter
			if chosen.Key == "" {
				chosen.Key = chosen.Name
			}
		}
	}
	if chosen.Key == "" {
		chosen.Key = chosen.Name
	}
	pref := &config.GPUPreference{
		Provider: provider,
		Key:      chosen.Key,
		Name:     chosen.Name,
		MemoryGB: chosen.MemoryGB,
	}
	s := config.LoadSettings()
	s.GPU = pref
	if err := config.SaveSettings(s); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "%s  GPU preference %s / %s", green("✓"), bold(pref.Provider), bold(pref.Key))
	if pref.Name != "" && pref.Name != pref.Key {
		fmt.Fprintf(os.Stdout, " (%s)", pref.Name)
	}
	fmt.Fprintln(os.Stdout)
	if pref.MemoryGB > 0 {
		printKV(os.Stdout, "vram", fmt.Sprintf("%.0f GB", pref.MemoryGB))
	}
	return nil
}

func cmdGPUClear(args []string) error {
	_ = args
	s := config.LoadSettings()
	if s.GPU == nil {
		fmt.Fprintln(os.Stdout, dim("No GPU preference set."))
		return nil
	}
	s.GPU = nil
	if err := config.SaveSettings(s); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "%s  GPU preference cleared\n", green("✓"))
	return nil
}

func cmdGPUShow(args []string) error {
	_ = args
	pref := savedGPUPreference()
	if pref == nil {
		fmt.Fprintln(os.Stdout, dim("No GPU preference. Set one with `runhug gpu set`."))
		return nil
	}
	heading(os.Stdout, "GPU preference")
	printKV(os.Stdout, "provider", pref.Provider)
	printKV(os.Stdout, "key", pref.Key)
	if pref.Name != "" {
		printKV(os.Stdout, "name", pref.Name)
	}
	if pref.MemoryGB > 0 {
		printKV(os.Stdout, "vram", fmt.Sprintf("%.0f GB", pref.MemoryGB))
	}
	return nil
}
