package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/jobs"
	"github.com/adamsiwiec1/runhug/internal/runpod"
	"github.com/adamsiwiec1/runhug/internal/store"
)

func cmdList(args []string) error {
	fs := newFlagSet("list")
	localOnly := fs.Bool("local", false, "registry only")
	remote := fs.Bool("remote", false, "include account endpoints (default when connected)")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	reg, path, err := store.Load()
	if err != nil {
		return err
	}
	env := config.Load()
	wantRemote := !*localOnly && (env.Connected() || *remote)
	if *remote && !env.Connected() {
		return env.RequireRunpod()
	}

	var eps []runpod.Endpoint
	if wantRemote {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		eps, err = runpod.New(env.RunpodAPIKey).ListEndpoints(ctx, 100)
		if err != nil {
			if *asJSON {
				return err
			}
			fmt.Fprintln(os.Stdout, yellow("Runpod list failed: "+err.Error()))
			fmt.Fprintln(os.Stdout)
			eps = nil
			wantRemote = false
		}
	}

	if *asJSON {
		if wantRemote {
			return writeJSON(map[string]any{"registry": reg, "remote": eps})
		}
		return writeJSON(reg)
	}

	heading(os.Stdout, "List")
	if len(reg.Models) == 0 {
		fmt.Fprintf(os.Stdout, "%s  %s\n\n", dim("registry"), path)
		commands(os.Stdout, "Empty — search or init:",
			"runhug search instruct --sort likes",
			"runhug init",
		)
	} else {
		printRegistry(reg)
	}

	ours := registryEndpointIDs(reg)
	if wantRemote {
		printRemoteEndpoints(eps, ours)
	} else if !env.Connected() {
		fmt.Fprintln(os.Stdout, dim("Runpod  (not connected)"))
		commands(os.Stdout, "",
			"runhug connect",
		)
	}
	if len(reg.Models) > 0 || wantRemote {
		commands(os.Stdout, "Next:",
			"runhug proxy",
			"runhug deploy <org/model>",
		)
	}
	return nil
}

func registryEndpointIDs(reg *store.Registry) map[string]string {
	out := map[string]string{}
	for _, m := range reg.Models {
		if m.EndpointID != "" {
			out[m.EndpointID] = m.HFRepo
		}
	}
	return out
}

