package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/packs"
)

func cmdUpdate(args []string) error {
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "self", "cli":
			return cmdUpgrade(args[1:])
		case "index", "packs":
			args = append([]string{"--packs"}, args[1:]...)
		}
	}

	fs := newFlagSet("update")
	cliFlag := fs.Bool("cli", false, "upgrade this CLI via the detected install method (brew/scoop/npm/apt/…)")
	force := fs.Bool("force", false, "re-download category packs from Releases (full replace)")
	packsFlag := fs.Bool("packs", false, "refresh installed packs (delta when available; skip if already current)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *cliFlag {
		return upgradeCLI(false, false)
	}

	heading(os.Stdout, "Update search index")
	fmt.Fprintln(os.Stdout, dim("Install/refresh category packs from github.com/"+packs.ReleaseRepo()+" Releases."))
	fmt.Fprintln(os.Stdout)

	instPath, _ := packs.InstalledPath()
	inst, _ := packs.LoadInstalled(instPath)
	hasPacks := inst != nil && len(inst.SelectedIDs()) > 0

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	if *force || *packsFlag {
		if !hasPacks {
			fmt.Fprintln(os.Stdout, dim("No packs installed yet — prompting for categories."))
			ids, err := promptPackCategories(false)
			if err != nil {
				return err
			}
			return installPackCategories(ctx, ids)
		}
		return updateInstalledPacks(ctx, *force)
	}

	if hasPacks {
		fmt.Fprintln(os.Stdout, dim("Refreshing installed category packs from Releases…"))
		fmt.Fprintln(os.Stdout)
		return updateInstalledPacks(ctx, false)
	}

	fmt.Fprintln(os.Stdout, dim("No packs installed — install from hfpacks Releases."))
	ids, err := promptPackCategories(false)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		fmt.Fprintln(os.Stdout, dim("Nothing to do. Search will use the bundled index until you install packs."))
		return nil
	}
	return installPackCategories(ctx, ids)
}
