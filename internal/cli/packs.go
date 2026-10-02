package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/index"
	"github.com/adamsiwiec1/runhug/internal/packs"
)

// promptPackCategories asks which category packs to install.
// Returns selected category ids. With yes=true and empty previous, installs all.
func promptPackCategories(yes bool) ([]string, error) {
	cats := packs.DefaultCategories()
	fmt.Fprintln(os.Stdout, bold("Index category packs"))
	fmt.Fprintln(os.Stdout, dim("Download curated SQLite packs from openhat-security/hfpacks Releases (merged into models.db)."))
	fmt.Fprintln(os.Stdout, dim("Override source with RUNHUG_PACKS_REPO=owner/name if needed."))
	fmt.Fprintln(os.Stdout)
	for i, c := range cats {
		extra := c.Pipeline
		if c.Filter != "" {
			extra = "filter=" + c.Filter
		}
		fmt.Fprintf(os.Stdout, "  %s  %s  %s\n", cyan(fmt.Sprintf("%d)", i+1)), bold(c.Title), dim("("+c.ID+"; "+extra+")"))
	}
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, dim("Enter numbers (e.g. 1,2,5), ranges (1-3), 'all', or 'none'."))

	if yes {
		ids := packs.CategoryIDs()
		fmt.Fprintf(os.Stdout, "%s  Installing all categories (--yes)\n\n", green("✓"))
		return ids, nil
	}
	if !canPrompt() {
		return nil, fmt.Errorf("non-interactive: pass --yes to install all packs, or --skip-index")
	}
	line, err := readLine("Categories [all]: ")
	if err != nil {
		return nil, err
	}
	ids, err := parseCategorySelection(line, cats)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		fmt.Fprintln(os.Stdout, dim("No packs selected — search will use bundled/local index only."))
		fmt.Fprintln(os.Stdout)
	}
	return ids, nil
}

