package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/store"
	"github.com/adamsiwiec1/runhug/internal/version"
)

// pickStartModel chooses a registry key and/or served model name when the user
// did not already pin one. Never silently uses reg.Current.
//
// Behavior:
//   - positional / --endpoint already set → pass-through
//   - --base-url with --model → pass-through
//   - --base-url without --model → GET {base}/models and pick 1–N (sets serveModel)
//   - neither → menu of usable registry endpoints; empty registry → deploy/list hint
//
// skipPrompt (non-TTY, --yes, or --no-launch): auto-pick only when exactly one
// option; otherwise error asking for an explicit arg.
func pickStartModel(registryKey, baseURL, serveModel, apiKeyEnv string, skipPrompt bool) (string, string, error) {
	registryKey = strings.TrimSpace(registryKey)
	baseURL = strings.TrimSpace(baseURL)
	serveModel = strings.TrimSpace(serveModel)

	if registryKey != "" {
		return registryKey, serveModel, nil
	}
	if baseURL != "" {
		if serveModel != "" {
			return "", serveModel, nil
		}
		ids, err := listRemoteModelIDs(baseURL, apiKeyEnv)
		if err != nil {
			return "", "", fmt.Errorf("list models at %s: %w\nHint: pass --model <served-name>", baseURL, err)
		}
		if len(ids) == 0 {
			return "", "default", nil
		}
		picked, err := pickNumbered("model", ids, func(w io.Writer) {
			printRemoteModelPickTable(w, ids)
		}, skipPrompt)
		if err != nil {
			return "", "", err
		}
		return "", picked, nil
	}

	reg, _, err := store.Load()
	if err != nil {
		return "", "", err
	}
	entries := usableRegistryEntries(reg)
	if len(entries) == 0 {
		return "", "", fmt.Errorf(
			"no registry endpoints to start\nHint: run `%s list`, or deploy (`%s deploy <model> --dry-run`) / `%s local add`",
			version.Name, version.Name, version.Name,
		)
	}
	labels := make([]string, len(entries))
	for i, e := range entries {
		labels[i] = e.HFRepo
	}
	picked, err := pickNumbered("model", labels, func(w io.Writer) {
		printRegistryPickTable(w, entries)
	}, skipPrompt)
	if err != nil {
		return "", "", err
	}
	return picked, serveModel, nil
}

type registryPickEntry struct {
	HFRepo string
	Kind   string // "runpod" | "local"
	Where  string // endpoint id or base_url
	Extra  string
}

