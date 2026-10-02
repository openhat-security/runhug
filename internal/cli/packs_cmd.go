package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/adamsiwiec1/runhug/internal/packs"
)

func cmdPacks(args []string) error {
	if len(args) == 0 {
		printPacksHelp(os.Stdout)
		return nil
	}
	if showCmdHelp(args, "runhug packs", printPacksHelp, printPacksHelpFull) {
		return nil
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "categories", "types":
		fmt.Fprintln(os.Stderr, dim("categories is deprecated — use:"), cyan("runhug packs list"))
		return cmdPacksList(rest)
	case "install":
		return cmdPacksInstall(rest)
	case "list", "status":
		return cmdPacksList(rest)
	case "update", "refresh", "sync":
		return cmdPacksUpdate(rest)
	case "remove", "rm", "uninstall":
		return cmdPacksRemove(rest)
	case "build", "upsert", "index", "hfpacks", "addon", "export":
		printPacksProducerHint(os.Stderr)
		return fmt.Errorf("%q moved to hfpacks — crawl/build lives in https://github.com/openhat-security/hfpacks", sub)
	default:
		printPacksHelp(os.Stderr)
		return fmt.Errorf("unknown packs command %q\nRun `runhug packs --help` or `runhug packs --help-full`", sub)
	}
}

func printPacksProducerHint(w io.Writer) {
	fmt.Fprintln(w, yellow("⚠")+"  "+dim("Pack crawling/building is owned by hfpacks (separate producer CLI)."))
	fmt.Fprintln(w)
	fmt.Fprintln(w, dim("Install packs here:"))
	fmt.Fprintln(w, "  "+cyan("runhug packs install"))
	fmt.Fprintln(w, "  "+cyan("runhug packs update"))
	fmt.Fprintln(w)
	fmt.Fprintln(w, dim("Build / export packs:"))
	fmt.Fprintln(w, "  "+cyan("https://github.com/openhat-security/hfpacks"))
	fmt.Fprintln(w, "  "+cyan("hfpacks build -out dist/index"))
	fmt.Fprintln(w, "  "+cyan("hfpacks export -out dist/index -format csv,parquet"))
}

func printPacksHelp(w io.Writer) {
	helpUsage(w, "runhug packs <command>")
	fmt.Fprintln(w, dim("Install category index packs from openhat-security/hfpacks Releases"))
	fmt.Fprintln(w)

	helpSection(w, "commands")
	helpCmd(w, "list", "installed + release + catalog (✓/✗); --local / --remote")
	helpCmd(w, "install [ids…]", "download packs (interactive if no ids)")
	helpCmd(w, "update", "refresh installed packs from latest release")
	helpCmd(w, "remove [ids…]", "uninstall packs (interactive if no ids)")
	fmt.Fprintln(w)

	fmt.Fprintf(w, "%s %s\n", dim("example:"), cyan(`runhug packs list`))
	fmt.Fprintf(w, "%s %s\n", dim("example:"), cyan(`runhug packs install`))
	fmt.Fprintf(w, "%s %s\n", dim("example:"), cyan(`runhug packs install llm gguf`))
	fmt.Fprintf(w, "%s %s\n", dim("produce packs:"), cyan("https://github.com/openhat-security/hfpacks"))
}

func cmdPacksUpdate(args []string) error {
	fs := newFlagSet("packs-update")
	force := fs.Bool("force", false, "full re-download even if a delta exists")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	return updateInstalledPacks(ctx, *force)
}

type packRow struct {
	Num        int
	Info       packs.PackInfo
	Installed  bool
	Local      packs.InstalledPack
	FromRemote bool
	OnRelease  bool
	Match      string
}

