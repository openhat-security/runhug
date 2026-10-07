package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/adamsiwiec1/runhug/internal/hf"
)

type hubView struct {
	Query      string
	Models     []hf.Model
	Pick       int
	Sort       string
	Limit      int
	Command    string
	SkipFooter bool
	RankSource string
	Queries    []string
	// WrapWidth > 0 wraps MODEL across lines at that rune width.
	// 0 = single-line ellipsis truncate (default column max 48).
	WrapWidth int
	Verbose   bool
	// OfferPick tells the footer to use row numbers (last search) instead of
	// always repeating inspect/deploy for row 1.
	OfferPick bool
}

type hubOpts struct {
	Sort            string
	Limit           int
	Command         string
	WrapWidth       int
	DisableSemantic bool
	Verbose         bool
}

func printHubResults(w io.Writer, v hubView) {
	if w == nil {
		w = os.Stdout
	}
	title := "Search"
	if strings.Contains(strings.ToLower(v.RankSource), "hub") {
		title = "Hugging Face"
	}
	if v.Query != "" {
		title = fmt.Sprintf("%s  %s", title, v.Query)
	}
	heading(w, fmt.Sprintf("%s  (%d)", title, len(v.Models)))
	if v.Verbose {
		if v.Sort != "" || v.Limit > 0 {
			fmt.Fprintf(w, "%s  %s", dim("sorted by"), sortLabel(v.Sort))
			if v.Limit > 0 {
				fmt.Fprintf(w, "   %s %d", dim("--limit"), v.Limit)
			}
			fmt.Fprintln(w)
		}
		if v.RankSource != "" {
			fmt.Fprintf(w, "%s  %s\n", dim("source"), v.RankSource)
		}
		if v.Sort != "" || v.Limit > 0 || v.RankSource != "" {
			fmt.Fprintln(w)
		}
	}
	if len(v.Models) == 0 {
		if strings.Contains(strings.ToLower(v.RankSource), "hub") {
			fmt.Fprintln(w, yellow("No models on the Hub matched."))
		} else {
			fmt.Fprintln(w, yellow("No models in the local index matched."))
		}
		fmt.Fprintln(w)
		commands(w, "Try a broader query, or refresh the index:",
			`runhug update`,
			`runhug search instruct --sort likes --limit 20`,
			`runhug search --online -q "qwen" --limit 20`,
		)
		return
	}

	ids := make([]string, len(v.Models))
	likes := make([]string, len(v.Models))
	dls := make([]string, len(v.Models))
	engines := make([]string, len(v.Models))
	licenses := make([]string, len(v.Models))
	formats := make([]hf.Format, len(v.Models))
	for i, m := range v.Models {
		ids[i] = m.RepoID()
		likes[i] = formatCount(int64(m.Likes))
		dls[i] = formatCount(m.Downloads)
		f := hf.DetectFormat(m)
		formats[i] = f
		engines[i] = string(f.Engine)
		licenses[i] = dash(m.License())
	}

	wrapW := clampWrapWidth(v.WrapWidth)
	var idW int
	if wrapW > 0 {
		idW = wrapW
		if idW < utf8.RuneCountInString("MODEL") {
			idW = utf8.RuneCountInString("MODEL")
		}
	} else {
		idW = colWidth("MODEL", ids, 48)
	}
	likeW := colWidth("LIKES", likes, 0)
	dlW := colWidth("DOWNLOADS", dls, 0)
	engW := colWidth("ENGINE", engines, 0)
	licW := colWidth("LICENSE", licenses, 16)
	actW := colWidth("ACTIONS", []string{"🔗 📋"}, 0)

	fmt.Fprintf(w, "  %s  %s  %s  %s  %s  %s  %s\n",
		dim(padRight("#", 2)),
		dim(padRight("MODEL", idW)),
		dim(padRight("LIKES", likeW)),
		dim(padRight("DOWNLOADS", dlW)),
		dim(padRight("ENGINE", engW)),
		dim(padRight("LICENSE", licW)),
		dim(padRight("ACTIONS", actW)),
	)

	// Indent before MODEL column: "  " + "#" pad(2) + "  "
	modelIndent := 2 + 2 + 2

	for i := range v.Models {
		var modelLines []string
		if wrapW > 0 {
			modelLines = wrapModelLines(ids[i], idW)
		} else {
			modelLines = []string{truncateRunes(ids[i], idW)}
		}
		fmt.Fprintf(w, "  %s  %s  %s  %s  %s  %s  %s\n",
			padRight(fmt.Sprintf("%d", i+1), 2),
			bold(padRight(modelLines[0], idW)),
			dim(padRight(likes[i], likeW)),
			dim(padRight(dls[i], dlW)),
			engineTag(formats[i].Engine, engW),
			dim(padRight(truncateRunes(licenses[i], licW), licW)),
			actionsCell(ids[i]),
		)
		for _, cont := range modelLines[1:] {
			fmt.Fprintf(w, "%s%s\n", strings.Repeat(" ", modelIndent), bold(cont))
		}
	}
	fmt.Fprintln(w)
	if v.Verbose {
		fmt.Fprintln(w, dim("ACTIONS  🔗 opens Hub  ·  📋 / copy N copies model id  ·  or search --copy N"))
		fmt.Fprintln(w)
	}

	if v.SkipFooter {
		return
	}
	if v.OfferPick {
		n := len(v.Models)
		open := []string{
			fmt.Sprintf("1–%d          inspect that row", n),
			"deploy N      RunPod/GCP for row N",
			"copy N        clipboard the model id",
		}
		if v.Command != "" {
			open = append(open, v.Command+" --sort likes --limit 20")
		}
		commands(w, "Next:", open...)
		return
	}
	shown := 1
	if v.Pick > 0 {
		shown = v.Pick
	}
	if shown < 1 || shown > len(v.Models) {
		shown = 1
	}
	id := v.Models[shown-1].RepoID()
	open := []string{
		hubLink(id),
		"runhug inspect " + id,
		"runhug deploy " + id,
	}
	if v.Command != "" {
		open = append(open, v.Command+" --sort likes --limit 20")
	}
	commands(w, "Next:", open...)
}

