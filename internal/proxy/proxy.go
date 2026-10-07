package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/gcp"
	"github.com/adamsiwiec1/runhug/internal/runpod"
	"github.com/adamsiwiec1/runhug/internal/store"
)

type Server struct {
	Registry   *store.Registry
	APIKey     string
	EmbedModel string // settings.embed_model; optional preferred registry id
}

func New(reg *store.Registry, apiKey string) *Server {
	return &Server{
		Registry:   reg,
		APIKey:     config.SanitizeAPIKey(apiKey),
		EmbedModel: strings.TrimSpace(config.LoadSettings().EmbedModel),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/v1/models", s.models)
	mux.HandleFunc("/openai/v1/models", s.models)
	mux.HandleFunc("/v1/", s.forward)
	mux.HandleFunc("/openai/v1/", s.forward)
	return mux
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	embedID := ""
	if m, ok := s.Registry.PickEmbed(s.EmbedModel); ok {
		embedID = m.HFRepo
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"current":     s.Registry.Current,
		"models":      len(s.Registry.Models),
		"embed_model": embedID,
	})
}

func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	type item struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		OwnedBy string `json:"owned_by"`
	}
	data := make([]item, 0, len(s.Registry.Models))
	for id := range s.Registry.Models {
		data = append(data, item{ID: id, Object: "model", OwnedBy: "runhug"})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func (s *Server) forward(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && (strings.HasSuffix(r.URL.Path, "/models") || r.URL.Path == "/v1/models" || r.URL.Path == "/openai/v1/models") {
		s.models(w, r)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		openaiError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	_ = r.Body.Close()

	model := ""
	var peek struct {
		Model string `json:"model"`
	}
	if len(body) > 0 && json.Unmarshal(body, &peek) == nil {
		model = peek.Model
	}

	suffix := openaiSuffix(r.URL.Path)
	wantEmbed := strings.HasSuffix(suffix, "/embeddings") || suffix == "/embeddings"

	entry, ok := s.Registry.Lookup(model)
	if wantEmbed {
		if !ok || !entry.IsEmbed() {
			if emb, found := s.Registry.PickEmbed(s.EmbedModel); found {
				entry, ok = emb, true
			}
		}
	}
	if !ok {
		if wantEmbed {
			openaiError(w, http.StatusNotFound, "no embedding model in registry; add nomic-embed-text (ollama) or deploy an embed GGUF with --embeddings")
			return
		}
		openaiError(w, http.StatusNotFound, "unknown model "+model+"; register it with init, local add, or deploy")
		return
	}
	if rewritten, err := rewriteModel(body, entry.UpstreamModel()); err == nil {
		body = rewritten
	}
	if wantEmbed && entry.Runtime == "ollama" {
		if stripped, err := stripDimensions(body); err == nil {
			body = stripped
		}
	}

	upstream, err := upstreamBase(entry)
	if err != nil {
		openaiError(w, http.StatusBadGateway, err.Error())
		return
	}
	target, err := url.Parse(upstream + suffix)
	if err != nil {
		openaiError(w, http.StatusBadGateway, "bad upstream url")
		return
	}
	if r.URL.RawQuery != "" {
		target.RawQuery = r.URL.RawQuery
	}

	auth := upstreamAuth(entry, s.APIKey)
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			u := *target
			pr.Out.URL = &u
			pr.Out.Host = u.Host
			pr.SetXForwarded()
			if auth != "" {
				pr.Out.Header.Set("Authorization", "Bearer "+auth)
			} else if entry.Kind() != store.BackendRunpod {
				pr.Out.Header.Del("Authorization")
			}
			pr.Out.Header.Del("Cookie")
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			openaiError(w, http.StatusBadGateway, "upstream: "+err.Error())
		},
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("Content-Length", itoa(len(body)))
	proxy.ServeHTTP(w, r)
}

func upstreamBase(entry store.Model) (string, error) {
	switch entry.Kind() {
	case store.BackendLocal, store.BackendGCP:
		if entry.BaseURL == "" {
			if entry.Kind() == store.BackendLocal {
				return "", errString("local model has no base_url; run `local start` or point ollama at 127.0.0.1:11434/v1")
			}
			return "", errString("gcp model has no base_url; run `runhug gcp tunnel` first")
		}
		return strings.TrimRight(entry.BaseURL, "/"), nil
	default:
		return runpod.OpenAIURLFor(entry.EndpointType, entry.EndpointID), nil
	}
}

func upstreamAuth(entry store.Model, runpodKey string) string {
	switch entry.Kind() {
	case store.BackendRunpod:
		return config.SanitizeAPIKey(runpodKey)
	case store.BackendGCP:
		if entry.PodID == "" {
			return ""
		}
		tok, err := gcp.LoadBearer(entry.PodID)
		if err != nil {
			return ""
		}
		return config.SanitizeAPIKey(tok)
	default:
		return ""
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func rewriteModel(body []byte, id string) ([]byte, error) {
	if len(body) == 0 || id == "" {
		return body, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return body, err
	}
	obj["model"] = id
	return json.Marshal(obj)
}

// stripDimensions drops OpenAI "dimensions" so ollama/nomic can answer.
func stripDimensions(body []byte) ([]byte, error) {
	if len(body) == 0 {
		return body, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return body, err
	}
	if _, ok := obj["dimensions"]; !ok {
		return body, nil
	}
	delete(obj, "dimensions")
	return json.Marshal(obj)
}

func openaiSuffix(path string) string {
	path = strings.TrimPrefix(path, "/openai")
	path = strings.TrimPrefix(path, "/v1")
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func openaiError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    "invalid_request_error",
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func ListenAndServe(addr string, h http.Handler) error {
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}
