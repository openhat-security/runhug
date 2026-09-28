package bridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/adamsiwiec1/runhug/internal/hparams"
)

// Config for the local Anthropic→OpenAI bridge.
type Config struct {
	// UpstreamBase is the OpenAI-compatible root ending in /v1 (e.g. Runpod …/openai/v1).
	UpstreamBase string
	// UpstreamKey is forwarded as Authorization: Bearer to the upstream.
	UpstreamKey string
	// DefaultModel overrides Claude-ish model ids in requests.
	DefaultModel string
	// ListenAddr e.g. "127.0.0.1:0" or "127.0.0.1:9090".
	ListenAddr string
	// ExpectedToken optional; if set, require Bearer or x-api-key to match.
	ExpectedToken string
	// Sampling fills unset temperature/top_p/max_tokens on translated requests.
	Sampling *hparams.Sampling
}

// Server is a local HTTP bridge.
type Server struct {
	cfg    Config
	client *http.Client
	http   *http.Server
	ln     net.Listener
	mu     sync.Mutex
}

// Start listens and serves in the background. Returns the bound base URL
// (http://127.0.0.1:<port>) with no trailing path — Claude uses this as ANTHROPIC_BASE_URL.
func Start(cfg Config) (*Server, string, error) {
	cfg.UpstreamBase = strings.TrimRight(strings.TrimSpace(cfg.UpstreamBase), "/")
	if cfg.UpstreamBase == "" {
		return nil, "", fmt.Errorf("bridge: empty upstream base")
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:0"
	}
	s := &Server{
		cfg: cfg,
		client: &http.Client{
			Timeout: 10 * time.Minute,
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				MaxIdleConns:          32,
				IdleConnTimeout:       90 * time.Second,
				ResponseHeaderTimeout: 5 * time.Minute,
			},
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/messages", s.handleMessages)
	mux.HandleFunc("/v1/messages/", s.handleMessages) // count_tokens under prefix
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/", s.handleRoot)

	ln, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return nil, "", err
	}
	s.ln = ln
	s.http = &http.Server{Handler: mux, ReadHeaderTimeout: 15 * time.Second}
	go func() { _ = s.http.Serve(ln) }()
	addr := ln.Addr().String()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		_ = s.Close()
		return nil, "", err
	}
	if host == "::" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	base := fmt.Sprintf("http://%s:%s", host, port)
	return s, base, nil
}