func usableRegistryEntries(reg *store.Registry) []registryPickEntry {
	if reg == nil || len(reg.Models) == 0 {
		return nil
	}
	ids := make([]string, 0, len(reg.Models))
	for id, m := range reg.Models {
		if !registryEntryUsable(m) {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	// Surface "current" first (still requires an explicit pick).
	if reg.Current != "" {
		cur := make([]string, 0, len(ids))
		rest := make([]string, 0, len(ids))
		for _, id := range ids {
			if id == reg.Current {
				cur = append(cur, id)
			} else {
				rest = append(rest, id)
			}
		}
		ids = append(cur, rest...)
	}
	out := make([]registryPickEntry, 0, len(ids))
	for _, id := range ids {
		m := reg.Models[id]
		e := registryPickEntry{HFRepo: m.HFRepo, Kind: m.Kind()}
		switch m.Kind() {
		case store.BackendLocal:
			e.Where = m.BaseURL
			e.Extra = m.Runtime
		case store.BackendGCP:
			e.Where = m.PodID
			if e.Where == "" {
				e.Where = m.EndpointID // project fallback
			}
			e.Extra = m.EndpointType // zone
		default:
			e.Where = m.EndpointID
			e.Extra = m.EndpointType
		}
		out = append(out, e)
	}
	return out
}

func registryEntryUsable(m store.Model) bool {
	switch m.Kind() {
	case store.BackendLocal:
		return strings.TrimSpace(m.BaseURL) != ""
	case store.BackendGCP:
		return strings.TrimSpace(m.EndpointID) != "" || strings.TrimSpace(m.BaseURL) != ""
	default:
		return strings.TrimSpace(m.EndpointID) != ""
	}
}

const (
	pickColNum     = 2
	pickColModel   = 40
	pickColBackend = 8
	pickColWhere   = 28
	pickColDetail  = 14
)

func printRegistryPickTable(w io.Writer, entries []registryPickEntry) {
	fmt.Fprintln(w, bold("Endpoints"))
	modelW, whereW, detailW := 5, 5, 6 // min = header lengths MODEL/WHERE/DETAIL
	for _, e := range entries {
		if n := len(e.HFRepo); n > modelW {
			modelW = n
		}
		if n := len(e.Where); n > whereW {
			whereW = n
		}
		if n := len(e.Extra); n > detailW {
			detailW = n
		}
	}
	if modelW > pickColModel {
		modelW = pickColModel
	}
	if whereW > pickColWhere {
		whereW = pickColWhere
	}
	if detailW > pickColDetail {
		detailW = pickColDetail
	}
	fmt.Fprintf(w, "  %s  %s  %s  %s  %s\n",
		dim(padRight("#", pickColNum)),
		dim(padRight("MODEL", modelW)),
		dim(padRight("BACKEND", pickColBackend)),
		dim(padRight("WHERE", whereW)),
		dim(padRight("DETAIL", detailW)),
	)
	for i, e := range entries {
		fmt.Fprintf(w, "  %s  %s  %s  %s  %s\n",
			cyan(padRight(strconv.Itoa(i+1), pickColNum)),
			bold(padRight(truncateRunes(e.HFRepo, modelW), modelW)),
			colorBackend(padRight(truncateRunes(e.Kind, pickColBackend), pickColBackend), e.Kind),
			dim(padRight(truncateRunes(e.Where, whereW), whereW)),
			dim(padRight(truncateRunes(e.Extra, detailW), detailW)),
		)
	}
	fmt.Fprintln(w)
}

func printRemoteModelPickTable(w io.Writer, ids []string) {
	fmt.Fprintln(w, bold("Models"))
	modelW := 5
	for _, id := range ids {
		if n := len(id); n > modelW {
			modelW = n
		}
	}
	if modelW > pickColModel {
		modelW = pickColModel
	}
	fmt.Fprintf(w, "  %s  %s\n",
		dim(padRight("#", pickColNum)),
		dim(padRight("MODEL", modelW)),
	)
	for i, id := range ids {
		fmt.Fprintf(w, "  %s  %s\n",
			cyan(padRight(strconv.Itoa(i+1), pickColNum)),
			bold(padRight(truncateRunes(id, modelW), modelW)),
		)
	}
	fmt.Fprintln(w)
}

func colorBackend(padded, kind string) string {
	switch kind {
	case store.BackendLocal:
		return cyan(padded)
	case store.BackendGCP:
		return yellow(padded)
	default:
		return green(padded)
	}
}

// pickNumbered prints a 1–N menu and returns the chosen label.
func pickNumbered(noun string, labels []string, printMenu func(io.Writer), skipPrompt bool) (string, error) {
	if len(labels) == 0 {
		return "", fmt.Errorf("no %ss to pick", noun)
	}
	if len(labels) == 1 && (skipPrompt || !canPrompt()) {
		return labels[0], nil
	}
	if skipPrompt || !canPrompt() {
		return "", fmt.Errorf(
			"multiple %ss available — pass a positional arg, --endpoint, or --model (or run interactively to pick 1-%d)",
			noun, len(labels),
		)
	}

	w := os.Stderr
	printMenu(w)
	line, err := readLine(fmt.Sprintf("Pick %s 1-%d: ", noun, len(labels)))
	if err != nil {
		return "", err
	}
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(labels) {
		return "", fmt.Errorf("expected a number 1-%d", len(labels))
	}
	chosen := labels[n-1]
	fmt.Fprintln(w, green("✓")+"  "+dim("Using "+chosen))
	fmt.Fprintln(w)
	return chosen, nil
}

func listRemoteModelIDs(baseURL, apiKeyEnv string) ([]string, error) {
	base := strings.TrimRight(NormalizeOpenAIBase(baseURL), "/")
	url := base + "/models"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	key := config.Load().RunpodAPIKey
	if apiKeyEnv != "" {
		if v := config.SanitizeAPIKey(os.Getenv(apiKeyEnv)); v != "" {
			key = v
		}
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		return nil, fmt.Errorf("HTTP %d: %s", res.StatusCode, msg)
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode /models: %w", err)
	}
	ids := make([]string, 0, len(parsed.Data))
	seen := map[string]struct{}{}
	for _, d := range parsed.Data {
		id := strings.TrimSpace(d.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}
