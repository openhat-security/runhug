package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"time"

	"github.com/adamsiwiec1/runhug/internal/store"
	"github.com/adamsiwiec1/runhug/internal/version"
)

// errTurnCanceled is returned when Ctrl-C aborts an in-flight chat turn.
var errTurnCanceled = errors.New("cancelled")

// withTurnCancel runs fn with a context cancelled by SIGINT (Ctrl-C).
// The process stays alive; the REPL should print a soft "cancelled" and continue.
func withTurnCancel(parent context.Context, fn func(context.Context) error) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	defer signal.Stop(sigs)

	var hit atomic.Bool
	done := make(chan struct{})
	go func() {
		select {
		case <-sigs:
			hit.Store(true)
			cancel()
		case <-done:
		}
	}()

	err := fn(ctx)
	signal.Stop(sigs)
	close(done)
	if hit.Load() {
		return errTurnCanceled
	}
	return err
}

func cmdRun(args []string) error {
	fs := newFlagSet("run")
	baseURL := fs.String("base-url", "", "OpenAI-compatible base URL (…/v1)")
	apiKeyEnv := fs.String("api-key-env", "", "env var holding the API key (default: stored Runpod key / RUNPOD_API_KEY)")
	serveModel := fs.String("model", "", "served model name for chat completions")
	oneshot := fs.String("q", "", "one-shot prompt then exit")
	stream := fs.Bool("stream", true, "use chat completions streaming when supported")
	yes := fs.Bool("yes", false, "non-interactive: auto-pick when exactly one registry/remote model")
	sessionID := fs.String("session", "", "resume this chat session id")
	fresh := fs.Bool("new", false, "start a new chat session")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	modelKey := ""
	if fs.NArg() > 0 {
		modelKey = fs.Arg(0)
	}
	skipPrompt := *yes || !canPrompt()
	pickedKey, pickedModel, err := pickStartModel(modelKey, *baseURL, *serveModel, *apiKeyEnv, skipPrompt)
	if err != nil {
		return err
	}

	target, cleanup, err := resolveReadyEndpoint(pickedKey, *baseURL, *apiKeyEnv, pickedModel, false)
	if err != nil {
		return err
	}
	target.BaseURL = NormalizeOpenAIBase(target.BaseURL)

	cwd, _ := os.Getwd()
	sess := &runSession{Target: target, Stream: *stream, Tools: true, Cwd: cwd, RegistryKey: pickedKey, Cleanup: cleanup}
	applyRunSettingsDefaults(sess)
	defer func() {
		if sess.Cleanup != nil {
			sess.Cleanup()
		}
	}()
	if pickedKey != "" {
		if reg, _, err := store.Load(); err == nil {
			if m, ok := reg.Lookup(pickedKey); ok {
				sess.Model = m
			}
		}
	}

	heading(os.Stdout, "Run")
	printKV(os.Stdout, "base_url", cyan(target.BaseURL))
	printKV(os.Stdout, "model", bold(target.Model))
	printKV(os.Stdout, "source", target.Source)
	printKV(os.Stdout, "cwd", sess.Cwd)
	if target.APIKey == "" {
		fmt.Fprintln(os.Stdout, dim("No API key resolved — local backends may still work."))
	}
	fmt.Fprintln(os.Stdout)
	printRunStatusStrip(os.Stdout, sess)

	if q := strings.TrimSpace(*oneshot); q != "" {
		return chatOnce(target, q, *stream)
	}

	if !stdinIsTTY() {
		return fmt.Errorf("interactive run needs a TTY (or pass -q \"prompt\")")
	}

	fmt.Fprintf(os.Stdout, "%s chat — /help · Ctrl-D or /exit to quit\n", bold(version.Name))
	fmt.Fprintln(os.Stdout)

	capBuf := &lineBuf{}
	sess.TTY = os.Stdout
	sess.Transcript = capBuf
	sess.Out = &captureWriter{w: os.Stdout, buf: capBuf}
	if *fresh {
		sess.PersistID = newSessionID()
	} else if id := strings.TrimSpace(*sessionID); id != "" {
		p, err := loadPersistedChat(id)
		if err != nil {
			return err
		}
		applyPersistedChat(sess, p)
	} else if p := latestPersistedChat(sess.Cwd); p != nil && (len(p.Agent) > 0 || len(p.Chat) > 0) {
		applyPersistedChat(sess, p)
	} else {
		sess.PersistID = newSessionID()
	}
	printKV(os.Stdout, "session", sess.PersistID)
	if n := len(sess.Agent) + len(sess.Chat); n > 0 {
		fmt.Fprintln(os.Stdout, dim("resumed conversation  ·  /sessions  /new"))
		fmt.Fprintln(os.Stdout)
		printSessionHistory(sess.out(), sess)
	} else {
		fmt.Fprintln(os.Stdout, dim("drag to copy  ·  Ctrl-V paste  ·  /sessions"))
		fmt.Fprintln(os.Stdout)
	}

	clearHistory := func() {
		sess.Chat = nil
		sess.Agent = nil
		sess.undo = nil
		sess.redo = nil
	}
	defer func() { _ = saveRunPersist(sess) }()

	ed := newRunLineEditor(green("you> "), loadRunInputHistory())
	ed.Cwd = sess.Cwd
	ed.Capture = capBuf
	defer sess.stopMetricsPump()
	defer sess.leaveAltScreen()
	for {
		if sess.ChatFull {
			ed.Prompt = fullChatPrompt()
			ed.regionTop = sess.fullPromptRow
			_, h := termWH()
			ed.regionBot = h - 1
			ed.CaptureMouse = true
		} else {
			ed.Prompt = green("you> ")
			ed.regionTop, ed.regionBot = 0, 0
			ed.CaptureMouse = false
		}
		ed.Cwd = sess.Cwd
		line, eof, err := ed.ReadLine()
		if err != nil {
			return err
		}
		if eof {
			break
		}
		if line == "" {
			continue // empty Enter does not exit
		}
		ed.PushHistory(line)

		handled, exit, err := handleRunSlash(sess, line, clearHistory)
		if err != nil {
			fmt.Fprintln(os.Stderr, FormatError(err.Error()))
			continue
		}
		if exit {
			break
		}
		if handled {
			ed.Cwd = sess.Cwd
			_ = saveRunPersist(sess)
			if sess.ChatFull {
				sess.refreshFullChrome()
			}
			continue
		}

		sess.pushUndo()

		if sess.Tools && looksLikeDirectList(line) {
			name, args, out, lerr := runDirectList(sess.Cwd, line)
			printToolCall(sess.out(), name, args)
			printToolResult(sess.out(), name, out, lerr)
			result := out
			if lerr != nil {
				result = "error: " + lerr.Error()
			}
			if result == "" {
				result = "(empty)"
			}
			tc := agentToolCall{ID: "direct-list", Type: "function"}
			tc.Function.Name = name
			tc.Function.Arguments = args
			sess.Agent = append(sess.Agent,
				agentMsg{Role: "user", Content: line},
				agentMsg{Role: "assistant", Content: "", ToolCalls: []agentToolCall{tc}},
				agentMsg{Role: "tool", Content: result, ToolCallID: "direct-list", Name: name},
				agentMsg{Role: "assistant", Content: "listed " + summarizeToolArgs(name, args)},
			)
			fmt.Fprintln(sess.out())
			_ = saveRunPersist(sess)
			if sess.ChatFull {
				sess.refreshFullChrome()
			}
			continue
		}

		if sess.Tools {
			err := sess.runTurnWithWake(true, line)
			if errors.Is(err, errTurnCanceled) {
				clearThinkingLine()
				fmt.Fprintln(sess.out(), dim("cancelled"))
			} else if err != nil {
				sess.noteError(err)
			}
			fmt.Fprintln(sess.out())
			_ = saveRunPersist(sess)
			if sess.ChatFull {
				sess.refreshFullChrome()
			}
			continue
		}

		sess.Chat = append(sess.Chat, chatMsg{Role: "user", Content: line})
		fmt.Fprint(sess.out(), dim("thinking…"))
		_ = os.Stdout.Sync()
		err = sess.runTurnWithWake(false, line)
		if errors.Is(err, errTurnCanceled) {
			clearThinkingLine()
			fmt.Fprintln(sess.out(), dim("cancelled"))
			if n := len(sess.Chat); n > 0 && sess.Chat[n-1].Role == "user" {
				sess.Chat = sess.Chat[:n-1]
			}
			continue
		}
		if err != nil {
			clearThinkingLine()
			sess.noteError(err)
			if n := len(sess.Chat); n > 0 && sess.Chat[n-1].Role == "user" {
				sess.Chat = sess.Chat[:n-1]
			}
			continue
		}
		fmt.Fprintln(sess.out())
		_ = saveRunPersist(sess)
		if sess.ChatFull {
			sess.refreshFullChrome()
		}
	}
	return nil
}