// Close shuts down the listener.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if s.http != nil {
		_ = s.http.Shutdown(ctx)
	}
	if s.ln != nil {
		return s.ln.Close()
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "bridge": "anthropic-openai"})
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" || r.URL.Path == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"name":     "runhug-anthropic-bridge",
			"upstream": s.cfg.UpstreamBase,
			"endpoints": []string{
				"POST /v1/messages",
				"POST /v1/messages/count_tokens",
				"GET /v1/models",
				"GET /health",
			},
		})
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := s.cfg.DefaultModel
	if id == "" {
		id = "default"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data": []map[string]any{
			{"id": id, "object": "model", "owned_by": "runhug"},
		},
	})
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	if strings.HasSuffix(path, "/count_tokens") {
		s.handleCountTokens(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorize(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write(anthropicErrorJSON("authentication_error", "invalid api key"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		anthropicHTTPError(w, http.StatusBadRequest, "invalid_request_error", "failed to read body")
		return
	}
	_ = r.Body.Close()

	openaiBody, areq, err := TranslateRequest(body, s.cfg.DefaultModel)
	if err != nil {
		anthropicHTTPError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	openaiBody = FillOpenAISampling(openaiBody, s.cfg.Sampling)
	modelForResp := areq.Model
	if dm := strings.TrimSpace(s.cfg.DefaultModel); dm != "" && (modelForResp == "" || isClaudeish(modelForResp)) {
		modelForResp = dm
	}

	hadTools := openaiBodyHasTools(openaiBody)
	upRes, err := s.doChatCompletions(r.Context(), openaiBody)
	if err != nil {
		anthropicHTTPError(w, http.StatusBadGateway, "api_error", "upstream: "+err.Error())
		return
	}
	retriedSansTools := false
	// worker-vllm often 500s on tools unless ENABLE_AUTO_TOOL_CHOICE + TOOL_CALL_PARSER
	// are configured (and some models still lack tool support). Retry once without tools.
	if upRes.StatusCode >= 500 && hadTools {
		_ = upRes.Body.Close()
		stripped := stripTools(openaiBody)
		retriedSansTools = true
		fmt.Fprintln(os.Stderr, "runhug bridge: upstream rejected tools (HTTP 5xx); retrying without tools/tool_choice")
		upRes, err = s.doChatCompletions(r.Context(), stripped)
		if err != nil {
			anthropicHTTPError(w, http.StatusBadGateway, "api_error", "upstream: "+err.Error())
			return
		}
	}
	defer upRes.Body.Close()

	if upRes.StatusCode < 200 || upRes.StatusCode >= 300 {
		ub, _ := io.ReadAll(io.LimitReader(upRes.Body, 1<<20))
		msg := strings.TrimSpace(string(ub))
		if len(msg) > 800 {
			msg = msg[:800] + "…"
		}
		typ := "api_error"
		if upRes.StatusCode == 401 || upRes.StatusCode == 403 {
			typ = "authentication_error"
		}
		anthropicHTTPError(w, upRes.StatusCode, typ, fmt.Sprintf("upstream HTTP %d: %s", upRes.StatusCode, msg))
		return
	}

	ct := upRes.Header.Get("Content-Type")
	wantStream := areq.Stream || strings.Contains(ct, "text/event-stream")
	if wantStream {
		s.serveStream(w, r, upRes, modelForResp, openaiBody, hadTools, retriedSansTools)
		return
	}

	ub, err := io.ReadAll(io.LimitReader(upRes.Body, 16<<20))
	if err != nil {
		anthropicHTTPError(w, http.StatusBadGateway, "api_error", "read upstream: "+err.Error())
		return
	}
	out, err := TranslateResponse(ub, modelForResp)
	if err != nil {
		anthropicHTTPError(w, http.StatusBadGateway, "api_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// serveStream pipes OpenAI SSE → Anthropic SSE.
//
// When tools were present and we have not yet stripped them, peek until the
// first non-empty delta (content / reasoning / tool_calls) or EOF. Empty 200
// streams still trigger a no-tools retry (same fallback as HTTP 5xx). A live
// reasoning stream is NOT fully buffered — that held Claude on "Embellishing"
// until max_tokens completed, with no Anthropic bytes flushed.
func (s *Server) serveStream(w http.ResponseWriter, r *http.Request, upRes *http.Response, modelForResp string, originalBody []byte, hadTools, retriedSansTools bool) {
	var src io.Reader = upRes.Body
	if hadTools && !retriedSansTools {
		prefix, rest, empty, err := peekOpenAISSE(r.Context(), upRes.Body)
		if err != nil {
			anthropicHTTPError(w, http.StatusBadGateway, "api_error", "read upstream stream: "+err.Error())
			return
		}
		if empty {
			stripped := stripTools(originalBody)
			fmt.Fprintln(os.Stderr, "runhug bridge: upstream stream empty with tools; retrying without tools/tool_choice")
			up2, err := s.doChatCompletions(r.Context(), stripped)
			if err != nil {
				anthropicHTTPError(w, http.StatusBadGateway, "api_error", "upstream: "+err.Error())
				return
			}
			defer up2.Body.Close()
			if up2.StatusCode < 200 || up2.StatusCode >= 300 {
				ub, _ := io.ReadAll(io.LimitReader(up2.Body, 1<<20))
				msg := strings.TrimSpace(string(ub))
				if len(msg) > 800 {
					msg = msg[:800] + "…"
				}
				anthropicHTTPError(w, up2.StatusCode, "api_error", fmt.Sprintf("upstream HTTP %d after tools strip: %s", up2.StatusCode, msg))
				return
			}
			prefix, rest, empty, err = peekOpenAISSE(r.Context(), up2.Body)
			if err != nil {
				anthropicHTTPError(w, http.StatusBadGateway, "api_error", "read upstream stream: "+err.Error())
				return
			}
			retriedSansTools = true
		}
		if empty {
			anthropicHTTPError(w, http.StatusBadGateway, "api_error",
				"upstream returned empty stream (no content or tool_calls); refusing empty Anthropic SSE")
			return
		}
		if rest != nil {
			src = io.MultiReader(bytes.NewReader(prefix), rest)
		} else {
			src = bytes.NewReader(prefix)
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	bw := &flushWriter{w: w, f: flusher}
	_ = PipeOpenAISSE(src, bw, modelForResp)
}

// peekOpenAISSE reads OpenAI SSE until a non-empty delta or EOF.
// prefix is bytes already consumed; rest is the unread remainder (nil on EOF).
func peekOpenAISSE(ctx context.Context, body io.Reader) (prefix []byte, rest io.Reader, empty bool, err error) {
	br := bufio.NewReaderSize(body, 64*1024)
	var buf bytes.Buffer
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, false, err
		}
		line, rerr := br.ReadBytes('\n')
		if len(line) > 0 {
			buf.Write(line)
		}
		if rerr != nil {
			if rerr == io.EOF {
				return buf.Bytes(), nil, openAIStreamEmpty(buf.Bytes()), nil
			}
			return nil, nil, false, rerr
		}
		if len(bytes.TrimRight(line, "\r\n")) == 0 && !openAIStreamEmpty(buf.Bytes()) {
			return buf.Bytes(), br, false, nil
		}
	}
}

func (s *Server) handleCountTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		anthropicHTTPError(w, http.StatusBadRequest, "invalid_request_error", "failed to read body")
		return
	}
	var req anthropicReq
	if err := json.Unmarshal(body, &req); err != nil {
		anthropicHTTPError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"input_tokens": EstimateTokens(req)})
}

func (s *Server) authorize(r *http.Request) bool {
	want := strings.TrimSpace(s.cfg.ExpectedToken)
	if want == "" {
		return true
	}
	got := ""
	if ah := r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(ah), "bearer ") {
		got = strings.TrimSpace(ah[7:])
	}
	if got == "" {
		got = strings.TrimSpace(r.Header.Get("x-api-key"))
	}
	return got == want
}

type flushWriter struct {
	w io.Writer
	f http.Flusher
}

func (f *flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if f.f != nil {
		f.f.Flush()
	}
	return n, err
}

func (f *flushWriter) Flush() {
	if f.f != nil {
		f.f.Flush()
	}
}

func (s *Server) doChatCompletions(ctx context.Context, openaiBody []byte) (*http.Response, error) {
	upURL := s.cfg.UpstreamBase + "/chat/completions"
	upReq, err := http.NewRequestWithContext(ctx, http.MethodPost, upURL, bytes.NewReader(openaiBody))
	if err != nil {
		return nil, err
	}
	upReq.Header.Set("Content-Type", "application/json")
	if key := strings.TrimSpace(s.cfg.UpstreamKey); key != "" {
		upReq.Header.Set("Authorization", "Bearer "+key)
	}
	return s.client.Do(upReq)
}

// stripTools removes tools and tool_choice from an OpenAI chat-completions JSON body.
func stripTools(openaiBody []byte) []byte {
	var m map[string]any
	if err := json.Unmarshal(openaiBody, &m); err != nil {
		return openaiBody
	}
	delete(m, "tools")
	delete(m, "tool_choice")
	delete(m, "parallel_tool_calls")
	out, err := json.Marshal(m)
	if err != nil {
		return openaiBody
	}
	return out
}

// openAIStreamEmpty reports whether an OpenAI chat.completion SSE body produced no
// assistant text and no tool_calls (the blank stream Claude Code shows as "Sautéed").
func openAIStreamEmpty(sse []byte) bool {
	if len(bytes.TrimSpace(sse)) == 0 {
		return true
	}
	// Non-SSE JSON error / empty completion also counts as empty for our guard.
	trim := bytes.TrimSpace(sse)
	if len(trim) > 0 && trim[0] == '{' {
		var probe struct {
			Choices []struct {
				Message struct {
					Content          any `json:"content"`
					Reasoning        any `json:"reasoning"`
					ReasoningContent any `json:"reasoning_content"`
					ToolCalls        any `json:"tool_calls"`
				} `json:"message"`
				Delta struct {
					Content          any `json:"content"`
					Reasoning        any `json:"reasoning"`
					ReasoningContent any `json:"reasoning_content"`
					ToolCalls        any `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Error any `json:"error"`
		}
		if json.Unmarshal(trim, &probe) == nil {
			if probe.Error != nil {
				return true
			}
			if len(probe.Choices) == 0 {
				return true
			}
			ch := probe.Choices[0]
			if hasNonEmptyAny(ch.Message.Content) || hasNonEmptyAny(ch.Message.ToolCalls) ||
				hasNonEmptyAny(ch.Message.Reasoning) || hasNonEmptyAny(ch.Message.ReasoningContent) ||
				hasNonEmptyAny(ch.Delta.Content) || hasNonEmptyAny(ch.Delta.ToolCalls) ||
				hasNonEmptyAny(ch.Delta.Reasoning) || hasNonEmptyAny(ch.Delta.ReasoningContent) {
				return false
			}
			return true
		}
	}
	sc := bufio.NewScanner(bytes.NewReader(sse))
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          *string `json:"content"`
					Reasoning        string  `json:"reasoning"`
					ReasoningContent string  `json:"reasoning_content"`
					ToolCalls        []any   `json:"tool_calls"`
				} `json:"delta"`
				Message struct {
					Content          any `json:"content"`
					Reasoning        any `json:"reasoning"`
					ReasoningContent any `json:"reasoning_content"`
					ToolCalls        any `json:"tool_calls"`
				} `json:"message"`
			} `json:"choices"`
			Error any `json:"error"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}
		if chunk.Error != nil {
			continue
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.Content != nil && *ch.Delta.Content != "" {
				return false
			}
			if ch.Delta.Reasoning != "" || ch.Delta.ReasoningContent != "" {
				return false
			}
			if len(ch.Delta.ToolCalls) > 0 {
				return false
			}
			if hasNonEmptyAny(ch.Message.Content) || hasNonEmptyAny(ch.Message.ToolCalls) ||
				hasNonEmptyAny(ch.Message.Reasoning) || hasNonEmptyAny(ch.Message.ReasoningContent) {
				return false
			}
		}
	}
	return true
}

func hasNonEmptyAny(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(t) != ""
	case []any:
		return len(t) > 0
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return false
		}
		s := strings.TrimSpace(string(b))
		return s != "" && s != "null" && s != "[]" && s != `""`
	}
}

func openaiBodyHasTools(body []byte) bool {
	var probe struct {
		Tools json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}
	raw := bytes.TrimSpace(probe.Tools)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || bytes.Equal(raw, []byte("[]")) {
		return false
	}
	return true
}

func anthropicHTTPError(w http.ResponseWriter, status int, typ, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(anthropicErrorJSON(typ, msg))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
