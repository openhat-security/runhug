package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/hf"
)

type lastSearch struct {
	Query string    `json:"query"`
	IDs   []string  `json:"ids"`
	At    time.Time `json:"at"`
}

func lastSearchPath() (string, error) {
	d, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "last_search.json"), nil
}

func saveLastSearch(query string, models []hf.Model) {
	ids := make([]string, 0, len(models))
	for _, m := range models {
		if id := m.RepoID(); id != "" {
			ids = append(ids, id)
		}
	}
	p, err := lastSearchPath()
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	raw, err := json.Marshal(lastSearch{Query: query, IDs: ids, At: time.Now().UTC()})
	if err != nil {
		return
	}
	_ = os.WriteFile(p, raw, 0o600)
}

func loadLastSearch() (lastSearch, error) {
	p, err := lastSearchPath()
	if err != nil {
		return lastSearch{}, err
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return lastSearch{}, err
	}
	var ls lastSearch
	if err := json.Unmarshal(raw, &ls); err != nil {
		return lastSearch{}, err
	}
	return ls, nil
}

func lookupLastSearchID(n int) (string, bool) {
	if n < 1 {
		return "", false
	}
	ls, err := loadLastSearch()
	if err != nil || n > len(ls.IDs) {
		return "", false
	}
	id := strings.TrimSpace(ls.IDs[n-1])
	if id == "" {
		return "", false
	}
	return id, true
}

// resolveModelArg maps a 1-based last-search row number to a Hub repo id.
// A bare integer is never sent to Hugging Face as a model id.
func resolveModelArg(arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", fmt.Errorf("model required")
	}
	n, err := strconv.Atoi(arg)
	if err != nil || n < 1 || strings.Contains(arg, "/") {
		return arg, nil
	}
	if n > 100 {
		return arg, nil
	}
	ls, loadErr := loadLastSearch()
	if loadErr != nil || len(ls.IDs) == 0 {
		return "", fmt.Errorf("row %d: no last search — run `./bin/runhug search …` first (PATH runhug is the brew build and does not save rows)", n)
	}
	if n > len(ls.IDs) {
		return "", fmt.Errorf("row %d out of range for last search %q (1-%d)", n, ls.Query, len(ls.IDs))
	}
	id := strings.TrimSpace(ls.IDs[n-1])
	if id == "" {
		return "", fmt.Errorf("row %d is empty in last search", n)
	}
	fmt.Fprintf(os.Stderr, "%s  #%d %s → %s\n", dim("note:"), n, ls.Query, id)
	return id, nil
}

func parseSearchPick(line string, n int) (action string, idx int, repo string, err error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "skip", 0, "", nil
	}
	fields := strings.Fields(line)
	act := "inspect"
	num := fields[0]
	if len(fields) >= 2 {
		switch strings.ToLower(fields[0]) {
		case "inspect", "i", "use":
			act = "inspect"
			num = fields[1]
		case "deploy", "d":
			act = "deploy"
			num = fields[1]
		case "copy", "c":
			act = "copy"
			num = fields[1]
		case "run":
			act = "run"
			num = fields[1]
		default:
			return "", 0, "", fmt.Errorf("pick 1-%d, deploy N, copy N, or Enter", n)
		}
	} else if strings.Contains(line, "/") {
		return "inspect", 0, line, nil
	}
	idx, convErr := strconv.Atoi(num)
	if convErr != nil {
		return "", 0, "", fmt.Errorf("pick 1-%d, deploy N, copy N, or Enter", n)
	}
	if idx < 1 || idx > n {
		return "", 0, "", fmt.Errorf("row %d out of range (1-%d)", idx, n)
	}
	return act, idx, "", nil
}

func offerSearchPick(models []hf.Model) error {
	if len(models) == 0 || !promptOK() {
		return nil
	}
	for {
		line, err := readLine(fmt.Sprintf("Use 1-%d (inspect), deploy N, copy N, or Enter: ", len(models)))
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		act, idx, repo, err := parseSearchPick(line, len(models))
		if err != nil {
			fmt.Fprintln(os.Stderr, dim(err.Error()))
			continue
		}
		if act == "skip" {
			return nil
		}
		id := repo
		if id == "" {
			id = models[idx-1].RepoID()
		}
		switch act {
		case "inspect":
			return cmdInspect([]string{id})
		case "deploy":
			return cmdDeploy([]string{id})
		case "copy":
			if err := copyToClipboard(id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "%s  %s\n", green("copied"), id)
			return nil
		case "run":
			return cmdRun([]string{id})
		default:
			return nil
		}
	}
}
