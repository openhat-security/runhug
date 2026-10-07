package runpod

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/version"
)

const (
	APIBase      = "https://api.runpod.io"
	OpenAIBase   = "https://api.runpod.ai/v2"
	DefaultImage = "runpod/worker-v1-vllm:v2.27.0"
)

type Client struct {
	HTTP    *http.Client
	APIKey  string
	BaseURL string
}

func New(apiKey string) *Client {
	return &Client{
		HTTP:    &http.Client{Timeout: 60 * time.Second},
		APIKey:  config.SanitizeAPIKey(apiKey),
		BaseURL: APIBase,
	}
}

type GPU struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Pool         *string      `json:"pool"`
	Memory       float64      `json:"memory"`
	Availability string       `json:"availability"`
	Price        Price        `json:"price"`
	DataCenters  []DataCenter `json:"dataCenters"`
	Manufacturer string       `json:"manufacturer"`
}

type Price struct {
	Community  float64 `json:"community"`
	Secure     float64 `json:"secure"`
	Serverless float64 `json:"serverless"`
}

type DataCenter struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Availability string `json:"availability"`
}

type Endpoint struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Type          string            `json:"type"`
	Image         string            `json:"image"`
	Disk          int               `json:"disk"`
	Env           map[string]string `json:"env"`
	GPU           *GPUConfig        `json:"gpu"`
	Workers       *Workers          `json:"workers"`
	Scaling       *Scaling          `json:"scaling"`
	Timeout       int               `json:"timeout"`
	Flashboot     string            `json:"flashboot"`
	CreatedAt     string            `json:"createdAt"`
	DataCenterIDs []string          `json:"dataCenterIds"`
	RequestURLs   *RequestURLs      `json:"requestUrls"`
}

type GPUConfig struct {
	Pools []string `json:"pools"`
	Count int      `json:"count,omitempty"`
}

type Workers struct {
	Min         int `json:"min"`
	Max         int `json:"max"`
	IdleTimeout int `json:"idleTimeout,omitempty"`
}

// Scaling is a discriminated union on Type (QUEUE_DELAY | REQUEST_COUNT).
// Use QueueDelay OR RequestCount — never Value/IdleTimeout (those belong on Workers).
type Scaling struct {
	Type         string   `json:"type"`
	QueueDelay   *float64 `json:"queueDelay,omitempty"`
	RequestCount *int     `json:"requestCount,omitempty"`
}

type RequestURLs struct {
	Run     string `json:"run"`
	RunSync string `json:"runSync"`
	Base    string `json:"base"`
}

type CreateEndpointRequest struct {
	Name      string            `json:"name"`
	Type      string            `json:"type"` // QUEUE | LOAD_BALANCER (required by Runpod v2)
	Image     string            `json:"image"`
	Disk      int               `json:"disk,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	GPU       GPUConfig         `json:"gpu"`
	Workers   *Workers          `json:"workers,omitempty"`
	Scaling   *Scaling          `json:"scaling,omitempty"`
	Timeout   int               `json:"timeout,omitempty"`
	Flashboot string            `json:"flashboot,omitempty"`
}

const (
	EndpointTypeQueue        = "QUEUE"
	EndpointTypeLoadBalancer = "LOAD_BALANCER"
	ScalingTypeQueueDelay    = "QUEUE_DELAY"
	ScalingTypeRequestCount  = "REQUEST_COUNT"
)

func RequestCountScaling(n int) *Scaling {
	if n < 1 {
		n = 1
	}
	return &Scaling{Type: ScalingTypeRequestCount, RequestCount: &n}
}

func QueueDelayScaling(seconds float64) *Scaling {
	if seconds < 0.5 {
		seconds = 4
	}
	return &Scaling{Type: ScalingTypeQueueDelay, QueueDelay: &seconds}
}

type Problem struct {
	Title  string   `json:"title"`
	Status int      `json:"status"`
	Detail string   `json:"detail"`
	Errors []string `json:"errors"`
}

func (c *Client) ListGPUs(ctx context.Context) ([]GPU, error) {
	return c.listGPUs(ctx, "SERVERLESS")
}

// ListPodGPUs returns the POD-priced catalog (the one pod creation stocks from).
func (c *Client) ListPodGPUs(ctx context.Context) ([]GPU, error) {
	return c.listGPUs(ctx, "POD")
}

func (c *Client) listGPUs(ctx context.Context, product string) ([]GPU, error) {
	q := url.Values{}
	q.Set("include", "AVAILABILITY")
	q.Set("product", product)
	var wrap struct {
		GPUs  []GPU `json:"gpus"`
		Items []GPU `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/v2/catalog/gpus?"+q.Encode(), nil, &wrap); err != nil {
		return nil, err
	}
	if len(wrap.GPUs) > 0 {
		return wrap.GPUs, nil
	}
	return wrap.Items, nil
}