func chatOnce(target EndpointTarget, prompt string, stream bool) error {
	msgs := []chatMsg{{Role: "user", Content: prompt}}
	if stream {
		fmt.Fprint(os.Stdout, dim("thinking…"))
		_ = os.Stdout.Sync()
	}
	reply, _, err := chatCompletions(context.Background(), target, msgs, stream, os.Stdout)
	if err != nil {
		if stream {
			clearThinkingLine()
		}
		return err
	}
	_ = reply
	return nil
}

type chatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatReq struct {
	Model         string      `json:"model"`
	Messages      []chatMsg   `json:"messages"`
	Stream        bool        `json:"stream,omitempty"`
	Temperature   *float64    `json:"temperature,omitempty"`
	TopP          *float64    `json:"top_p,omitempty"`
	MaxTokens     *int        `json:"max_tokens,omitempty"`
	StreamOptions *streamOpts `json:"stream_options,omitempty"`
}

type streamOpts struct {
	IncludeUsage bool `json:"include_usage"`
}

func chatCompletions(ctx context.Context, target EndpointTarget, msgs []chatMsg, stream bool, w io.Writer) (string, tokenUsage, error) {
	base := strings.TrimRight(target.BaseURL, "/")
	url := base + "/chat/completions"
	payload := chatReq{
		Model:    target.Model,
		Messages: msgs,
		Stream:   stream,
	}
	if stream {
		payload.StreamOptions = &streamOpts{IncludeUsage: true}
	}
	if s := target.Sampling; s != nil {
		payload.Temperature = s.Temperature
		payload.TopP = s.TopP
		payload.MaxTokens = s.MaxTokens
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", tokenUsage{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", tokenUsage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if target.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+target.APIKey)
	}

	if w == nil {
		w = os.Stdout
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	res, err := client.Do(req)
	if err != nil {
		return "", tokenUsage{}, fmt.Errorf("chat request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		msg := strings.TrimSpace(string(body))
		if len(msg) > 400 {
			msg = msg[:400] + "…"
		}
		return "", tokenUsage{}, fmt.Errorf("HTTP %d from %s: %s", res.StatusCode, base, msg)
	}

	ct := res.Header.Get("Content-Type")
	if stream && strings.Contains(ct, "text/event-stream") {
		return readSSEChat(res.Body, w)
	}
	// Non-stream (or upstream ignored stream=true)
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return "", tokenUsage{}, err
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content          string          `json:"content"`
				Reasoning        json.RawMessage `json:"reasoning"`
				ReasoningContent json.RawMessage `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
		Usage json.RawMessage `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", tokenUsage{}, fmt.Errorf("decode chat response: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return "", tokenUsage{}, fmt.Errorf("upstream: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", tokenUsage{}, fmt.Errorf("empty completion")
	}
	msg := parsed.Choices[0].Message
	rs := &replyStream{w: w}
	rs.addReasoning(jsonStringish(msg.ReasoningContent))
	rs.addReasoning(jsonStringish(msg.Reasoning))
	rs.addContent(msg.Content)
	rs.finish()
	return rs.answerText(), parseTokenUsage(parsed.Usage), nil
}

func clearThinkingLine() {
	clearThinkingOn(os.Stdout)
}

func clearThinkingOn(w io.Writer) {
	fmt.Fprint(w, "\r\033[K")
}

func normalizeStreamPiece(s string) string {
	return strings.ReplaceAll(s, "\r", "")
}