func parseCategorySelection(line string, cats []packs.Category) ([]string, error) {
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "" || line == "all" || line == "*" {
		return packs.CategoryIDs(), nil
	}
	if line == "none" || line == "skip" || line == "n" {
		return nil, nil
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
			if err1 != nil || err2 != nil || start < 1 || end < start || end > len(cats) {
				return nil, fmt.Errorf("invalid range %q (use 1-%d)", p, len(cats))
			}
			for i := start; i <= end; i++ {
				id := cats[i-1].ID
				if !seen[id] {
					seen[id] = true
					out = append(out, id)
				}
			}
			continue
		}
		// id by name
		if c, ok := packs.LookupCategory(p); ok {
			if !seen[c.ID] {
				seen[c.ID] = true
				out = append(out, c.ID)
			}
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > len(cats) {
			return nil, fmt.Errorf("invalid selection %q (use 1-%d, id, all, or none)", p, len(cats))
		}
		id := cats[n-1].ID
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

// installPackCategories downloads selected packs from the latest release,
// verifies sha256, stores under packs/, and merges into models.db.
func installPackCategories(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	rc := packs.NewReleaseClient(packs.ReleaseRepo())
	if tok := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); tok != "" {
		rc.Token = tok
	}

	fmt.Fprintf(os.Stderr, "%s Fetching pack manifest from github.com/%s …\n", bold("⚡"), rc.Repo)
	manifest, tag, err := rc.FetchManifest(ctx)
	if err != nil {
		return fmt.Errorf("fetch pack manifest: %w\nHint: publish index packs on openhat-security/hfpacks Releases (see hfpacks CI)", err)
	}
	fmt.Fprintf(os.Stderr, "%s Release %s (%d packs in manifest)\n", green("✓"), tag, len(manifest.Packs))

	idxPath := indexFilePath()
	// Seed from bundled if no local models.db yet
	if !index.Exists(idxPath) {
		if bundled := bundledIndexPath(); bundled != "" {
			if err := copyFile(bundled, idxPath); err != nil {
				fmt.Fprintf(os.Stderr, "%s  could not seed from bundled: %v\n", yellow("⚠"), err)
			}
		}
	}
	idx, err := index.Open(idxPath)
	if err != nil {
		return err
	}
	defer idx.Close()

	instPath, err := packs.InstalledPath()
	if err != nil {
		return err
	}
	inst, err := packs.LoadInstalled(instPath)
	if err != nil {
		return err
	}

	for _, id := range ids {
		info, ok := manifest.FindPack(id)
		if !ok {
			fmt.Fprintf(os.Stderr, "%s  pack %q not in manifest — skip\n", yellow("⚠"), id)
			continue
		}
		packPath, err := packs.PackDBPath(id)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "📥 Downloading %s (%s)…\n", info.Title, info.DBFilename)
		if err := rc.DownloadPack(ctx, info, packPath); err != nil {
			return fmt.Errorf("download %s: %w", id, err)
		}
		n, wm, err := packs.MergePackDBWithID(idx, packPath, id)
		if err != nil {
			return fmt.Errorf("merge %s: %w", id, err)
		}
		if wm == "" {
			wm = info.Watermark
		}
		_ = idx.SetMetadata(packs.MetadataKeyWatermark(id), wm)
		_ = idx.SetMetadata(packs.MetadataKeyInstalled(id), "1")
		cat, _ := packs.LookupCategory(id)
		title := info.Title
		if title == "" {
			title = cat.Title
		}
		inst.Categories[id] = packs.InstalledPack{
			ID:            id,
			Title:         title,
			Watermark:     wm,
			InstalledAt:   time.Now().UTC().Format(time.RFC3339),
			SourceRelease: tag,
			SHA256:        info.SHA256,
			Rows:          info.Rows,
		}
		fmt.Fprintf(os.Stderr, "%s Merged %s (%d rows, watermark %s)\n", green("✓"), id, n, dash(wm))
	}

	_ = idx.SetMetadata("last_update", time.Now().UTC().Format(time.RFC3339))
	if err := packs.SaveInstalled(instPath, inst); err != nil {
		return err
	}
	count, _ := idx.Count()
	fmt.Fprintf(os.Stderr, "%s Search index ready: %s models → %s\n\n", green("✓"), bold(fmt.Sprintf("%d", count)), idxPath)
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// updateInstalledPacks refreshes installed categories from hfpacks Releases
// (optional delta asset, else full pack replace). No Hub crawl.
func updateInstalledPacks(ctx context.Context, forcePacks bool) error {
	instPath, err := packs.InstalledPath()
	if err != nil {
		return err
	}
	inst, err := packs.LoadInstalled(instPath)
	if err != nil {
		return err
	}
	ids := inst.SelectedIDs()
	if len(ids) == 0 {
		return fmt.Errorf("no packs installed — run: runhug packs install")
	}

	idxPath := indexFilePath()
	idx, err := index.Open(idxPath)
	if err != nil {
		return err
	}
	defer idx.Close()

	rc := packs.NewReleaseClient(packs.ReleaseRepo())
	if tok := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); tok != "" {
		rc.Token = tok
	}

	manifest, tag, err := rc.FetchManifest(ctx)
	if err != nil {
		return fmt.Errorf("fetch pack manifest: %w", err)
	}

	total := 0
	skipped := 0
	for _, id := range ids {
		n, upToDate, err := updateOneCategory(ctx, idx, inst, rc, manifest, tag, id, forcePacks)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s  %s: %v\n", yellow("⚠"), id, err)
			continue
		}
		if upToDate {
			skipped++
			continue
		}
		total += n
	}
	if total > 0 {
		_ = idx.SetMetadata("last_update", time.Now().UTC().Format(time.RFC3339))
	}
	if err := packs.SaveInstalled(instPath, inst); err != nil {
		return err
	}
	if total == 0 && skipped == len(ids) {
		fmt.Fprintf(os.Stderr, "%s All %d packs up to date (%s)\n", green("✓"), len(ids), tag)
		return nil
	}
	fmt.Fprintf(os.Stderr, "%s Updated %s model rows across %d categories",
		green("✓"), bold(fmt.Sprintf("%d", total)), len(ids)-skipped)
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, " (%d already current)", skipped)
	}
	fmt.Fprintln(os.Stderr)
	return nil
}

