package semantic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/version"
)

// Ollama embeds via POST /api/embed (batched). Falls back to OpenAI-compatible
// /v1/embeddings. Never calls /chat/completions.
type Ollama struct {
	BaseURL string
	Model   string
	HTTP    *http.Client
}

func (o *Ollama) Label() string {
	return strings.TrimSpace(o.Model) + " (ollama)"
}

func (o *Ollama) Embed(ctx context.Context, query string, docs []string) ([]float32, [][]float32, error) {
	if o == nil || o.BaseURL == "" || o.Model == "" {
		return nil, nil, fmt.Errorf("ollama embed: no model")
	}
	texts := make([]string, 0, 1+len(docs))
	texts = append(texts, prefixNomic(o.Model, true, query))
	for _, d := range docs {
		texts = append(texts, prefixNomic(o.Model, false, clipEmbed(d)))
	}
	vecs, err := o.embedAll(ctx, texts)
	if err != nil {
		return nil, nil, err
	}
	if len(vecs) != len(texts) {
		return nil, nil, fmt.Errorf("ollama embed: got %d vectors for %d texts", len(vecs), len(texts))
	}
	return vecs[0], vecs[1:], nil
}

func prefixNomic(model string, query bool, text string) string {
	if !strings.Contains(strings.ToLower(model), "nomic") {
		return text
	}
	if query {
		return "search_query: " + text
	}
	return "search_document: " + text
}

func (o *Ollama) embedAll(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for i := 0; i < len(texts); i += ollamaBatch {
		end := i + ollamaBatch
		if end > len(texts) {
			end = len(texts)
		}
		batch, err := o.embedBatch(ctx, texts[i:end])
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
	}
	return out, nil
}

func (o *Ollama) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	vecs, err := o.postEmbed(ctx, "/api/embed", ollamaEmbedReq{Model: o.Model, Input: texts, Truncate: true})
	if err == nil {
		return vecs, nil
	}
	return o.postOpenAI(ctx, texts)
}

type ollamaEmbedReq struct {
	Model    string   `json:"model"`
	Input    []string `json:"input"`
	Truncate bool     `json:"truncate"`
}

type ollamaEmbedRes struct {
	Embeddings [][]float64 `json:"embeddings"`
	Embedding  []float64   `json:"embedding"`
}

func (o *Ollama) postEmbed(ctx context.Context, path string, body ollamaEmbedReq) ([][]float32, error) {
	raw, code, err := o.post(ctx, o.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		return nil, fmt.Errorf("ollama embed: HTTP %d: %s", code, trimHTTP(raw))
	}
	var res ollamaEmbedRes
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("ollama embed: decode: %w", err)
	}
	if len(res.Embeddings) > 0 {
		out := make([][]float32, len(res.Embeddings))
		for i, v := range res.Embeddings {
			out[i] = toF32(v)
		}
		return out, nil
	}
	if len(res.Embedding) > 0 {
		return [][]float32{toF32(res.Embedding)}, nil
	}
	return nil, fmt.Errorf("ollama embed: empty embeddings")
}

type openaiEmbReq struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openaiEmbRes struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

func (o *Ollama) postOpenAI(ctx context.Context, texts []string) ([][]float32, error) {
	raw, code, err := o.post(ctx, o.BaseURL+"/v1/embeddings", openaiEmbReq{Model: o.Model, Input: texts})
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		return nil, fmt.Errorf("ollama embeddings: HTTP %d: %s", code, trimHTTP(raw))
	}
	var res openaiEmbRes
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("ollama embeddings: decode: %w", err)
	}
	out := make([][]float32, len(texts))
	for _, d := range res.Data {
		if d.Index < 0 || d.Index >= len(out) {
			continue
		}
		out[d.Index] = toF32(d.Embedding)
	}
	for i, v := range out {
		if len(v) == 0 {
			return nil, fmt.Errorf("ollama embeddings: missing index %d", i)
		}
	}
	return out, nil
}

// PickOllamaEmbed chooses a local embedding model. Instruct/chat names score 0.
func PickOllamaEmbed(names []string) string {
	best, bestScore := "", 0
	for _, n := range names {
		if s := embedModelScore(n); s > bestScore {
			best, bestScore = n, s
		}
	}
	return best
}

func embedModelScore(name string) int {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return 0
	}
	if strings.Contains(n, "instruct") || strings.Contains(n, "chat") {
		if !strings.Contains(n, "embed") {
			return 0
		}
	}
	switch {
	case strings.Contains(n, "nomic-embed"):
		return 100
	case strings.Contains(n, "all-minilm"):
		return 80
	case strings.Contains(n, "mxbai-embed"):
		return 70
	case strings.Contains(n, "bge-") && strings.Contains(n, "embed"):
		return 60
	case strings.Contains(n, "nomic") && strings.Contains(n, "embed"):
		return 90
	case strings.Contains(n, "embed"):
		return 40
	default:
		return 0
	}
}

func (o *Ollama) post(ctx context.Context, url string, body any) ([]byte, int, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", version.Name+"/"+version.Version)
	client := o.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, res.StatusCode, err
	}
	return b, res.StatusCode, nil
}

func trimHTTP(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
