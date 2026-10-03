package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/version"
)

func cmdRun(args []string) error {
	fs := newFlagSet("run")
	baseURL := fs.String("base-url", "", "OpenAI-compatible base URL (…/v1)")
	apiKeyEnv := fs.String("api-key-env", "", "env var holding the API key (default: stored Runpod key / RUNPOD_API_KEY)")
	serveModel := fs.String("model", "", "served model name for chat completions")
	oneshot := fs.String("q", "", "one-shot prompt then exit")
	stream := fs.Bool("stream", true, "use chat completions streaming when supported")
	yes := fs.Bool("yes", false, "non-interactive: auto-pick when exactly one registry/remote model")
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
	defer cleanup()
	target.BaseURL = NormalizeOpenAIBase(target.BaseURL)

	heading(os.Stdout, "Run")
	printKV(os.Stdout, "base_url", cyan(target.BaseURL))
	printKV(os.Stdout, "model", bold(target.Model))
	printKV(os.Stdout, "source", target.Source)
	if target.APIKey == "" {
		fmt.Fprintln(os.Stdout, dim("No API key resolved — local backends may still work."))
	}
	fmt.Fprintln(os.Stdout)

	if q := strings.TrimSpace(*oneshot); q != "" {
		return chatOnce(target, q, *stream)
	}

	if !stdinIsTTY() {
		return fmt.Errorf("interactive run needs a TTY (or pass -q \"prompt\")")
	}

	fmt.Fprintf(os.Stdout, "%s chat — empty line or Ctrl-D to exit; /model <name> to switch served name\n", bold(version.Name))
	fmt.Fprintln(os.Stdout)

	var history []chatMsg
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for {
		fmt.Fprint(os.Stdout, green("you> "))
		if !in.Scan() {
			fmt.Fprintln(os.Stdout)
			break
		}
		line := strings.TrimSpace(in.Text())
		if line == "" || line == "/exit" || line == "/quit" {
			break
		}
		if strings.HasPrefix(line, "/model ") {
			target.Model = strings.TrimSpace(strings.TrimPrefix(line, "/model "))
			printKV(os.Stdout, "model", bold(target.Model))
			continue
		}
		history = append(history, chatMsg{Role: "user", Content: line})
		reply, err := chatCompletions(context.Background(), target, history, *stream)
		if err != nil {
			fmt.Fprintln(os.Stderr, FormatError(err.Error()))
			// drop failed user turn so retries stay clean
			history = history[:len(history)-1]
			continue
		}
		history = append(history, chatMsg{Role: "assistant", Content: reply})
		if !*stream {
			fmt.Fprintf(os.Stdout, "%s %s\n\n", cyan("assistant>"), reply)
		} else {
			fmt.Fprintln(os.Stdout)
		}
	}
	if err := in.Err(); err != nil {
		return err
	}
	return nil
}

func chatOnce(target EndpointTarget, prompt string, stream bool) error {
	msgs := []chatMsg{{Role: "user", Content: prompt}}
	reply, err := chatCompletions(context.Background(), target, msgs, stream)
	if err != nil {
		return err
	}
	if !stream {
		fmt.Println(reply)
	} else if !strings.HasSuffix(reply, "\n") {
		fmt.Println()
	}
	return nil
}

type chatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatReq struct {
	Model       string    `json:"model"`
	Messages    []chatMsg `json:"messages"`
	Stream      bool      `json:"stream,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
	TopP        *float64  `json:"top_p,omitempty"`
	MaxTokens   *int      `json:"max_tokens,omitempty"`
}

func chatCompletions(ctx context.Context, target EndpointTarget, msgs []chatMsg, stream bool) (string, error) {
	base := strings.TrimRight(target.BaseURL, "/")
	url := base + "/chat/completions"
	payload := chatReq{
		Model:    target.Model,
		Messages: msgs,
		Stream:   stream,
	}
	if s := target.Sampling; s != nil {
		payload.Temperature = s.Temperature
		payload.TopP = s.TopP
		payload.MaxTokens = s.MaxTokens
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if target.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+target.APIKey)
	}

	client := &http.Client{Timeout: 10 * time.Minute}
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("chat request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		msg := strings.TrimSpace(string(body))
		if len(msg) > 400 {
			msg = msg[:400] + "…"
		}
		return "", fmt.Errorf("HTTP %d from %s: %s", res.StatusCode, base, msg)
	}

	ct := res.Header.Get("Content-Type")
	if stream && strings.Contains(ct, "text/event-stream") {
		return readSSEChat(res.Body)
	}
	// Non-stream (or upstream ignored stream=true)
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return "", err
	}
	var parsed struct {
		Choices []struct {
			Message chatMsg `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode chat response: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return "", fmt.Errorf("upstream: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("empty completion")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

func readSSEChat(r io.Reader) (string, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 2<<20)
	var b strings.Builder
	fmt.Fprint(os.Stdout, cyan("assistant> "))
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		piece := chunk.Choices[0].Delta.Content
		if piece == "" {
			continue
		}
		fmt.Fprint(os.Stdout, piece)
		b.WriteString(piece)
	}
	fmt.Fprintln(os.Stdout)
	if err := sc.Err(); err != nil {
		return b.String(), err
	}
	return b.String(), nil
}