// wrapModelLines splits s into chunks of at most width runes. width must be > 0.
func wrapModelLines(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	r := []rune(s)
	if len(r) == 0 {
		return []string{""}
	}
	if len(r) <= width {
		return []string{s}
	}
	lines := make([]string, 0, (len(r)+width-1)/width)
	for len(r) > 0 {
		if len(r) <= width {
			lines = append(lines, string(r))
			break
		}
		lines = append(lines, string(r[:width]))
		r = r[width:]
	}
	return lines
}

func engineTag(engine hf.Engine, width int) string {
	s := padRight(string(engine), width)
	switch engine {
	case hf.EngineGGUF:
		return yellow(s)
	case hf.EngineVLLM:
		return green(s)
	default:
		return dim(s)
	}
}

func colWidth(header string, vals []string, max int) int {
	w := utf8.RuneCountInString(header)
	for _, v := range vals {
		n := utf8.RuneCountInString(v)
		if n > w {
			w = n
		}
	}
	if max > 0 && w > max {
		return max
	}
	return w
}

func sortLabel(sortKey string) string {
	switch strings.ToLower(strings.TrimSpace(sortKey)) {
	case "downloads":
		return "downloads"
	case "likes":
		return "likes"
	case "relevance", "relevant", "rank", "":
		return "relevance"
	default:
		return sortKey
	}
}

func clampLimit(n int) int {
	if n < 1 {
		return 1
	}
	if n > 100 {
		return 100
	}
	return n
}

func sortHubModels(models []hf.Model, key string) {
	hf.SortModels(models, key)
}

func quotedSearchCmd(query string) string {
	return quotedCmd("search", query)
}

func quotedCmd(name, query string) string {
	if query == "" {
		return "runhug " + name
	}
	if strings.ContainsAny(query, " \t\"'") {
		return fmt.Sprintf("runhug %s %q", name, query)
	}
	return "runhug " + name + " " + query
}