func printRegistry(reg *store.Registry) {
	w := os.Stdout
	ids := make([]string, 0, len(reg.Models))
	for id := range reg.Models {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if reg.Current != "" {
		cur := []string{reg.Current}
		for _, id := range ids {
			if id != reg.Current {
				cur = append(cur, id)
			}
		}
		ids = cur
	}

	idW := 8
	backW := 8
	for _, id := range ids {
		m := reg.Models[id]
		if n := len(id); n > idW {
			idW = n
		}
		b := registryBackend(m)
		if n := len(b); n > backW {
			backW = n
		}
	}
	if idW > 48 {
		idW = 48
	}

	fmt.Fprintln(w, bold("Registry"))
	fmt.Fprintf(w, "  %s  %s  %s  %s  %s\n",
		dim(padRight("", 1)),
		dim(padRight("MODEL", idW)),
		dim(padRight("BACKEND", backW)),
		dim(padRight("WHERE", 16)),
		dim("OPENAI / ENDPOINT"),
	)
	for _, id := range ids {
		m := reg.Models[id]
		mark := " "
		if id == reg.Current {
			mark = "*"
		}
		backend := registryBackend(m)
		where := "local"
		target := dash(m.BaseURL)
		if m.Kind() == store.BackendLocal {
			if target == "-" && m.GGUFPath != "" {
				target = m.GGUFPath
			}
		} else if m.PodID != "" {
			where = "pod"
			target = m.PodID
			if m.DashboardURL != "" {
				target = m.DashboardURL
			}
		} else {
			where = "runpod"
			target = m.EndpointID
		}
		star := mark
		if mark == "*" {
			star = green("*")
		}
		fmt.Fprintf(w, "  %s  %s  %s  %s  %s\n",
			star,
			bold(padRight(truncateRunes(id, idW), idW)),
			dim(padRight(backend, backW)),
			dim(padRight(where, 16)),
			cyan(target),
		)
	}
	fmt.Fprintln(w)
	if reg.Current != "" {
		printKV(w, "current", bold(reg.Current))
		fmt.Fprintln(w)
	}
}

func registryBackend(m store.Model) string {
	backend := m.Kind()
	if m.Runtime != "" {
		return m.Kind() + "/" + m.Runtime
	}
	return backend
}

func printRemoteEndpoints(eps []runpod.Endpoint, ours map[string]string) {
	w := os.Stdout
	fmt.Fprintln(w, bold("Runpod"))
	if len(eps) == 0 {
		fmt.Fprintln(w, dim("  no serverless endpoints"))
		fmt.Fprintln(w)
		return
	}
	idW, nameW := 12, 16
	for _, ep := range eps {
		if n := len(ep.ID); n > idW {
			idW = n
		}
		label := endpointLabel(ep)
		if repo := ours[ep.ID]; repo != "" {
			label = repo
		}
		if n := len(label); n > nameW {
			nameW = n
		}
	}
	if idW > 28 {
		idW = 28
	}
	if nameW > 40 {
		nameW = 40
	}
	fmt.Fprintf(w, "  %s  %s  %s  %s\n",
		dim(padRight("ID", idW)),
		dim(padRight("MODEL / NAME", nameW)),
		dim(padRight("MARK", 8)),
		dim("IMAGE"),
	)
	for _, ep := range eps {
		label := endpointLabel(ep)
		markOut := dim(padRight("account", 8))
		if repo, ok := ours[ep.ID]; ok {
			markOut = green(padRight("ours", 8))
			if repo != "" {
				label = repo
			}
		}
		fmt.Fprintf(w, "  %s  %s  %s  %s\n",
			cyan(padRight(truncateRunes(ep.ID, idW), idW)),
			bold(padRight(truncateRunes(label, nameW), nameW)),
			markOut,
			dim(dash(ep.Image)),
		)
	}
	fmt.Fprintln(w)
}

func endpointLabel(ep runpod.Endpoint) string {
	if ep.Env != nil {
		if m := strings.TrimSpace(ep.Env["MODEL_NAME"]); m != "" {
			return m
		}
		if m := strings.TrimSpace(ep.Env["OPENAI_SERVED_MODEL_NAME_OVERRIDE"]); m != "" {
			return m
		}
	}
	return ep.Name
}

func cmdUse(args []string) error {
	fs := newFlagSet("use")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	reg, _, err := store.Load()
	if err != nil {
		return err
	}
	if fs.NArg() == 0 {
		if reg.Current == "" {
			return fmt.Errorf("no current model; deploy or import one")
		}
		m := reg.Models[reg.Current]
		fmt.Fprintf(os.Stdout, "%s  %s\n", bold(reg.Current), dim(m.Kind()))
		if m.Kind() == store.BackendLocal {
			fmt.Fprintln(os.Stdout, cyan(dash(m.BaseURL)))
		} else {
			fmt.Fprintln(os.Stdout, cyan(runpod.OpenAIURLFor(m.EndpointType, m.EndpointID)))
		}
		return nil
	}
	m, err := reg.Use(fs.Arg(0))
	if err != nil {
		return err
	}
	if err := reg.Save(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "%s  %s  (%s)\n", green("current"), bold(m.HFRepo), dim(m.Kind()))
	if m.Kind() == store.BackendLocal {
		fmt.Fprintln(os.Stdout, cyan(dash(m.BaseURL)))
	} else {
		fmt.Fprintln(os.Stdout, cyan(runpod.OpenAIURLFor(m.EndpointType, m.EndpointID)))
	}
	return nil
}

func cmdURL(args []string) error {
	fs := newFlagSet("url")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	reg, _, err := store.Load()
	if err != nil {
		return err
	}
	key := ""
	if fs.NArg() > 0 {
		key = fs.Arg(0)
	}
	m, ok := reg.Lookup(key)
	if !ok {
		return fmt.Errorf("unknown model")
	}
	if m.Kind() == store.BackendLocal {
		if m.BaseURL == "" {
			return fmt.Errorf("local model has no URL; run `local start`")
		}
		fmt.Println(m.BaseURL)
		return nil
	}
	fmt.Println(runpod.OpenAIURLFor(m.EndpointType, m.EndpointID))
	return nil
}

func cmdDelete(args []string) error {
	fs := newFlagSet("delete")
	yes := fs.Bool("yes", false, "delete without a prompt")
	keepLocal := fs.Bool("keep-local", false, "do not remove the registry entry")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: runhug delete <model|endpoint-id>")
	}
	env := config.Load()
	reg, _, err := store.Load()
	if err != nil {
		return err
	}
	key := fs.Arg(0)
	m, ok := reg.Lookup(key)
	id := key
	label := key
	if ok {
		id = m.EndpointID
		label = m.HFRepo
		if m.Kind() == store.BackendLocal {
			if !*yes && !confirm("Remove local registry row "+label+"?") {
				return fmt.Errorf("aborted")
			}
			if m.LocalPID != 0 {
				if proc, err := os.FindProcess(m.LocalPID); err == nil {
					_ = proc.Kill()
				}
			}
			reg.Remove(m.HFRepo)
			if err := reg.Save(); err != nil {
				return err
			}
			fmt.Printf("%s %s\n", green("removed"), bold(label))
			return nil
		}
		if m.PodID != "" {
			if err := env.RequireRunpod(); err != nil {
				return err
			}
			if !*yes && !confirm("Terminate pod "+m.PodID+" ("+label+")?") {
				return fmt.Errorf("aborted")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := runpod.New(env.RunpodAPIKey).DeletePod(ctx, m.PodID); err != nil {
				return err
			}
			if !*keepLocal {
				reg.Remove(m.HFRepo)
				if err := reg.Save(); err != nil {
					return err
				}
			}
			fmt.Printf("%s %s\n", green("terminated"), cyan(m.PodID))
			return nil
		}
	}
	if err := env.RequireRunpod(); err != nil {
		return err
	}
	if !*yes && !confirm("Delete endpoint "+id+" ("+label+")?") {
		return fmt.Errorf("aborted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := runpod.New(env.RunpodAPIKey).DeleteEndpoint(ctx, id); err != nil {
		return err
	}
	if ok && !*keepLocal {
		reg.Remove(m.HFRepo)
		if err := reg.Save(); err != nil {
			return err
		}
	}
	fmt.Printf("%s %s\n", green("deleted"), cyan(id))
	return nil
}

func cmdStatus(args []string) error {
	fs := newFlagSet("status")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	env := config.Load()
	reg, _, err := store.Load()
	if err != nil {
		return err
	}
	key := ""
	if fs.NArg() > 0 {
		key = fs.Arg(0)
	}
	m, ok := reg.Lookup(key)
	if !ok {
		return fmt.Errorf("unknown model")
	}
	if m.Kind() == store.BackendLocal {
		fmt.Fprintf(os.Stdout, "%s  %s\n", bold(m.HFRepo), dim("local"))
		if m.BaseURL != "" {
			printKV(os.Stdout, "url", cyan(m.BaseURL))
		}
		if m.GGUFPath != "" {
			printKV(os.Stdout, "gguf", m.GGUFPath)
		}
		if m.LocalPID != 0 {
			printKV(os.Stdout, "pid", fmt.Sprintf("%d", m.LocalPID))
		}
		return nil
	}
	if m.PodID != "" {
		if err := env.RequireRunpod(); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pod, err := runpod.New(env.RunpodAPIKey).GetPod(ctx, m.PodID)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "%s  %s\n", bold(m.HFRepo), cyan(m.PodID))
		printKV(os.Stdout, "status", pod.Status)
		printKV(os.Stdout, "cloud", strings.ToUpper(m.PodCloud))
		printKV(os.Stdout, "gpu", fmt.Sprintf("%s ×%d", m.GPUPool, m.GPUCount))
		if pod.Runtime != nil && pod.Runtime.Uptime > 0 {
			printKV(os.Stdout, "uptime", fmt.Sprintf("%dm%02ds", pod.Runtime.Uptime/60, pod.Runtime.Uptime%60))
		}
		if m.DashboardURL != "" {
			printKV(os.Stdout, "dashboard", cyan(m.DashboardURL))
		}
		if m.HourlyUSD > 0 {
			printKV(os.Stdout, "billing", fmt.Sprintf("~$%.2f/hr", m.HourlyUSD))
		}
		return nil
	}
	if err := env.RequireRunpod(); err != nil {
		return err
	}
	h, err := jobs.Health(env.RunpodAPIKey, m.EndpointID)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "%s  %s\n", bold(m.HFRepo), cyan(m.EndpointID))
	if h != nil && h.Workers != nil {
		w := h.Workers
		printKV(os.Stdout, "workers", fmt.Sprintf("ready=%s running=%s idle=%s initializing=%s throttled=%s",
			itoaPtr(w.Ready), itoaPtr(w.Running), itoaPtr(w.Idle), itoaPtr(w.Initializing), itoaPtr(w.Throttled)))
	}
	if h != nil && h.Jobs != nil {
		j := h.Jobs
		printKV(os.Stdout, "jobs", fmt.Sprintf("queue=%s progress=%s completed=%s failed=%s",
			itoaPtr(j.InQueue), itoaPtr(j.InProgress), itoaPtr(j.Completed), itoaPtr(j.Failed)))
	}
	return nil
}

func cmdImport(args []string) error {
	fs := newFlagSet("import")
	model := fs.String("model", "", "Hugging Face repo id to bind")
	gpu := fs.String("gpu", "", "optional pool label")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() < 1 || *model == "" {
		return fmt.Errorf("usage: runhug import <endpoint-id> --model org/name")
	}
	reg, _, err := store.Load()
	if err != nil {
		return err
	}
	reg.Put(store.Model{
		HFRepo:     *model,
		EndpointID: fs.Arg(0),
		GPUPool:    *gpu,
		GPUCount:   1,
		CreatedAt:  time.Now().UTC(),
	})
	reg.Current = *model
	if err := reg.Save(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "%s  %s → %s\n", green("imported"), bold(*model), cyan(fs.Arg(0)))
	fmt.Fprintln(os.Stdout, cyan(runpod.OpenAIURL(fs.Arg(0))))
	return nil
}

func cmdGPUs(args []string) error {
	// Deprecated alias — kept for any external callers; routes to gpu list.
	return cmdGPUList(args)
}

func itoaPtr(p *int) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("%d", *p)
}
