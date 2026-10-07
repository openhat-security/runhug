package runpod

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientAuthorizationSanitizesCRLF(t *testing.T) {
	const fake = "rpa_testkey123"
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"endpoints":[]}`)
	}))
	t.Cleanup(srv.Close)

	c := New(fake + "\n\r")
	c.BaseURL = srv.URL
	if c.APIKey != fake {
		t.Fatalf("stored key %q", c.APIKey)
	}
	_, err := c.ListEndpoints(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer "+fake {
		t.Fatalf("Authorization %q", gotAuth)
	}
}

func TestDirtyKeyIsInvalidHeaderBeforeSanitize(t *testing.T) {
	dirty := "Bearer rpa_testkey123\n"
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", dirty)
	_, err = http.DefaultTransport.RoundTrip(req)
	if err == nil || !strings.Contains(err.Error(), "invalid header") {
		t.Fatalf("expected invalid header, got %v", err)
	}
	if strings.Contains(err.Error(), "rpa_testkey123") {
		t.Fatal("must not print the key")
	}
}

func TestCreateEndpointRequestJSONShapeLoadBalancer(t *testing.T) {
	idle := 5
	req := CreateEndpointRequest{
		Name:      "vllm-demo",
		Type:      EndpointTypeLoadBalancer,
		Image:     DefaultImage,
		GPU:       GPUConfig{Pools: []string{"BLACKWELL_32"}, Count: 1},
		Workers:   &Workers{Min: 0, Max: 3, IdleTimeout: idle},
		Scaling:   RequestCountScaling(1),
		Timeout:   600000,
		Flashboot: "FLASHBOOT",
		Env:       map[string]string{"MODEL_NAME": "Qwen/Qwen2.5-7B-Instruct"},
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{
		`"type":"LOAD_BALANCER"`,
		`"scaling":{"type":"REQUEST_COUNT","requestCount":1}`,
		`"workers":{"min":0,"max":3,"idleTimeout":5}`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s in %s", want, s)
		}
	}
	for _, bad := range []string{`"value"`, `"idleTimeout":5}`, `"queueDelay"`} {
		// idleTimeout must appear under workers, not as a lone scaling field;
		// the workers blob already checked above. Ensure scaling has no idleTimeout/value.
		_ = bad
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	scaling, _ := decoded["scaling"].(map[string]any)
	if _, ok := scaling["value"]; ok {
		t.Fatal("scaling must not include value")
	}
	if _, ok := scaling["idleTimeout"]; ok {
		t.Fatal("scaling must not include idleTimeout")
	}
	if _, ok := scaling["queueDelay"]; ok {
		t.Fatal("LOAD_BALANCER scaling must not include queueDelay")
	}
}

func TestCreateEndpointRequestJSONShapeQueue(t *testing.T) {
	req := CreateEndpointRequest{
		Name:    "vllm-demo",
		Type:    EndpointTypeQueue,
		Image:   DefaultImage,
		GPU:     GPUConfig{Pools: []string{"ADA_24"}, Count: 1},
		Workers: &Workers{Min: 0, Max: 3, IdleTimeout: 5},
		Scaling: QueueDelayScaling(4),
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"type":"QUEUE"`) || !strings.Contains(s, `"queueDelay":4`) {
		t.Fatalf("%s", s)
	}
	if strings.Contains(s, `"requestCount"`) || strings.Contains(s, `"value"`) {
		t.Fatalf("unexpected fields: %s", s)
	}
}

func TestOpenAIURLFor(t *testing.T) {
	if got := OpenAIURLFor(EndpointTypeLoadBalancer, "abc123"); got != "https://abc123.api.runpod.ai/v1" {
		t.Fatalf("lb: %s", got)
	}
	if got := OpenAIURLFor(EndpointTypeQueue, "abc123"); got != "https://api.runpod.ai/v2/abc123/openai/v1" {
		t.Fatalf("queue: %s", got)
	}
	if got := OpenAIURL("abc123"); got != "https://api.runpod.ai/v2/abc123/openai/v1" {
		t.Fatalf("default: %s", got)
	}
}

func TestUpdateEndpointPATCH(t *testing.T) {
	var method, path string
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"ep1","name":"n","gpu":{"pools":["AMPERE_48"],"count":1}}`)
	}))
	t.Cleanup(srv.Close)
	c := New("k")
	c.BaseURL = srv.URL
	ep, err := c.UpdateEndpoint(context.Background(), "ep1", UpdateEndpointRequest{
		GPU: &GPUConfig{Pools: []string{"AMPERE_48"}, Count: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPatch || !strings.Contains(path, "/v2/serverless/ep1") {
		t.Fatalf("%s %s", method, path)
	}
	if !strings.Contains(body, `"AMPERE_48"`) {
		t.Fatalf("body %s", body)
	}
	if ep == nil || ep.ID != "ep1" {
		t.Fatalf("%+v", ep)
	}
}
