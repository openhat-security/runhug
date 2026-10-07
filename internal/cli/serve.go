package cli

import (
	"fmt"
	"os"
	"os/signal"
	"sort"
	"syscall"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/proxy"
	"github.com/adamsiwiec1/runhug/internal/store"
)

func cmdProxy(args []string) error {
	fs := newFlagSet("proxy")
	addr := fs.String("addr", "", "listen address (default from registry or 127.0.0.1:8080)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	env := config.Load()
	reg, _, err := store.Load()
	if err != nil {
		return err
	}
	if len(reg.Models) == 0 {
		return fmt.Errorf("registry is empty; init, local add, or deploy first")
	}
	needsRunpod := false
	for _, m := range reg.Models {
		if m.Kind() == store.BackendRunpod {
			needsRunpod = true
			break
		}
	}
	if needsRunpod {
		if err := env.RequireRunpod(); err != nil {
			return err
		}
	}
	listen := *addr
	if listen == "" {
		listen = reg.Listen
	}
	if listen == "" {
		listen = "127.0.0.1:8080"
	}

	heading(os.Stdout, "Proxy")
	fmt.Fprintf(os.Stdout, "%s  %s\n", green("listening"), cyan("http://"+listen+"/v1"))
	printKV(os.Stdout, "models", fmt.Sprintf("%d", len(reg.Models)))
	if reg.Current != "" {
		printKV(os.Stdout, "current", bold(reg.Current))
	}
	settings := config.LoadSettings()
	if emb, ok := reg.PickEmbed(settings.EmbedModel); ok {
		printKV(os.Stdout, "embed_model", bold(emb.HFRepo))
	} else {
		printKV(os.Stdout, "embed_model", dim("(none — add nomic-embed-text)"))
	}
	ids := make([]string, 0, len(reg.Models))
	for id := range reg.Models {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		fmt.Fprintf(os.Stdout, "           %s\n", bold(id))
	}
	fmt.Fprintln(os.Stdout)
	printKV(os.Stdout, "base_url", cyan("http://"+listen+"/v1"))
	printKV(os.Stdout, "model", "registry id (or \"default\")")
	fmt.Fprintln(os.Stdout)
	commands(os.Stdout, "Health:",
		fmt.Sprintf("curl http://%s/v1/models", listen),
		fmt.Sprintf("curl -sS http://%s/v1/embeddings -H 'Content-Type: application/json' -d '{\"model\":\"default\",\"input\":\"hi\"}'", listen),
	)

	h := proxy.New(reg, env.RunpodAPIKey).Handler()
	errCh := make(chan error, 1)
	go func() { errCh <- proxy.ListenAndServe(listen, h) }()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sig:
		fmt.Fprintln(os.Stderr, dim("\nstopped"))
		return nil
	case err := <-errCh:
		return err
	}
}
