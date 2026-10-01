package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/hf"
	"github.com/adamsiwiec1/runhug/internal/index"
	"github.com/adamsiwiec1/runhug/internal/packs"
)

// promptPackCategories asks which category packs to install.
// Returns selected category ids. With yes=true and empty previous, installs all.
func promptPackCategories(yes bool) ([]string, error) {
	cats := packs.DefaultCategories()
	fmt.Fprintln(os.Stdout, bold("Index category packs"))
	fmt.Fprintln(os.Stdout, dim("Download curated SQLite packs from GitHub Releases (merged into models.db)."))
	fmt.Fprintln(os.Stdout, dim("v1 packs are top-N / samples per category; structure supports growing."))
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
		return fmt.Errorf("fetch pack manifest: %w\nHint: publish index packs on a release, or run update without packs (Hub refresh)", err)
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

// updateInstalledPacks refreshes installed categories via delta asset, Hub
// incremental fetch, or full pack replace (--packs).
func updateInstalledPacks(ctx context.Context, forcePacks bool, updateLimit int) error {
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
		return fmt.Errorf("no packs installed — run: runhug init")
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
	hfClient := hf.New(config.Load().HFToken)

	var total int
	for _, id := range ids {
		n, err := updateOneCategory(ctx, idx, inst, rc, hfClient, id, forcePacks, updateLimit)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s  %s: %v\n", yellow("⚠"), id, err)
			continue
		}
		total += n
	}
	_ = idx.SetMetadata("last_update", time.Now().UTC().Format(time.RFC3339))
	if err := packs.SaveInstalled(instPath, inst); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s Updated %s model rows across %d categories\n",
		green("✓"), bold(fmt.Sprintf("%d", total)), len(ids))
	return nil
}

func updateOneCategory(
	ctx context.Context,
	idx *index.Index,
	inst *packs.Installed,
	rc *packs.ReleaseClient,
	hfClient *hf.Client,
	id string,
	forcePacks bool,
	updateLimit int,
) (int, error) {
	entry := inst.Categories[id]
	wmStr := entry.Watermark
	if wmStr == "" {
		wmStr, _ = idx.GetMetadata(packs.MetadataKeyWatermark(id))
	}

	if forcePacks {
		return replacePackFromRelease(ctx, idx, inst, rc, id)
	}

	// Prefer delta asset when present
	deltaPath := filepath.Join(os.TempDir(), "runhug-"+id+"-delta.jsonl")
	ok, err := rc.TryDownloadDelta(ctx, id, deltaPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s  delta check %s: %v\n", dim("·"), id, err)
	} else if ok {
		defer os.Remove(deltaPath)
		n, newWM, err := packs.ApplyDeltaJSONL(idx, deltaPath)
		if err != nil {
			return 0, err
		}
		if newWM != "" {
			entry.Watermark = newWM
		}
		inst.Categories[id] = entry
		_ = idx.SetMetadata(packs.MetadataKeyWatermark(id), entry.Watermark)
		fmt.Fprintf(os.Stderr, "%s %s: applied delta (%d rows)\n", green("✓"), id, n)
		return n, nil
	}

	// Incremental Hub fetch since watermark
	cat, ok := packs.LookupCategory(id)
	if !ok {
		return 0, fmt.Errorf("unknown category %q", id)
	}
	since := int64(0)
	if wmStr != "" {
		if t, err := time.Parse(time.RFC3339, wmStr); err == nil {
			since = t.Unix()
		}
	}
	fmt.Fprintf(os.Stderr, "📥 %s: Hub delta since %s…\n", id, dash(wmStr))

	pipelines := []string{}
	if cat.Pipeline != "" {
		pipelines = append(pipelines, cat.Pipeline)
	}
	pipelines = append(pipelines, cat.ExtraPipelines...)
	if len(pipelines) == 0 {
		pipelines = []string{""}
	}

	var models []hf.Model
	seen := map[string]bool{}
	for _, task := range pipelines {
		batch, err := hfClient.ListModels(ctx, hf.ListOpts{
			Task:      task,
			Filter:    cat.Filter,
			Sort:      "lastModified",
			Limit:     updateLimit,
			PageSize:  100,
			Sleep:     200 * time.Millisecond,
			SinceUnix: since,
			Full:      true,
		})
		if err != nil {
			return 0, err
		}
		for _, m := range batch {
			rid := m.RepoID()
			if rid == "" || seen[rid] {
				continue
			}
			seen[rid] = true
			models = append(models, m)
		}
	}
	models = filterHubDeltaModels(idx, models)
	n, maxLM, err := packs.UpsertModels(idx, models)
	if err != nil {
		return n, err
	}
	if !maxLM.IsZero() {
		entry.Watermark = maxLM.UTC().Format(time.RFC3339)
		_ = idx.SetMetadata(packs.MetadataKeyWatermark(id), entry.Watermark)
	}
	inst.Categories[id] = entry
	fmt.Fprintf(os.Stderr, "%s %s: upserted %d from Hub\n", green("✓"), id, n)
	return n, nil
}

// filterHubDeltaModels keeps all existing ids (metadata refresh) and only
// admits NEW models that meet MinLikes≥3 and MinDownloads≥100.
func filterHubDeltaModels(idx *index.Index, models []hf.Model) []hf.Model {
	const minLikes = 3
	const minDownloads int64 = 100
	out := make([]hf.Model, 0, len(models))
	for _, m := range models {
		rid := m.RepoID()
		if rid == "" {
			continue
		}
		exists, err := idx.HasModel(rid)
		if err == nil && exists {
			out = append(out, m)
			continue
		}
		if m.Likes < minLikes || m.Downloads < minDownloads {
			continue
		}
		out = append(out, m)
	}
	return out
}

func replacePackFromRelease(
	ctx context.Context,
	idx *index.Index,
	inst *packs.Installed,
	rc *packs.ReleaseClient,
	id string,
) (int, error) {
	manifest, tag, err := rc.FetchManifest(ctx)
	if err != nil {
		return 0, err
	}
	info, ok := manifest.FindPack(id)
	if !ok {
		return 0, fmt.Errorf("pack %q missing from release %s", id, tag)
	}
	packPath, err := packs.PackDBPath(id)
	if err != nil {
		return 0, err
	}
	fmt.Fprintf(os.Stderr, "📥 Replacing pack %s from %s…\n", id, tag)
	if err := rc.DownloadPack(ctx, info, packPath); err != nil {
		return 0, err
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
	fmt.Fprintf(os.Stderr, "%s %s: merged %d rows from release pack\n", green("✓"), id, n)
	return n, nil
}
