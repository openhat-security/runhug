package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/adamsiwiec1/runhug/internal/runpod"
)

func TestHereticSlug(t *testing.T) {
	cases := map[string]string{
		"Qwen/Qwen2.5-7B-Instruct": "qwen-qwen2.5-7b-instruct",
		"  Mistral-7B  ":           "mistral-7b",
		"/":                        "model",
		"a/b":                      "a-b",
	}
	for in, want := range cases {
		if got := hereticSlug(in); got != want {
			t.Errorf("hereticSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDashboardClientStatusAndLogs(t *testing.T) {
	status := hereticStatus{
		Model:         "Qwen/Qwen2.5-7B-Instruct",
		Status:        "training",
		TrialsDone:    2,
		TrialsTotal:   100,
		BestRefusals:  3,
		RefusalsTotal: 100,
		BestKL:        0.44,
		Done:          false,
	}
	ls := hereticLogs{Lines: []string{"a", "b"}, Offset: 2, Total: 2, Done: false}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "sekrit" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var v any
		switch r.URL.Path {
		case "/status.json":
			v = status
		case "/logs":
			v = ls
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(v)
	}))
	defer srv.Close()

	host := &dashboardClient{base: srv.URL, token: "sekrit"}
	got, err := host.fetchStatus()
	if err != nil {
		t.Fatalf("fetchStatus: %v", err)
	}
	if got.Model != status.Model || got.BestRefusals != 3 || got.TrialsDone != 2 {
		t.Errorf("fetchStatus mismatch: %+v", got)
	}
	lg, err := host.fetchLogs(1)
	if err != nil {
		t.Fatalf("fetchLogs: %v", err)
	}
	if len(lg.Lines) != 2 || lg.Offset != 2 {
		t.Errorf("fetchLogs mismatch: %+v", lg)
	}

	badHost := &dashboardClient{base: srv.URL, token: "nope"}
	if _, err := badHost.fetchStatus(); err == nil {
		t.Errorf("expected unauthorized error with bad token")
	}
}

func TestPrintHereticResultError(t *testing.T) {
	st := hereticStatus{Model: "Org/M", Error: "boom"}
	if err := printHereticResult(st); err == nil {
		t.Fatal("expected error when status.Error is set")
	}
}

func TestPrintHereticResultSuccess(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	st := hereticStatus{
		Model:         "Org/M",
		TrialsDone:    20,
		TrialsTotal:   20,
		BestRefusals:  7,
		RefusalsTotal: 100,
		BestKL:        0.0019,
		ElapsedSec:    125,
	}
	if err := printHereticResult(st); err != nil {
		t.Fatalf("printHereticResult: %v", err)
	}
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	s := string(out)
	for _, want := range []string{"heretic done", "Org/M", "20/20", "7/100", "0.0019", "2m05s"} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q:\n%s", want, s)
		}
	}
}

func TestPrintHereticResultUploadShown(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()

	st := hereticStatus{Model: "Org/M", UploadRepoID: "adam/heretic-M", RefusalsTotal: 100}
	if err := printHereticResult(st); err != nil {
		t.Fatalf("printHereticResult: %v", err)
	}
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if !strings.Contains(string(out), "adam/heretic-M") {
		t.Errorf("expected upload repo in output")
	}
}

func TestPrintHereticResultRefusalsDefault(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()

	st := hereticStatus{Model: "Org/M"} // RefusalsTotal 0 → shown as 100
	if err := printHereticResult(st); err != nil {
		t.Fatalf("printHereticResult: %v", err)
	}
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if !bytes.Contains(out, []byte("0/100")) {
		t.Errorf("expected fallback 0/100 refusals, got:\n%s", out)
	}
}

func TestPoolGPUName(t *testing.T) {
	if got := poolGPUName(nil); got != "" {
		t.Errorf("poolGPUName(nil) = %q, want empty", got)
	}
	got := poolGPUName(&runpod.PodGPU{ID: "AMPERE_24"})
	if got != "AMPERE_24" {
		t.Errorf("poolGPUName = %q, want AMPERE_24", got)
	}
}

func TestDateTimeFormat(t *testing.T) {
	got := dateTime()
	if _, err := regexp.MatchString(`^\d{2}:\d{2}:\d{2}$`, got); err != nil {
		t.Fatalf("regexp: %v", err)
	}
	if len(got) != 8 {
		t.Errorf("dateTime() = %q, want HH:MM:SS", got)
	}
}
func TestHereticWizardChecklist(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	if err := hereticWizardChecklist(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"Heretic wizard",
		"runhug connect",
		"runhug connect hf",
		"heretic make",
		"--dry-run",
		"heretic wizard",
		"Never auto-creates",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("checklist missing %q\n%s", want, out)
		}
	}
}

func TestHereticWizardCommandYes(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if err := Run([]string{"heretic", "wizard", "--yes"}); err != nil {
		t.Fatal(err)
	}
}

func TestHereticHelpMentionsWizard(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	printHereticHelp(&buf)
	if !strings.Contains(buf.String(), "wizard") {
		t.Fatalf("heretic --help should list wizard\n%s", buf.String())
	}
	var full bytes.Buffer
	printHereticHelpFull(&full)
	if !strings.Contains(full.String(), "Interactive walkthrough") {
		t.Fatalf("heretic --help-full should explain wizard\n%s", full.String())
	}
}

func TestHereticMakeArgs(t *testing.T) {
	opts := hereticWizardOpts{Trials: 50, NoUpload: true, Cloud: "COMMUNITY", GPU: "ADA_24"}
	got := hereticMakeArgs("Org/M", opts, true, true)
	joined := strings.Join(got, " ")
	for _, want := range []string{"Org/M", "--dry-run", "--yes", "--trials", "50", "--cloud", "COMMUNITY", "--no-upload", "--gpu", "ADA_24"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q: %v", want, got)
		}
	}
}