func cmdPacksList(args []string) error {
	fs := newFlagSet("packs-list")
	localOnly := fs.Bool("local", false, "show only installed packs")
	remoteOnly := fs.Bool("remote", false, "show only remote release packs")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *localOnly && *remoteOnly {
		return fmt.Errorf("use only one of --local or --remote")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	inst, err := loadInstalledPacks()
	if err != nil {
		return err
	}

	var manifest *packs.Manifest
	var tag string
	if !*localOnly {
		var ferr error
		manifest, tag, ferr = fetchPackManifest(ctx)
		if ferr != nil && *remoteOnly {
			return ferr
		}
		if ferr != nil {
			fmt.Fprintf(os.Stderr, "%s  remote: %v\n", yellow("⚠"), ferr)
			fmt.Fprintln(os.Stderr, dim("Showing local packs only. Fix network / publish hfpacks Releases."))
			*localOnly = true
		}
	}

	includeCatalog := !*localOnly && !*remoteOnly
	rows := buildPackRows(inst, manifest, *localOnly, *remoteOnly, includeCatalog)
	if len(rows) == 0 {
		if *localOnly {
			fmt.Fprintln(os.Stdout, dim("No packs installed. Run:"), cyan("runhug packs install"))
		} else {
			fmt.Fprintln(os.Stdout, dim("No packs found on remote or locally."))
			fmt.Fprintf(os.Stdout, "%s github.com/%s\n", dim("source:"), packs.ReleaseRepo())
		}
		return nil
	}

	heading(os.Stdout, "Index packs")
	printKV(os.Stdout, "source", "github.com/"+packs.ReleaseRepo())
	if tag != "" {
		printKV(os.Stdout, "release", tag)
	}
	installedN, releaseN, catalogN := packRowStats(rows)
	if !*localOnly && releaseN > 0 {
		pct := installedN * 100 / releaseN
		printKV(os.Stdout, "installed", fmt.Sprintf("%s / %s on release (%d%%)",
			bold(strconv.Itoa(installedN)), strconv.Itoa(releaseN), pct))
	} else if *localOnly {
		printKV(os.Stdout, "installed", bold(strconv.Itoa(installedN))+" packs")
	}
	if catalogN > 0 {
		printKV(os.Stdout, "catalog", fmt.Sprintf("%s not on latest release yet", bold(strconv.Itoa(catalogN))))
	}
	fmt.Fprintln(os.Stdout)

	printPackTable(os.Stdout, rows, includeCatalog)
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "%s  %s installed · %s not installed · REL %s on hfpacks release\n",
		dim("legend:"), green("✓"), red("✗"), green("✓"))
	fmt.Fprintf(os.Stdout, "%s  INDEXED = rows in pack · HUB≈ = Hub models at build · MATCH = Hub pipelines / filter\n", dim(""))
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "%s %s\n", dim("install:"), cyan("runhug packs install"))
	fmt.Fprintf(os.Stdout, "%s %s\n", dim("update:"), cyan("runhug packs update"))
	fmt.Fprintf(os.Stdout, "%s %s\n", dim("remove:"), cyan("runhug packs remove"))
	return nil
}

func packRowStats(rows []packRow) (installed, onRelease, catalogOnly int) {
	for _, r := range rows {
		if r.Installed {
			installed++
		}
		if r.OnRelease {
			onRelease++
		} else if !r.Installed {
			catalogOnly++
		}
	}
	return installed, onRelease, catalogOnly
}

func cmdPacksInstall(args []string) error {
	fs := newFlagSet("packs-install")
	yes := fs.Bool("yes", false, "install all remote packs without prompting")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	ids := normalizePackIDs(fs.Args())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	if len(ids) == 0 {
		var err error
		ids, err = promptInstallPacks(ctx, *yes)
		if err != nil {
			return err
		}
	}
	if len(ids) == 0 {
		fmt.Fprintln(os.Stdout, dim("No packs selected."))
		return nil
	}
	return installPackCategories(ctx, ids)
}

func cmdPacksRemove(args []string) error {
	fs := newFlagSet("packs-remove")
	yes := fs.Bool("yes", false, "remove all installed packs without prompting")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	ids := normalizePackIDs(fs.Args())
	inst, err := loadInstalledPacks()
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		ids, err = promptRemovePacks(inst, *yes)
		if err != nil {
			return err
		}
	}
	if len(ids) == 0 {
		fmt.Fprintln(os.Stdout, dim("No packs selected."))
		return nil
	}
	for _, id := range ids {
		path, err := packs.PackDBPath(id)
		if err != nil {
			return err
		}
		_ = os.Remove(path)
		delete(inst.Categories, id)
		fmt.Fprintf(os.Stderr, "%s removed %s\n", green("✓"), id)
	}
	instPath, err := packs.InstalledPath()
	if err != nil {
		return err
	}
	return packs.SaveInstalled(instPath, inst)
}

func loadInstalledPacks() (*packs.Installed, error) {
	instPath, err := packs.InstalledPath()
	if err != nil {
		return nil, err
	}
	return packs.LoadInstalled(instPath)
}

func fetchPackManifest(ctx context.Context) (*packs.Manifest, string, error) {
	rc := packs.NewReleaseClient(packs.ReleaseRepo())
	if tok := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); tok != "" {
		rc.Token = tok
	}
	return rc.FetchManifest(ctx)
}