func (c *Client) ListEndpoints(ctx context.Context, limit int) ([]Endpoint, error) {
	if limit <= 0 {
		limit = 50
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	var wrap struct {
		Endpoints []Endpoint `json:"endpoints"`
		Items     []Endpoint `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/v2/serverless?"+q.Encode(), nil, &wrap); err != nil {
		return nil, err
	}
	if len(wrap.Endpoints) > 0 {
		return wrap.Endpoints, nil
	}
	return wrap.Items, nil
}

func (c *Client) GetEndpoint(ctx context.Context, id string) (*Endpoint, error) {
	var ep Endpoint
	if err := c.do(ctx, http.MethodGet, "/v2/serverless/"+url.PathEscape(id), nil, &ep); err != nil {
		return nil, err
	}
	return &ep, nil
}

func (c *Client) CreateEndpoint(ctx context.Context, req CreateEndpointRequest) (*Endpoint, error) {
	var ep Endpoint
	if err := c.do(ctx, http.MethodPost, "/v2/serverless", req, &ep); err != nil {
		return nil, err
	}
	return &ep, nil
}

func (c *Client) DeleteEndpoint(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v2/serverless/"+url.PathEscape(id), nil, nil)
}

type UpdateEndpointRequest struct {
	GPU     *GPUConfig `json:"gpu,omitempty"`
	Workers *Workers   `json:"workers,omitempty"`
}

func (c *Client) UpdateEndpoint(ctx context.Context, id string, req UpdateEndpointRequest) (*Endpoint, error) {
	var ep Endpoint
	if err := c.do(ctx, http.MethodPatch, "/v2/serverless/"+url.PathEscape(id), req, &ep); err != nil {
		return nil, err
	}
	return &ep, nil
}

// OpenAIURL returns the queue-based OpenAI base URL for worker-v1-vllm.
// Prefer OpenAIURLFor when the endpoint type is known.
func OpenAIURL(endpointID string) string {
	return OpenAIURLFor(EndpointTypeQueue, endpointID)
}

// OpenAIURLFor returns the OpenAI-compatible base URL for an endpoint type.
// LOAD_BALANCER (FastAPI LB image): https://{id}.api.runpod.ai/v1
// QUEUE (worker-v1-vllm default): https://api.runpod.ai/v2/{id}/openai/v1
func OpenAIURLFor(endpointType, endpointID string) string {
	id := strings.TrimSpace(endpointID)
	switch strings.ToUpper(strings.TrimSpace(endpointType)) {
	case EndpointTypeLoadBalancer:
		return "https://" + id + ".api.runpod.ai/v1"
	default:
		return strings.TrimRight(OpenAIBase, "/") + "/" + id + "/openai/v1"
	}
}

func (c *Client) do(ctx context.Context, method, path string, body any, dest any) error {
	base := c.BaseURL
	if base == "" {
		base = APIBase
	}
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", version.Name+"/"+version.Version)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key := config.SanitizeAPIKey(c.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return err
	}
	if res.StatusCode == http.StatusNoContent {
		return nil
	}
	if res.StatusCode >= 300 {
		return formatAPIError(res.StatusCode, raw)
	}
	if dest == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("runpod: decode: %w", err)
	}
	return nil
}

func formatAPIError(status int, raw []byte) error {
	var p Problem
	if json.Unmarshal(raw, &p) == nil && (p.Detail != "" || p.Title != "" || len(p.Errors) > 0) {
		msg := strings.TrimSpace(p.Title + ": " + p.Detail)
		if len(p.Errors) > 0 {
			msg += " (" + strings.Join(p.Errors, "; ") + ")"
		}
		return fmt.Errorf("runpod: HTTP %d: %s", status, strings.Trim(msg, ": "))
	}
	s := strings.TrimSpace(string(raw))
	if len(s) > 400 {
		s = s[:400] + "…"
	}
	if s == "" {
		s = http.StatusText(status)
	}
	return fmt.Errorf("runpod: HTTP %d: %s", status, s)
}
