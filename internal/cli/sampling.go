package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/adamsiwiec1/runhug/internal/hf"
	"github.com/adamsiwiec1/runhug/internal/hparams"
)

func loadRecommendSampling(ctx context.Context, client *hf.Client, model hf.Model, format hf.Format) hparams.Sampling {
	var gen map[string]any
	if client != nil {
		g, err := client.GenerationConfig(ctx, model.RepoID())
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s generation_config.json: %v (using family table)\n", dim("note:"), err)
		} else {
			gen = g
		}
	}
	return hparams.Recommend(model, format, gen)
}

// chooseSampling is the deploy-time final step: none / recommended / customize.
// --yes and non-TTY default to recommended. --dry-run prints the block and returns rec
// unless mode is none.
func chooseSampling(rec hparams.Sampling, mode string, sets []string, yes, dry, jsonOut, verbose bool) (*hparams.Sampling, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "recommended"
	}
	custom, err := hparams.Parse(sets)
	if err != nil {
		return nil, err
	}
	if !custom.Empty() {
		rec = hparams.Merge(rec, custom)
		rec.Source = "custom"
		mode = "customize"
	}

	fmt.Fprintln(os.Stdout)
	planHeading(os.Stdout, "Sampling")
	printKV(os.Stdout, "recommended", highlightUSD(rec.FormatBlock()))
	if verbose || (!dry && !jsonOut && canPrompt() && !yes) {
		fmt.Fprintln(os.Stdout, dim("  R recommended   s none (engine defaults)   c customize key=value"))
	}

	if jsonOut || dry {
		if mode == "none" || mode == "skip" {
			return nil, nil
		}
		out := rec
		return &out, nil
	}

	if yes || !canPrompt() {
		if mode == "none" || mode == "skip" {
			return nil, nil
		}
		out := rec
		return &out, nil
	}

	if mode == "none" || mode == "skip" {
		return nil, nil
	}
	if mode == "customize" && !custom.Empty() {
		out := rec
		return &out, nil
	}

	line, err := readLine("Sampling [R/s/c]: ")
	if err != nil {
		out := rec
		return &out, nil
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "s", "skip", "none", "n":
		fmt.Fprintln(os.Stdout, dim("using engine defaults"))
		return nil, nil
	case "c", "custom", "customize":
		fmt.Fprintln(os.Stdout, dim("enter KEY=VALUE (empty line to finish). keys: temp top_p top_k min_p rep max_tokens"))
		var pairs []string
		for {
			p, err := readLine("  set> ")
			if err != nil || strings.TrimSpace(p) == "" {
				break
			}
			pairs = append(pairs, p)
		}
		if len(pairs) == 0 {
			out := rec
			return &out, nil
		}
		parsed, err := hparams.Parse(pairs)
		if err != nil {
			return nil, err
		}
		out := hparams.Merge(rec, parsed)
		out.Source = "custom"
		printKV(os.Stdout, "using", out.FormatBlock())
		return &out, nil
	default:
		out := rec
		printKV(os.Stdout, "using", out.FormatBlock())
		return &out, nil
	}
}
