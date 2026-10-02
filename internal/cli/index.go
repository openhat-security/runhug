package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/index"
)

func cmdIndexInfo(args []string) error {
	indexPath := indexFilePath()
	bundledPath := bundledIndexPath()

	hasLocal := index.Exists(indexPath)
	hasBundled := bundledPath != "" && index.Exists(bundledPath)

	if !hasLocal && !hasBundled {
		fmt.Fprintf(os.Stderr, "%s  No index found\n", yellow("⚠"))
		fmt.Fprintf(os.Stderr, "   Run: %s to install packs from hfpacks Releases\n", cyan("runhug packs install"))
		return nil
	}

	// Prefer the user-local index. Bundled is only for first-run seed / when
	// the user has not created their own yet.
	path := indexPath
	title := "Search Index"
	if !hasLocal {
		path = bundledPath
		title = "Bundled Search Index"
	}

	idx, err := index.Open(path)
	if err != nil {
		return fmt.Errorf("open index: %w", err)
	}
	defer idx.Close()

	count, err := idx.Count()
	if err != nil {
		return fmt.Errorf("count models: %w", err)
	}

	lastUpdate, err := idx.LastUpdate()
	if err != nil {
		return fmt.Errorf("get last update: %w", err)
	}

	createdAt, _ := idx.GetMetadata("created_at")
	fileInfo, _ := os.Stat(path)
	sizeKB := fileInfo.Size() / 1024

	heading(os.Stdout, title)
	printKV(os.Stdout, "path", path)
	printKV(os.Stdout, "size", fmt.Sprintf("%d KB", sizeKB))
	printKV(os.Stdout, "models", bold(fmt.Sprintf("%d", count))+" models")
	if createdAt != "" {
		created, _ := time.Parse(time.RFC3339, createdAt)
		printKV(os.Stdout, "created", formatTime(created))
	}
	printKV(os.Stdout, "updated", formatDuration(time.Since(lastUpdate))+" ago")
	fmt.Fprintln(os.Stdout)

	if err := printIndexBreakdowns(os.Stdout, idx); err != nil {
		return err
	}

	if !hasLocal {
		fmt.Fprintf(os.Stdout, "%s  Using bundled index (ships with package; seeds your first local index)\n", dim("ℹ"))
		fmt.Fprintf(os.Stdout, "   Run %s for latest models\n", cyan("runhug packs install"))
		fmt.Fprintln(os.Stdout)
	} else if time.Since(lastUpdate).Hours() > 24*7 {
		fmt.Fprintf(os.Stdout, "%s  Index is over a week old\n", yellow("⚠"))
		fmt.Fprintf(os.Stdout, "   Run: %s\n", cyan("runhug update --packs"))
		fmt.Fprintln(os.Stdout)
	}

	commands(os.Stdout, "Commands:",
		"runhug search -q \"...\"",
		"runhug packs install",
		"runhug update --packs",
	)

	return nil
}

func printIndexBreakdowns(w io.Writer, idx *index.Index) error {
	sections := []struct {
		title     string
		fetch     func() ([]index.CountRow, error)
		limit     int
		skipEmpty bool
	}{
		{"By library", func() ([]index.CountRow, error) { return idx.CountBy("library_name") }, 12, false},
		{"By pipeline / type", func() ([]index.CountRow, error) { return idx.CountBy("pipeline_tag") }, 12, false},
		{"By license", func() ([]index.CountRow, error) { return idx.CountBy("license") }, 10, false},
		{"By pack category", func() ([]index.CountRow, error) { return idx.MembershipCounts() }, 12, true},
	}
	for _, s := range sections {
		rows, err := s.fetch()
		if err != nil {
			return err
		}
		if s.skipEmpty && len(rows) == 0 {
			continue
		}
		fmt.Fprintln(w, bold(s.title))
		if len(rows) == 0 {
			fmt.Fprintln(w, dim("  (none)"))
			fmt.Fprintln(w)
			continue
		}
		printCountRows(w, rows, s.limit)
		fmt.Fprintln(w)
	}
	return nil
}

func printCountRows(w io.Writer, rows []index.CountRow, limit int) {
	shown := rows
	var rest int
	if limit > 0 && len(rows) > limit {
		shown = rows[:limit]
		for _, r := range rows[limit:] {
			rest += r.Count
		}
	}
	labelW := 18
	for _, r := range shown {
		if n := len(r.Label); n > labelW && n <= 28 {
			labelW = n
		}
	}
	for _, r := range shown {
		label := r.Label
		if len(label) > 28 {
			label = truncateRunes(label, 28)
		}
		fmt.Fprintf(w, "  %s  %s\n", dim(padRight(label, labelW)), bold(fmt.Sprintf("%d", r.Count)))
	}
	if rest > 0 {
		fmt.Fprintf(w, "  %s  %s\n", dim(padRight("…", labelW)), dim(fmt.Sprintf("+%d in %d more", rest, len(rows)-limit)))
	}
}

func indexFilePath() string {
	dir, err := config.Dir()
	if err != nil {
		dir = filepath.Join(os.TempDir(), "runhug")
	}
	_ = config.MigrateFileIfMissing(dir, "models.db")
	return filepath.Join(dir, "models.db")
}

func bundledIndexPath() string {
	// 1. Relative to executable (production: bin/runhug -> ../data/models.db)
	exePath, err := os.Executable()
	if err == nil {
		bundled := filepath.Join(filepath.Dir(exePath), "..", "data", "models.db")
		absPath, _ := filepath.Abs(bundled)
		if _, err := os.Stat(absPath); err == nil {
			return absPath
		}
	}

	// 2. Working directory (development: run from repo root)
	if wd, err := os.Getwd(); err == nil {
		bundled := filepath.Join(wd, "data", "models.db")
		if _, err := os.Stat(bundled); err == nil {
			return bundled
		}
	}

	// 3. Executable's directory (if data is alongside bin/)
	if exePath, err := os.Executable(); err == nil {
		bundled := filepath.Join(filepath.Dir(exePath), "data", "models.db")
		if _, err := os.Stat(bundled); err == nil {
			return bundled
		}
	}

	return ""
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", mins)
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

func formatTime(t time.Time) string {
	return t.Format("Jan 2, 2006 3:04 PM")
}
