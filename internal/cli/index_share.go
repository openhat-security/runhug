package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/hf"
	"github.com/adamsiwiec1/runhug/internal/index"
	"github.com/adamsiwiec1/runhug/internal/packs"
)

// indexSearchPool upserts models into models.db (+ optional category pack DB)
// then optionally opens a community share PR.
func indexSearchPool(models []hf.Model, packID, query, shareFlag string) error {
	if len(models) == 0 {
		return fmt.Errorf("--index: no models to upsert")
	}
	if packID == "" {
		packID = "text-generation"
		fmt.Fprintf(os.Stderr, "%s  no --type set; indexing under %s\n", yellow("⚠"), packID)
	}

	idxPath := indexFilePath()
	idx, err := index.Open(idxPath)
	if err != nil {
		return err
	}
	defer idx.Close()

	n, _, err := packs.UpsertModelsWithPack(idx, models, packID)
	if err != nil {
		return err
	}
	_ = idx.SetMetadata("last_update", time.Now().UTC().Format(time.RFC3339))
	_ = idx.SetMetadata(packs.MetadataKeyInstalled(packID), "1")
	fmt.Fprintf(os.Stderr, "%s Indexed %d model(s) → %s (type=%s)\n", green("✓"), n, idxPath, packID)

	// Also maintain packs/<id>.db for contribution artifacts
	packPath, err := packs.PackDBPath(packID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(packPath), 0755); err != nil {
		return err
	}
	packIdx, err := index.Open(packPath)
	if err != nil {
		return err
	}
	if _, _, err := packs.UpsertModelsWithPack(packIdx, models, packID); err != nil {
		packIdx.Close()
		return err
	}
	_ = packIdx.SetMetadata("category", packID)
	_ = packIdx.SetMetadata("last_update", time.Now().UTC().Format(time.RFC3339))
	count, _ := packIdx.Count()
	packIdx.Close()
	fmt.Fprintf(os.Stderr, "%s Pack DB %s (%d rows)\n", green("✓"), packPath, count)

	wantShare, err := resolveShare(shareFlag)
	if err != nil {
		return err
	}
	if !wantShare {
		return nil
	}
	return sharePackContribution(packID, packPath, query, n)
}

func resolveShare(flag string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(flag)) {
	case "true", "yes", "1", "y":
		return true, nil
	case "false", "no", "0", "n":
		return false, nil
	case "":
		if !canPrompt() {
			fmt.Fprintln(os.Stderr, dim("Skipping community share (non-interactive; pass --share=true to force)."))
			return false, nil
		}
		return confirmPref("Share these models with the runhug community?", false), nil
	default:
		return false, fmt.Errorf("invalid --share %q (use true, false, or omit to prompt)", flag)
	}
}

