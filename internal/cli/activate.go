package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/hf"
	"github.com/adamsiwiec1/runhug/internal/local"
	"github.com/adamsiwiec1/runhug/internal/runtime"
	"github.com/adamsiwiec1/runhug/internal/store"
)

type localSpec struct {
	HF     string
	Ollama string
	MLX    string
	GGUF   string
}

func specFromDefault(d runtime.DefaultModel) localSpec {
	return localSpec{HF: d.HF, Ollama: d.Ollama, MLX: d.MLX, GGUF: d.GGUF}
}

func specFromOverride(raw string, kind string) localSpec {
	raw = strings.TrimSpace(raw)
	s := localSpec{HF: raw}
	if strings.Contains(raw, "/") {
		if strings.HasPrefix(strings.ToLower(raw), "mlx-community/") {
			s.MLX = raw
		}
		return s
	}
	switch kind {
	case runtime.Ollama:
		s.Ollama = raw
	case runtime.MLX:
		s.MLX = raw
	}
	return s
}

func specFromHub(m hf.Model, kind string) localSpec {
	id := m.RepoID()
	s := localSpec{HF: id}
	if kind == runtime.Ollama {
		s.Ollama = runtime.OllamaRef(id, "")
	}
	if kind == runtime.MLX {
		s.MLX = runtime.MapMLX(id)
	}
	if hf.DetectFormat(m).Engine == hf.EngineGGUF {
		s.GGUF = id
	}
	return s
}

func activateLocal(eng runtime.Engine, spec localSpec) error {
	reg, _, err := store.Load()
	if err != nil {
		return err
	}
	m := store.Model{
		HFRepo:    spec.HF,
		Backend:   store.BackendLocal,
		Runtime:   eng.Kind,
		CreatedAt: time.Now().UTC(),
	}
	if m.HFRepo == "" {
		m.HFRepo = firstNonEmpty(spec.Ollama, spec.MLX, spec.GGUF)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	switch eng.Kind {
	case runtime.Ollama:
		name := spec.Ollama
		if name == "" {
			name = runtime.OllamaRef(firstNonEmpty(spec.HF, spec.GGUF), "")
		}
		if err := runtime.PullOllama(eng.Binary, name); err != nil {
			return err
		}
		m.ServeName = name
		m.BaseURL = runtime.OllamaURL
	case runtime.MLX:
		name := spec.MLX
		if name == "" {
			name = runtime.MapMLX(firstNonEmpty(spec.HF, spec.GGUF))
		}
		cmd, err := runtime.StartMLX(name, runtime.MLXPort)
		if err != nil {
			return err
		}
		m.ServeName = name
		m.BaseURL = runtime.MLXURL
		m.LocalPID = cmd.Process.Pid
	default:
		repo := firstNonEmpty(spec.GGUF, spec.HF)
		if repo == "" {
			return fmt.Errorf("llama.cpp needs a Hub GGUF repo (pass --model org/name)")
		}
		client := hf.New(config.Load().HFToken)
		model, err := client.Get(ctx, repo)
		if err != nil {
			return err
		}
		filename, quant, ok := hf.PickGGUF(*model)
		if !ok {
			return fmt.Errorf("%s has no GGUF weights; try --runtime ollama", model.RepoID())
		}
		dir, err := hf.CacheDir()
		if err != nil {
			return err
		}
		dest := filepath.Join(dir, strings.ReplaceAll(model.RepoID(), "/", "--"), filepath.Base(filename))
		fmt.Fprintf(os.Stderr, "downloading %s (%s) → %s\n", filename, quant, dest)
		if err := client.Download(ctx, model.RepoID(), filename, dest); err != nil {
			return err
		}
		wantEmbed := strings.Contains(strings.ToLower(model.RepoID()), "embed")
		cmd, err := local.Start(eng.Binary, dest, model.RepoID(), runtime.LlamaPort, 4096, 0, wantEmbed)
		if err != nil {
			return err
		}
		m.HFRepo = model.RepoID()
		m.GGUFPath = dest
		m.LocalPID = cmd.Process.Pid
		m.BaseURL = local.DefaultURL(runtime.LlamaPort)
		m.ServeName = model.RepoID()
		if wantEmbed {
			m.Role = store.RoleEmbed
		}
	}

	reg.Put(m)
	reg.Current = m.HFRepo
	if err := reg.Save(); err != nil {
		return err
	}
	printInstalled(m)
	return nil
}

func printInstalled(m store.Model) {
	w := os.Stdout
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s  %s\n", green("Installed"), bold(m.HFRepo))
	if m.ServeName != "" && m.ServeName != m.HFRepo {
		printKV(w, "pull", m.ServeName)
	}
	if m.Runtime != "" {
		printKV(w, "runtime", m.Runtime)
	}
	if m.BaseURL != "" {
		printKV(w, "openai", cyan(m.BaseURL))
	}
	if strings.Contains(m.HFRepo, "/") {
		printKV(w, "hub", cyan(hubLink(m.HFRepo)))
	}
	fmt.Fprintln(w)
	commands(w, "Next:",
		`runhug search -q "top penetration testing models"`,
		"runhug inspect "+m.HFRepo,
		"runhug connect",
		"runhug deploy "+m.HFRepo,
		"runhug proxy",
	)
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
