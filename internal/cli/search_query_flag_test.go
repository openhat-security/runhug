package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/adamsiwiec1/runhug/internal/hf"
)

func TestResolveSearchQueryPrefersFlag(t *testing.T) {
	if got := resolveSearchQuery("cybersec", "positional"); got != "cybersec" {
		t.Fatalf("flag wins: %q", got)
	}
	if got := resolveSearchQuery("", "positional"); got != "positional" {
		t.Fatalf("positional: %q", got)
	}
	if got := resolveSearchQuery("  x  ", " y "); got != "x" {
		t.Fatalf("trim flag: %q", got)
	}
	if got := resolveSearchQuery("", ""); got != "" {
		t.Fatalf("empty %q", got)
	}
}

func TestSearchQueryFlagQ(t *testing.T) {
	fs := newFlagSet("search")
	var q string
	fs.StringVar(&q, "query", "", "")
	fs.StringVar(&q, "q", "", "")
	if err := parseFlags(fs, []string{"-q", "cybersec"}); err != nil {
		t.Fatal(err)
	}
	if q != "cybersec" {
		t.Fatalf("-q %q", q)
	}
	if fs.NArg() != 0 {
		t.Fatalf("args %v", fs.Args())
	}

	fs = newFlagSet("search")
	fs.StringVar(&q, "query", "", "")
	fs.StringVar(&q, "q", "", "")
	q = ""
	if err := parseFlags(fs, []string{"positional", "--query", "from-flag"}); err != nil {
		t.Fatal(err)
	}
	if q != "from-flag" {
		t.Fatalf("--query %q", q)
	}
	got := resolveSearchQuery(q, strings.Join(fs.Args(), " "))
	if got != "from-flag" {
		t.Fatalf("resolve %q", got)
	}
}

func TestSearchHelpMentionsQueryAndDescriptions(t *testing.T) {
	var buf bytes.Buffer
	fs := newFlagSet("search")
	fs.SetOutput(&buf)
	registerSearchFlags(fs)
	fs.PrintDefaults()
	s := buf.String()
	for _, want := range []string{"-q", "-query", "description", "positional", "semantic", "no-semantic", "keyword", "wrap", "word-wrap", "-ww", "-online", "-hub", "local", "HF_TOKEN", "rate-limited", "RUNHUG_OLLAMA_EMBED"} {
		if !strings.Contains(s, want) {
			t.Fatalf("search flags missing %q\n%s", want, s)
		}
	}

	buf.Reset()
	printSearchHelp(&buf)
	help := buf.String()
	for _, want := range []string{"usage:", "runhug search", "--query", "--online", "--verbose", "example:"} {
		if !strings.Contains(help, want) {
			t.Fatalf("search help missing %q\n%s", want, help)
		}
	}
}

func TestQuotedSearchCmd(t *testing.T) {
	if got := quotedSearchCmd("qwen"); got != "runhug search qwen" {
		t.Fatalf("%s", got)
	}
	if got := quotedSearchCmd("instruct coder"); !strings.Contains(got, `search "instruct coder"`) {
		t.Fatalf("%s", got)
	}
}

func TestPrintHubResultsNextUsesFullID(t *testing.T) {
	var buf bytes.Buffer
	long := "org-with-a-very-long-name/model-with-an-extremely-long-identifier-that-exceeds-forty-eight"
	printHubResults(&buf, hubView{
		Models: []hf.Model{{ID: long, Likes: 1, Downloads: 1, Tags: []string{"safetensors"}}},
		Sort:   "relevance",
		Limit:  5,
	})
	s := buf.String()
	if !strings.Contains(s, "inspect "+long) || !strings.Contains(s, "deploy "+long) {
		t.Fatalf("footer should contain full repo id\n%s", s)
	}
	if !strings.Contains(s, "https://huggingface.co/"+long) {
		t.Fatalf("footer should contain full Hub URL\n%s", s)
	}
	if !strings.Contains(s, truncateRunes(long, 48)) {
		t.Fatalf("default MODEL should ellipsize\n%s", s)
	}
}

func TestPrintHubResultsWrapsOnlyModel(t *testing.T) {
	long := "org-with-a-very-long-name/model-with-an-extremely-long-identifier-that-exceeds-forty-eight"
	trunc := truncateRunes(long, 48)
	m := []hf.Model{{ID: long, Likes: 12, Downloads: 1200, Tags: []string{"safetensors", "apache-2.0"}}}
	var buf bytes.Buffer
	printHubResults(&buf, hubView{Models: m, Sort: "relevance", Limit: 5, WrapWidth: 28})
	s := buf.String()
	if strings.Contains(s, trunc) {
		t.Fatalf("wrap should not ellipsize MODEL\n%s", s)
	}
	parts := wrapModelLines(long, 28)
	if len(parts) < 2 {
		t.Fatalf("fixture should wrap: %#v", parts)
	}
	for _, part := range parts {
		if !strings.Contains(s, part) {
			t.Fatalf("missing wrap chunk %q\n%s", part, s)
		}
	}
	if !strings.Contains(s, "ACTIONS") || !strings.Contains(s, "🔗") || !strings.Contains(s, "📋") {
		t.Fatalf("ACTIONS column missing\n%s", s)
	}
	if !strings.Contains(s, "inspect "+long) || !strings.Contains(s, "deploy "+long) {
		t.Fatalf("Next: should stay full with wrap\n%s", s)
	}
	lines := strings.Split(s, "\n")
	var dataLines []string
	for _, line := range lines {
		if strings.Contains(line, parts[0]) || strings.Contains(line, parts[1]) {
			dataLines = append(dataLines, line)
		}
	}
	if len(dataLines) < 2 {
		t.Fatalf("expected continuation line\n%s", s)
	}
	if !strings.Contains(dataLines[0], "12") {
		t.Fatalf("likes on first line\n%s", dataLines[0])
	}
	if strings.Contains(dataLines[1], "1.2K") {
		t.Fatalf("continuation must not repeat other columns\n%s", dataLines[1])
	}
}

func TestPrintHubResultsHasActionsColumn(t *testing.T) {
	var buf bytes.Buffer
	printHubResults(&buf, hubView{
		Models:  []hf.Model{{ID: "org/short", Likes: 1, Downloads: 1, Tags: []string{"safetensors"}}},
		Sort:    "relevance",
		Limit:   5,
		Verbose: true,
	})
	s := buf.String()
	if !strings.Contains(s, "ACTIONS") {
		t.Fatalf("header missing ACTIONS\n%s", s)
	}
	if !strings.Contains(s, "🔗") || !strings.Contains(s, "📋") {
		t.Fatalf("row missing action icons\n%s", s)
	}
	if !strings.Contains(s, "search --copy N") {
		t.Fatalf("footer hint missing\n%s", s)
	}
}

func TestWrapFlagsResolveWidth(t *testing.T) {
	cases := []struct {
		args []string
		want int
	}{
		{[]string{"--wrap", "28"}, 28},
		{[]string{"--word-wrap", "40"}, 40},
		{[]string{"-ww", "28"}, 28},
		{[]string{"--ww", "50"}, 50},
		{[]string{"--wrap", "20", "-ww", "50"}, 50},
		{nil, 0},
	}
	for _, tc := range cases {
		fs := newFlagSet("search")
		wrap, wordWrap, ww := addWrapFlags(fs)
		if err := parseFlags(fs, tc.args); err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		got := resolveWrapWidth(*wrap, *wordWrap, *ww)
		if got != tc.want {
			t.Fatalf("%v: got %d want %d", tc.args, got, tc.want)
		}
	}
}