func updateOneCategory(
	ctx context.Context,
	idx *index.Index,
	inst *packs.Installed,
	rc *packs.ReleaseClient,
	manifest *packs.Manifest,
	tag, id string,
	forcePacks bool,
) (n int, upToDate bool, err error) {
	info, ok := manifest.FindPack(id)
	if !ok {
		return 0, false, fmt.Errorf("pack %q missing from release %s", id, tag)
	}
	entry := inst.Categories[id]
	if !forcePacks && installedPackCurrent(entry, tag, info) {
		fmt.Fprintf(os.Stderr, "%s %s: up to date (%s)\n", green("✓"), id, tag)
		return 0, true, nil
	}

	if forcePacks {
		n, err = refreshPackFromRelease(ctx, idx, inst, rc, tag, id, info, true)
		return n, false, err
	}

	deltaPath := filepath.Join(os.TempDir(), "runhug-"+id+"-delta.jsonl")
	okDelta, err := rc.TryDownloadDelta(ctx, id, deltaPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s  delta check %s: %v\n", dim("·"), id, err)
	} else if okDelta {
		defer os.Remove(deltaPath)
		n, newWM, err := packs.ApplyDeltaJSONL(idx, deltaPath)
		if err != nil {
			return 0, false, err
		}
		if newWM != "" {
			entry.Watermark = newWM
		}
		entry.SourceRelease = tag
		entry.SHA256 = info.SHA256
		entry.Rows = info.Rows
		inst.Categories[id] = entry
		_ = idx.SetMetadata(packs.MetadataKeyWatermark(id), entry.Watermark)
		fmt.Fprintf(os.Stderr, "%s %s: applied delta (%d rows upserted)\n", green("✓"), id, n)
		return n, false, nil
	}

	n, err = refreshPackFromRelease(ctx, idx, inst, rc, tag, id, info, false)
	return n, false, err
}

func installedPackCurrent(entry packs.InstalledPack, releaseTag string, info packs.PackInfo) bool {
	if releaseTag == "" || info.SHA256 == "" {
		return false
	}
	if entry.SourceRelease != releaseTag {
		return false
	}
	return strings.EqualFold(entry.SHA256, info.SHA256)
}

func refreshPackFromRelease(
	ctx context.Context,
	idx *index.Index,
	inst *packs.Installed,
	rc *packs.ReleaseClient,
	tag, id string,
	info packs.PackInfo,
	forceDownload bool,
) (int, error) {
	packPath, err := packs.PackDBPath(id)
	if err != nil {
		return 0, err
	}
	needDownload := forceDownload
	if !needDownload && info.SHA256 != "" {
		if err := packs.VerifySHA256(packPath, info.SHA256); err != nil {
			needDownload = true
		}
	} else if !forceDownload {
		if _, err := os.Stat(packPath); err != nil {
			needDownload = true
		}
	}
	if needDownload {
		fmt.Fprintf(os.Stderr, "📥 Downloading %s (%s)…\n", id, tag)
		if err := rc.DownloadPack(ctx, info, packPath); err != nil {
			return 0, err
		}
	} else {
		fmt.Fprintf(os.Stderr, "%s Merging %s from local cache…\n", dim("·"), id)
	}
	n, wm, err := packs.MergePackDBWithID(idx, packPath, id)
	if err != nil {
		return 0, err
	}
	if wm == "" {
		wm = info.Watermark
	}
	entry := inst.Categories[id]
	entry.Watermark = wm
	entry.SourceRelease = tag
	entry.SHA256 = info.SHA256
	entry.Rows = info.Rows
	inst.Categories[id] = entry
	_ = idx.SetMetadata(packs.MetadataKeyWatermark(id), wm)
	fmt.Fprintf(os.Stderr, "%s %s: upserted %d rows into search index\n", green("✓"), id, n)
	return n, nil
}
