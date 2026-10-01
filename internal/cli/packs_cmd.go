package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/hf"
	"github.com/adamsiwiec1/runhug/internal/index"
	"github.com/adamsiwiec1/runhug/internal/packs"
)

func cmdPacks(args []string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		printPacksHelp(os.Stdout)
		return nil
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "categories", "types":
		return cmdPacksCategories(rest)
	case "build":
		return cmdPacksBuild(rest)
	case "upsert":
		return cmdPacksUpsert(rest)
	case "index":
		return cmdPacksIndex(rest)
	case "help", "-h", "--help":
		printPacksHelp(os.Stdout)
		return nil
	default:
		printPacksHelp(os.Stderr)
		return fmt.Errorf("unknown packs command %q", sub)
	}
}

func printPacksHelp(w io.Writer) {
	helpUsage(w, "runhug packs <command>")
	fmt.Fprintln(w, dim("Build / upsert Hub index packs (hfpacks workflow embedded in runhug)"))
	fmt.Fprintln(w)

	helpSection(w, "commands")
	helpCmd(w, "categories", "list --type / pack ids (release + Hub tags)")
	helpCmd(w, "build", "crawl Hub → index-*.db + manifest")
	helpCmd(w, "upsert <org/model>…", "fetch named models into a pack / models.db")
	helpCmd(w, "index --from-search <q>", "search Hub/local and upsert the pool")
	fmt.Fprintln(w)

	fmt.Fprintf(w, "%s %s\n", dim("example:"), cyan(`runhug search aero --type llm --online --index`))
	fmt.Fprintf(w, "%s %s\n", dim("example:"), cyan(`runhug packs upsert Qwen/Qwen3-8B --type llm`))
}

func cmdPacksCategories(args []string) error {
	fs := newFlagSet("packs-categories")
	releaseOnly := fs.Bool("release", false, "only release/install packs")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *releaseOnly {
		for _, c := range packs.DefaultCategories() {
			extra := c.Pipeline
			if c.Filter != "" {
				extra = "filter=" + c.Filter
			}
			if len(c.ExtraPipelines) > 0 {
				extra += "+" + strings.Join(c.ExtraPipelines, ",")
			}
			fmt.Printf("%s\t%s\t%s\n", c.ID, c.Title, extra)
		}
		return nil
	}
	for _, t := range packs.AllTypes() {
		kind := "hub"
		if t.ReleasePack {
			kind = "release"
		}
		pipes := strings.Join(t.Pipelines, ",")
		if t.Filter != "" {
			pipes = "filter=" + t.Filter
		}
		alias := ""
		if len(t.Aliases) > 0 {
			alias = " aliases=" + strings.Join(t.Aliases, ",")
		}
		fmt.Printf("%s\t%s\t%s\t%s%s\n", t.ID, t.Title, kind, pipes, alias)
	}
	return nil
}

