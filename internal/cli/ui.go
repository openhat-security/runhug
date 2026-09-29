package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/adamsiwiec1/runhug/internal/find"
)

func newTab(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
}

func printFoundTable(w io.Writer, hits []find.Found) {
	tw := newTab(w)
	fmt.Fprintln(tw, "  #\tKIND\tMODEL\tNOTE")
	for i, h := range hits {
		name := h.Name
		if name == "" {
			name = h.Path
		}
		fmt.Fprintf(tw, "  %d\t%s\t%s\t%s\n", i+1, h.Kind, bold(name), foundNote(h))
	}
	_ = tw.Flush()
	fmt.Fprintln(w)
}

func foundNote(h find.Found) string {
	var bits []string
	if isEmbedding(h.Name) || isEmbedding(h.Path) {
		bits = append(bits, "embedding")
	}
	if s := formatSize(h.Size); s != "" {
		bits = append(bits, s)
	}
	return strings.Join(bits, ", ")
}

func isEmbedding(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "embed") || strings.Contains(n, "nomic")
}

func examplePick(hits []find.Found) int {
	for i, h := range hits {
		if !isEmbedding(h.Name) && !isEmbedding(h.Path) {
			return i + 1
		}
	}
	if len(hits) == 0 {
		return 1
	}
	return 1
}

func exampleName(hits []find.Found) string {
	i := examplePick(hits) - 1
	if i < 0 || i >= len(hits) {
		return "qwen3"
	}
	return hubQueryFromFound(hits[i])
}

func hubQueryFromFound(h find.Found) string {
	name := h.Name
	if name == "" {
		name = h.Path
	}
	return hubQueryFromName(name)
}

func hubQueryFromName(name string) string {
	name = strings.TrimSpace(name)
	if k := strings.LastIndexByte(name, '/'); k >= 0 {
		name = name[k+1:]
	}
	if i := strings.IndexByte(name, ':'); i > 0 {
		name = name[:i]
	}
	name = strings.TrimSuffix(name, ".gguf")
	name = strings.TrimSuffix(name, ".GGUF")
	if name == "" {
		return "instruct"
	}
	return name
}

func printAddHelp(w io.Writer, hits []find.Found) {
	n := examplePick(hits)
	name := exampleName(hits)
	commands(w, "Search Hugging Face for one of these:",
		fmt.Sprintf("runhug local add --pick %d --limit 20", n),
		fmt.Sprintf("runhug search %s --sort likes --limit 20", name),
		`runhug search "instruct coder" --filter gguf --limit 20`,
	)
}

func printReady(mName, runtimeName, url string) {
	w := os.Stdout
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s  %s\n", green("Registered"), bold(mName))
	if runtimeName != "" {
		printKV(w, "runtime", runtimeName)
	}
	if url != "" {
		printKV(w, "openai", cyan(url))
	}
	fmt.Fprintln(w)
	q := mName
	if i := strings.IndexByte(q, ':'); i > 0 {
		q = q[:i]
	}
	next := []string{
		fmt.Sprintf("runhug search %s --sort likes", q),
		"runhug inspect <org/model>",
		"runhug connect",
		"runhug deploy <org/model>",
		"runhug proxy",
	}
	commands(w, "Next:", next...)
}