func buildPackRows(inst *packs.Installed, man *packs.Manifest, localOnly, remoteOnly, includeCatalog bool) []packRow {
	var rows []packRow
	seen := map[string]bool{}

	add := func(info packs.PackInfo, fromRemote, onRelease bool) {
		if seen[info.ID] {
			return
		}
		seen[info.ID] = true
		local, ok := packs.InstalledPack{}, false
		if inst != nil {
			local, ok = inst.Categories[info.ID]
		}
		if localOnly && !ok {
			return
		}
		if remoteOnly && !onRelease {
			return
		}
		if info.Title == "" {
			if c, found := packs.LookupCategory(info.ID); found {
				info.Title = c.Title
			}
		}
		match := ""
		if c, found := packs.LookupCategory(info.ID); found {
			match = packs.MatchLabel(c)
		}
		rows = append(rows, packRow{
			Info:       info,
			Installed:  ok,
			Local:      local,
			FromRemote: fromRemote,
			OnRelease:  onRelease,
			Match:      match,
		})
	}

	if man != nil && !localOnly {
		for _, info := range man.Packs {
			add(info, true, true)
		}
	}

	if inst != nil && !remoteOnly {
		for _, id := range inst.SelectedIDs() {
			if seen[id] {
				continue
			}
			e := inst.Categories[id]
			info := packs.PackInfo{
				ID: id, Title: e.Title, Rows: e.Rows, SHA256: e.SHA256, Watermark: e.Watermark,
			}
			onRelease := false
			if man != nil {
				if p, ok := man.FindPack(id); ok {
					info = p
					onRelease = true
				}
			}
			add(info, man != nil, onRelease)
		}
	}

	if includeCatalog {
		for _, c := range packs.DefaultCategories() {
			if seen[c.ID] {
				continue
			}
			add(packs.PackInfo{ID: c.ID, Title: c.Title}, false, false)
		}
	}

	// --local with manifest: prefer remote metadata for installed packs
	if localOnly && inst != nil {
		rows = rows[:0]
		seen = map[string]bool{}
		for _, id := range inst.SelectedIDs() {
			e := inst.Categories[id]
			info := packs.PackInfo{
				ID: id, Title: e.Title, Rows: e.Rows, SHA256: e.SHA256, Watermark: e.Watermark,
			}
			onRelease := false
			if man != nil {
				if p, ok := man.FindPack(id); ok {
					info = p
					onRelease = true
				}
			}
			add(info, man != nil, onRelease)
		}
	}

	for i := range rows {
		rows[i].Num = i + 1
	}
	return rows
}

func printPackTable(w io.Writer, rows []packRow, showMatch bool) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if showMatch {
		fmt.Fprintln(tw, "  #\tSTAT\tREL\tID\tTITLE\tINDEXED\tHUB≈\t%\tREMAIN\tMATCH")
	} else {
		fmt.Fprintln(tw, "  #\tSTAT\tREL\tID\tTITLE\tINDEXED\tHUB≈\t%\tREMAIN")
	}
	for _, r := range rows {
		stat := red("✗")
		if r.Installed {
			stat = green("✓")
		}
		rel := dim("—")
		if r.OnRelease {
			rel = green("✓")
		}
		title := r.Info.Title
		if title == "" {
			title = r.Info.ID
		}
		indexed := formatPackCount(r.Info.Rows)
		if r.Installed && r.Local.Rows > 0 && r.Info.Rows == 0 {
			indexed = formatPackCount(r.Local.Rows)
		}
		if !r.OnRelease && !r.Installed {
			indexed = dim("—")
		}
		hub, pct, remain := packCoverage(r.Info)
		if !r.OnRelease && r.Info.HubTotal <= 0 {
			hub, pct, remain = dim("—"), dim("—"), dim("—")
		}
		match := dim(truncatePackMatch(r.Match, 52))
		if showMatch {
			fmt.Fprintf(tw, "  %d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				r.Num, stat, rel, bold(r.Info.ID), title, indexed, hub, pct, remain, match)
		} else {
			fmt.Fprintf(tw, "  %d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				r.Num, stat, rel, bold(r.Info.ID), title, indexed, hub, pct, remain)
		}
	}
	_ = tw.Flush()
}

func truncatePackMatch(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func packCoverage(info packs.PackInfo) (hub, pct, remain string) {
	if info.HubTotal <= 0 {
		return dim("—"), dim("—"), dim("—")
	}
	hub = formatPackCount(info.HubTotal)
	p := info.Rows * 100 / info.HubTotal
	if p > 100 {
		p = 100
	}
	pct = fmt.Sprintf("%d%%", p)
	rem := info.HubTotal - info.Rows
	if rem < 0 {
		rem = 0
	}
	remain = formatPackCount(rem)
	return hub, pct, remain
}

func formatPackCount(n int) string {
	if n < 0 {
		return "—"
	}
	if n < 1000 {
		return strconv.Itoa(n)
	}
	if n < 10_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	if n < 1_000_000 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
}

func promptInstallPacks(ctx context.Context, yes bool) ([]string, error) {
	inst, err := loadInstalledPacks()
	if err != nil {
		return nil, err
	}
	man, tag, err := fetchPackManifest(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch pack manifest: %w\nHint: publish packs on github.com/%s Releases", err, packs.ReleaseRepo())
	}
	rows := buildPackRows(inst, man, false, true, false)
	if len(rows) == 0 {
		return nil, fmt.Errorf("no packs in release %s", tag)
	}

	heading(os.Stdout, "Install index packs")
	printKV(os.Stdout, "source", "github.com/"+packs.ReleaseRepo())
	printKV(os.Stdout, "release", tag)
	fmt.Fprintln(os.Stdout)
	printPackTable(os.Stdout, rows, false)
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, dim("Enter numbers (e.g. 1,2,5), ranges (1-3), ids, 'all', 'missing', or 'none'."))

	if yes {
		ids := packRowIDs(rows, true)
		fmt.Fprintf(os.Stdout, "%s  Installing all %d packs (--yes)\n\n", green("✓"), len(ids))
		return ids, nil
	}
	if !canPrompt() {
		return nil, fmt.Errorf("non-interactive: pass pack ids or --yes")
	}
	line, err := readLine("Install [missing]: ")
	if err != nil {
		return nil, err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		line = "missing"
	}
	return selectPackRows(line, rows, true)
}

func promptRemovePacks(inst *packs.Installed, yes bool) ([]string, error) {
	ids := inst.SelectedIDs()
	if len(ids) == 0 {
		fmt.Fprintln(os.Stdout, dim("No packs installed."))
		return nil, nil
	}
	rows := buildPackRows(inst, nil, true, false, false)
	heading(os.Stdout, "Remove installed packs")
	fmt.Fprintln(os.Stdout)
	printPackTable(os.Stdout, rows, false)
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, dim("Enter numbers, ids, 'all', or 'none'."))

	if yes {
		fmt.Fprintf(os.Stdout, "%s  Removing all %d packs (--yes)\n\n", yellow("⚠"), len(ids))
		return ids, nil
	}
	if !canPrompt() {
		return nil, fmt.Errorf("non-interactive: pass pack ids or --yes")
	}
	line, err := readLine("Remove [none]: ")
	if err != nil {
		return nil, err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		line = "none"
	}
	return selectPackRows(line, rows, false)
}