func cmdPacksBuild(args []string) error {
	fs := newFlagSet("packs-build")
	out := fs.String("out", "dist/index", "output directory")
	cats := fs.String("categories", "", "comma-separated pack ids (default: release packs)")
	limit := fs.Int("limit", 0, "max rows per category (0 = unlimited)")
	minLikes := fs.Int("min-likes", packs.DefaultMinLikes, "quality floor")
	minDownloads := fs.Int("min-downloads", packs.DefaultMinDownloads, "quality floor")
	sleepMs := fs.Int("sleep-ms", 250, "pause between Hub pages")
	full := fs.Bool("full", true, "Hub expand fields")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	var catIDs []string
	if s := strings.TrimSpace(*cats); s != "" {
		for _, p := range strings.Split(s, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				catIDs = append(catIDs, packs.CanonicalTypeID(p))
			}
		}
	}

	token := config.Load().HFToken
	client := hf.New(token)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 12*time.Hour)
	defer cancel()

	fmt.Fprintf(os.Stderr, "[+] packs build → %s\n", *out)
	man, err := packs.Build(ctx, client, packs.BuildOpts{
		OutDir:       *out,
		Limit:        *limit,
		Categories:   catIDs,
		Sleep:        time.Duration(*sleepMs) * time.Millisecond,
		Full:         *full,
		SourceRepo:   packs.ReleaseRepo(),
		MinLikes:     *minLikes,
		MinDownloads: *minDownloads,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s %d packs in %s\n", green("✓"), len(man.Packs), *out)
	for _, p := range man.Packs {
		fmt.Fprintf(os.Stderr, "    %s  rows=%d  %s\n", p.ID, p.Rows, p.DBFilename)
	}
	return nil
}

func cmdPacksUpsert(args []string) error {
	fs := newFlagSet("packs-upsert")
	typeFlag := fs.String("type", "text-generation", "pack/type id for membership")
	dbPath := fs.String("db", "", "SQLite path (default: ~/.config/runhug/models.db)")
	share := fs.String("share", "", "open community PR after upsert (true/false; omit to prompt)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	ids := fs.Args()
	if len(ids) == 0 {
		return fmt.Errorf("usage: runhug packs upsert <org/model>… [--type llm]")
	}
	packID := packs.CanonicalTypeID(*typeFlag)
	if packID == "" {
		return fmt.Errorf("unknown --type %q", *typeFlag)
	}
	path := strings.TrimSpace(*dbPath)
	if path == "" {
		path = indexFilePath()
	}

	client := hf.New(config.Load().HFToken)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	var models []hf.Model
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		m, err := client.Get(ctx, id)
		if err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
		models = append(models, *m)
		fmt.Fprintf(os.Stderr, "  + %s\n", m.RepoID())
	}
	idx, err := index.Open(path)
	if err != nil {
		return err
	}
	n, _, err := packs.UpsertModelsWithPack(idx, models, packID)
	idx.Close()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s Upserted %d → %s (type=%s)\n", green("✓"), n, path, packID)

	packPath, err := packs.PackDBPath(packID)
	if err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Dir(packPath), 0755)
	pidx, err := index.Open(packPath)
	if err != nil {
		return err
	}
	_, _, _ = packs.UpsertModelsWithPack(pidx, models, packID)
	pidx.Close()

	want, err := resolveShare(*share)
	if err != nil {
		return err
	}
	if want {
		return sharePackContribution(packID, packPath, strings.Join(ids, " "), n)
	}
	return nil
}

func cmdPacksIndex(args []string) error {
	fs := newFlagSet("packs-index")
	query := fs.String("from-search", "", "search query")
	typeFlag := fs.String("type", "", "pack/type id (required for membership)")
	online := fs.Bool("online", true, "search live Hub (default true for packs index)")
	limit := fs.Int("limit", 50, "pool size (1-100)")
	share := fs.String("share", "", "community PR after index (true/false; omit to prompt)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	q := strings.TrimSpace(*query)
	if q == "" {
		q = strings.Join(fs.Args(), " ")
	}
	if q == "" {
		return fmt.Errorf("usage: runhug packs index --from-search <query> --type llm")
	}
	if strings.TrimSpace(*typeFlag) == "" {
		return fmt.Errorf("--type is required for packs index")
	}
	req := searchRequest{
		Query:  q,
		Type:   *typeFlag,
		Task:   "any",
		Sort:   "relevance",
		Limit:  clampLimit(*limit),
		Online: *online,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	models, meta, err := searchModels(ctx, req)
	if err != nil {
		return err
	}
	pool := meta.Pool
	if len(pool) == 0 {
		pool = models
	}
	printHubResults(os.Stdout, hubView{
		Query: q, Models: models, Sort: "relevance", Limit: req.Limit,
		Command: quotedCmd("packs", "index"), RankSource: meta.RankSource,
	})
	return indexSearchPool(pool, meta.PackID, q, *share)
}