// sharePackContribution writes contrib artifacts and opens a PR via gh.
// Dry-run when RUNHUG_SHARE_DRY_RUN=1 (prints planned steps only).
func sharePackContribution(packID, packPath, query string, rowCount int) error {
	dry := strings.TrimSpace(os.Getenv("RUNHUG_SHARE_DRY_RUN")) == "1"
	outDir, err := os.MkdirTemp("", "runhug-contrib-")
	if err != nil {
		return err
	}
	contribDir := filepath.Join(outDir, "contrib", "packs", packID)
	if err := os.MkdirAll(contribDir, 0755); err != nil {
		return err
	}
	destDB := filepath.Join(contribDir, packs.DBFilenameFor(packID))
	if err := copyFile(packPath, destDB); err != nil {
		return err
	}

	cat, _ := packs.CategoryFromType(packID)
	info := packs.PackInfo{
		ID:         packID,
		Title:      cat.Title,
		Pipeline:   cat.Pipeline,
		Filter:     cat.Filter,
		Rows:       rowCount,
		DBFilename: packs.DBFilenameFor(packID),
		Watermark:  time.Now().UTC().Format(time.RFC3339),
	}
	if sum, err := packs.FileSHA256(destDB); err == nil {
		info.SHA256 = sum
		st, _ := os.Stat(destDB)
		if st != nil {
			info.SizeBytes = st.Size()
		}
	}
	man := &packs.Manifest{
		Version:     packs.ManifestVersion,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		SourceRepo:  packs.ReleaseRepo(),
		Packs:       []packs.PackInfo{info},
	}
	manPath := filepath.Join(contribDir, packs.ManifestFilename)
	if err := packs.WriteManifest(manPath, man); err != nil {
		return err
	}

	metaPath := filepath.Join(contribDir, "contribution.json")
	meta := map[string]any{
		"pack_id":    packID,
		"query":      query,
		"rows":       rowCount,
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"mode":       "search-index-upsert",
	}
	b, _ := json.MarshalIndent(meta, "", "  ")
	_ = os.WriteFile(metaPath, b, 0644)

	branch := fmt.Sprintf("packs/index-%s-%s", packID, time.Now().UTC().Format("20060102-1504"))
	title := fmt.Sprintf("packs: contribute %s (%d models)", packID, rowCount)
	body := fmt.Sprintf(`## Summary
- Upserted **%d** models into pack **%s** from search query %q
- Mode: search-index upsert (deduped by Hub repo id)
- Artifacts under `+"`contrib/packs/%s/`"+` (DB + manifest)

## Test plan
- [ ] `+"`runhug search -q %q --type %s`"+`
- [ ] Spot-check `+"`runhug inspect`"+` on a few rows
- [ ] Maintainers: publish as Release assets for `+"`init` / `update --packs`"+`

Do not merge multi-hundred-MB DBs into main without a Release plan — prefer Release assets linked from this PR.
`, rowCount, packID, query, packID, query, packID)

	steps := []string{
		"gh repo fork " + packs.DefaultRepo + " --clone=false 2>/dev/null || true",
		"git clone --depth 1 https://github.com/" + packs.DefaultRepo + ".git work && cd work",
		"git checkout -b " + branch,
		"mkdir -p contrib/packs/" + packID + " && cp -R " + contribDir + "/. contrib/packs/" + packID + "/",
		"git add contrib/packs/" + packID,
		"git commit -m " + shellQuote(title),
		"git push -u origin HEAD",
		"gh pr create --repo " + packs.DefaultRepo + " --title " + shellQuote(title) + " --body " + shellQuote(body),
	}

	if dry {
		fmt.Fprintln(os.Stderr, bold("Share dry-run (RUNHUG_SHARE_DRY_RUN=1)"))
		fmt.Fprintf(os.Stderr, "Artifacts: %s\n", contribDir)
		for _, s := range steps {
			fmt.Fprintf(os.Stderr, "  · %s\n", s)
		}
		return nil
	}

	if _, err := exec.LookPath("gh"); err != nil {
		fmt.Fprintln(os.Stderr, yellow("gh not found — write artifacts only. Install GitHub CLI to open a PR."))
		fmt.Fprintf(os.Stderr, "Artifacts ready at: %s\n", contribDir)
		fmt.Fprintln(os.Stderr, dim("See README: Share packs with the community"))
		return fmt.Errorf("community share requires gh (artifacts at %s)", contribDir)
	}

	work := filepath.Join(outDir, "work")
	clone := exec.Command("gh", "repo", "clone", packs.DefaultRepo, work, "--", "--depth", "1")
	clone.Stdout = os.Stderr
	clone.Stderr = os.Stderr
	if err := clone.Run(); err != nil {
		// fallback: git clone
		clone = exec.Command("git", "clone", "--depth", "1", "https://github.com/"+packs.DefaultRepo+".git", work)
		clone.Stdout = os.Stderr
		clone.Stderr = os.Stderr
		if err := clone.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Artifacts ready at: %s\n", contribDir)
			return fmt.Errorf("clone %s: %w (artifacts at %s)", packs.DefaultRepo, err, contribDir)
		}
	}

	run := func(name string, args ...string) error {
		cmd := exec.Command(name, args...)
		cmd.Dir = work
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	if err := run("git", "checkout", "-b", branch); err != nil {
		return err
	}
	dest := filepath.Join(work, "contrib", "packs", packID)
	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}
	entries, _ := os.ReadDir(contribDir)
	for _, e := range entries {
		if err := copyFile(filepath.Join(contribDir, e.Name()), filepath.Join(dest, e.Name())); err != nil {
			return err
		}
	}
	if err := run("git", "add", filepath.Join("contrib", "packs", packID)); err != nil {
		return err
	}
	if err := run("git", "commit", "-m", title); err != nil {
		return err
	}
	if err := run("git", "push", "-u", "origin", "HEAD"); err != nil {
		fmt.Fprintf(os.Stderr, "%s Push failed — you may need to fork and set origin.\nArtifacts: %s\n", yellow("⚠"), contribDir)
		return err
	}
	pr := exec.Command("gh", "pr", "create", "--repo", packs.DefaultRepo, "--title", title, "--body", body)
	pr.Dir = work
	pr.Stdout = os.Stdout
	pr.Stderr = os.Stderr
	if err := pr.Run(); err != nil {
		return fmt.Errorf("gh pr create: %w", err)
	}
	fmt.Fprintf(os.Stderr, "%s Opened community pack PR\n", green("✓"))
	return nil
}
