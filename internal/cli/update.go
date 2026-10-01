package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/index"
	"github.com/adamsiwiec1/runhug/internal/packs"
)

func cmdUpdate(args []string) error {
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "self", "cli":
			return cmdUpgrade(args[1:])
		case "index":
			args = args[1:]
		case "packs":
			args = append([]string{"--packs"}, args[1:]...)
		}
	}

	fs := newFlagSet("update")
	cliFlag := fs.Bool("cli", false, "upgrade this CLI via the detected install method (brew/scoop/npm/apt/…)")
	force := fs.Bool("force", false, "rebuild the local index from scratch (Hub scrape)")
	packsFlag := fs.Bool("packs", false, "re-download category packs from latest GitHub Release (full replace)")
	hubOnly := fs.Bool("hub", false, "refresh from Hub API only (ignore pack releases)")
	limitFlag := fs.Int("limit", -1, "Hub delta upsert cap (0=unlimited; default 2000; flag > RUNHUG_UPDATE_LIMIT > settings)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	updateLimit := config.ResolveUpdateLimit(*limitFlag)
	if *cliFlag {
		return upgradeCLI(false, false)
	}

	heading(os.Stdout, "Update search index")
	fmt.Fprintln(os.Stdout, dim("Local SQLite index + optional category packs. Prefer deltas over full re-download."))
	fmt.Fprintln(os.Stdout)

	indexPath := indexFilePath()
	instPath, _ := packs.InstalledPath()
	inst, _ := packs.LoadInstalled(instPath)
	hasPacks := inst != nil && len(inst.SelectedIDs()) > 0

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	if *force {
		return cmdIndexSetup([]string{"--force"})
	}

	if *packsFlag {
		if !hasPacks {
			fmt.Fprintln(os.Stdout, dim("No packs installed yet — prompting for categories."))
			ids, err := promptPackCategories(false)
			if err != nil {
				return err
			}
			return installPackCategories(ctx, ids)
		}
		return updateInstalledPacks(ctx, true, updateLimit)
	}

	if hasPacks && !*hubOnly {
		fmt.Fprintln(os.Stdout, dim("Refreshing installed category packs (delta / Hub since watermark)…"))
		fmt.Fprintln(os.Stdout)
		return updateInstalledPacks(ctx, false, updateLimit)
	}

	if !index.Exists(indexPath) {
		// Offer pack install first when nothing local exists
		if canPrompt() {
			ok, err := confirmPrefErr("No local index — install category packs from GitHub Releases?", true)
			if err != nil {
				return err
			}
			if ok {
				ids, err := promptPackCategories(false)
				if err != nil {
					return err
				}
				if len(ids) > 0 {
					return installPackCategories(ctx, ids)
				}
			}
		}
		return cmdIndexSetup(nil)
	}
	return cmdIndexUpdate([]string{"--limit", fmt.Sprintf("%d", updateLimit)})
}
