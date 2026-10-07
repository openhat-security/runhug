package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/adamsiwiec1/runhug/internal/hf"
)

func TestWizardAliasesRegistered(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, cmd := range []string{"wizard", "guide", "guided", "setup"} {
		cmd := cmd
		t.Run(cmd, func(t *testing.T) {
			done := make(chan error, 1)
			go func() {
				devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
				if err != nil {
					done <- err
					return
				}
				defer devNull.Close()
				stdout := os.Stdout
				os.Stdout = devNull
				defer func() { os.Stdout = stdout }()
				done <- Run([]string{cmd, "--yes"})
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("%s --yes: %v", cmd, err)
				}
			case <-time.After(3 * time.Second):
				t.Fatalf("%s --yes hung (expected non-interactive checklist)", cmd)
			}
		})
	}
}

func TestWizardChecklistContent(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	if err := wizardChecklist(&buf); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	for _, want := range []string{
		"Wizard",
		"Search index",
		"HF token",
		"Runpod",
		"Advisor",
		"Find a model",
		"GPU",
		"dry-run",
		"Live deploy",
		"Proxy",
		"Never auto-creates a live Runpod endpoint",
		"runhug packs install",
		"runhug deploy <org/model> --dry-run",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("checklist missing %q\n%s", want, s)
		}
	}
}

func TestWizardYesDoesNotReadStdin(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	oldIn := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = oldIn
		_ = r.Close()
	}()

	done := make(chan error, 1)
	go func() {
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			done <- err
			return
		}
		defer devNull.Close()
		stdout := os.Stdout
		os.Stdout = devNull
		defer func() { os.Stdout = stdout }()
		done <- cmdWizard([]string{"--yes"})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cmdWizard --yes hung with EOF stdin")
	}
}

func TestUsageMentionsWizard(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	printUsage(&buf)
	s := buf.String()
	if !strings.Contains(s, "wizard") {
		t.Fatalf("usage missing wizard\n%s", s)
	}
	// Aliases (guide/guided/setup) stay off root help; still registered in Run().
	for _, leak := range []string{"(guide", "guided, setup", "aliases:"} {
		if strings.Contains(s, leak) {
			t.Fatalf("root help must not dump wizard aliases (%q)\n%s", leak, s)
		}
	}
}

func TestWizardShortlistUsesLikesLexicalRawQuery(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	stubNoHub(t)
	path := writeTestIndex(t, []hf.Model{
		{ID: "qwen/Qwen2.5-7B-Instruct", Likes: 9000, Downloads: 500000, Description: "general instruct chat", Tags: []string{"text-generation"}},
		{ID: "mistralai/Mistral-7B-Instruct-v0.3", Likes: 8000, Downloads: 400000, Description: "instruct", Tags: []string{"text-generation"}},
		{ID: "empero-ai/Qwythos", Likes: 120, Downloads: 8000, Description: "hacking empero cyber pentest", Tags: []string{"text-generation", "hacking"}},
		{ID: "lab/hacking-empero-agent", Likes: 40, Downloads: 900, Description: "hacking empero virus analysis", Tags: []string{"text-generation"}},
		{ID: "acme/unrelated-vision", Likes: 50, Downloads: 1000, Description: "image classification", Tags: []string{"image-classification"}},
	})
	withIndexPaths(t, path, "")

	var buf bytes.Buffer
	ids, err := wizardShortlist(&buf, "hacking empero")
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "likes") {
		t.Fatalf("expected likes rank source in output:\n%s", out)
	}
	if strings.Contains(out, "score=") {
		t.Fatalf("wizard shortlist must not print semantic/recommend score:\n%s", out)
	}
	if !strings.Contains(out, "♥") || !strings.Contains(out, "↓") {
		t.Fatalf("expected likes/downloads markers:\n%s", out)
	}
	joined := strings.Join(ids, ",")
	if !strings.Contains(joined, "empero-ai/Qwythos") && !strings.Contains(joined, "lab/hacking-empero-agent") {
		t.Fatalf("raw query should surface cyber models, got %v\n%s", ids, out)
	}
	// Popular instruct models must not dominate a "hacking empero" shortlist.
	for _, bad := range []string{"qwen/Qwen2.5-7B-Instruct", "mistralai/Mistral-7B-Instruct-v0.3"} {
		for _, id := range ids {
			if id == bad {
				t.Fatalf("instruct default leaked into shortlist: %v\n%s", ids, out)
			}
		}
	}
	// Likes order among lexical hits: empero (120) before lab (40).
	ei, li := -1, -1
	for i, id := range ids {
		if id == "empero-ai/Qwythos" {
			ei = i
		}
		if id == "lab/hacking-empero-agent" {
			li = i
		}
	}
	if ei < 0 || li < 0 || ei > li {
		t.Fatalf("expected likes order empero before lab: %v", ids)
	}
}

func TestWizardShortlistDoesNotRewriteToInstruct(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	stubNoHub(t)
	// Only models that match "instruct" would win if ParseIntent rewrote the query.
	path := writeTestIndex(t, []hf.Model{
		{ID: "big/instruct-chat", Likes: 9999, Downloads: 1_000_000, Description: "general purpose instruct", Tags: []string{"text-generation"}},
		{ID: "niche/hacking-virus-scanner", Likes: 11, Downloads: 200, Description: "hacking virus malware reverse", Tags: []string{"text-generation"}},
	})
	withIndexPaths(t, path, "")

	var buf bytes.Buffer
	ids, err := wizardShortlist(&buf, "hacking virus")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) == 0 || ids[0] != "niche/hacking-virus-scanner" {
		t.Fatalf("want hacking-virus first from raw query, got %v\n%s", ids, buf.String())
	}
	if strings.Contains(strings.Join(ids, ","), "big/instruct-chat") {
		t.Fatalf("instruct rewrite must not pull unrelated instruct model: %v", ids)
	}
}

func TestDeployArgsIncludesGPU(t *testing.T) {
	got := deployArgs("org/model", "AMPERE_48", "--dry-run")
	want := []string{"org/model", "--dry-run", "--gpu", "AMPERE_48"}
	if len(got) != len(want) {
		t.Fatalf("got %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %#v want %#v", got, want)
		}
	}
	got = deployArgs("org/model", "", "--yes")
	if len(got) != 2 || got[1] != "--yes" {
		t.Fatalf("empty gpu should omit --gpu: %#v", got)
	}
}

func TestWizardChecklistMentionsGPUPick(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	if err := wizardChecklist(&buf); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.Contains(s, "--gpu") {
		t.Fatalf("checklist should mention deploy --gpu\n%s", s)
	}
}