func selectPackRows(line string, rows []packRow, installMode bool) ([]string, error) {
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "none" || line == "skip" || line == "n" {
		return nil, nil
	}
	if line == "all" || line == "*" {
		return packRowIDs(rows, installMode), nil
	}
	if installMode && (line == "missing" || line == "new" || line == "available") {
		var ids []string
		for _, r := range rows {
			if !r.Installed && r.OnRelease {
				ids = append(ids, r.Info.ID)
			}
		}
		return ids, nil
	}

	parts := strings.FieldsFunc(line, func(r rune) bool {
		return r == ',' || r == ' ' || r == ';'
	})
	seen := map[string]bool{}
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.Contains(p, "-") {
			a, b, ok := strings.Cut(p, "-")
			if !ok {
				continue
			}
			start, err1 := strconv.Atoi(strings.TrimSpace(a))
			end, err2 := strconv.Atoi(strings.TrimSpace(b))
			if err1 != nil || err2 != nil || start < 1 || end < start || end > len(rows) {
				return nil, fmt.Errorf("invalid range %q (use 1-%d)", p, len(rows))
			}
			for i := start; i <= end; i++ {
				id := rows[i-1].Info.ID
				if !seen[id] {
					seen[id] = true
					out = append(out, id)
				}
			}
			continue
		}
		if n, err := strconv.Atoi(p); err == nil {
			if n < 1 || n > len(rows) {
				return nil, fmt.Errorf("invalid selection %q (use 1-%d)", p, len(rows))
			}
			id := rows[n-1].Info.ID
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
			continue
		}
		id := packs.CanonicalTypeID(p)
		if id == "" {
			id = p
		}
		found := false
		for _, r := range rows {
			if r.Info.ID == id || strings.EqualFold(r.Info.ID, p) {
				if !seen[r.Info.ID] {
					seen[r.Info.ID] = true
					out = append(out, r.Info.ID)
				}
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown pack %q", p)
		}
	}
	return out, nil
}

func normalizePackIDs(raw []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range raw {
		id := packs.CanonicalTypeID(strings.TrimSpace(r))
		if id == "" {
			id = strings.TrimSpace(r)
		}
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func packRowIDs(rows []packRow, installMode bool) []string {
	var ids []string
	for _, r := range rows {
		if installMode && !r.OnRelease {
			continue
		}
		ids = append(ids, r.Info.ID)
	}
	return ids
}
