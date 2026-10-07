package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
)

// Compute Engine public service ID (Cloud Billing Catalog).
const computeEngineServiceID = "6F81-5844-456A"

const (
	spotQuoteCacheName = "gcp-spot-gpu-skus.json"
	spotQuoteTTL       = 12 * time.Hour
)

// SpotGPUQuote is one Cloud Billing list price for a Spot/Preemptible GPU SKU.
type SpotGPUQuote struct {
	Key         string    `json:"key"`
	PerGPUUSD   float64   `json:"per_gpu_usd"`
	SKUID       string    `json:"sku_id,omitempty"`
	Description string    `json:"description,omitempty"`
	FetchedAt   time.Time `json:"fetched_at,omitempty"`
}

type skuPage struct {
	SKUs          []billingSKU `json:"skus"`
	NextPageToken string       `json:"nextPageToken"`
}

type billingSKU struct {
	SKUId          string   `json:"skuId"`
	Description    string   `json:"description"`
	ServiceRegions []string `json:"serviceRegions"`
	Category       struct {
		ResourceGroup string `json:"resourceGroup"`
		UsageType     string `json:"usageType"`
	} `json:"category"`
	PricingInfo []struct {
		PricingExpression struct {
			UsageUnitDescription string `json:"usageUnitDescription"`
			TieredRates          []struct {
				UnitPrice struct {
					Units string `json:"units"`
					Nanos int64  `json:"nanos"`
				} `json:"unitPrice"`
			} `json:"tieredRates"`
		} `json:"pricingExpression"`
	} `json:"pricingInfo"`
}

var (
	quoteMu     sync.Mutex
	quoteCache  []SpotGPUQuote
	quoteLoaded bool
)

// SeedSpotQuotesForTest replaces the in-memory Cloud Billing cache.
func SeedSpotQuotesForTest(q []SpotGPUQuote) {
	quoteMu.Lock()
	defer quoteMu.Unlock()
	quoteCache = append([]SpotGPUQuote(nil), q...)
	quoteLoaded = true
}

// SpotQuoteHourly is GPU-count × Cloud Billing Spot GPU SKU $/hr, or 0 if Google
// publishes no matching SKU. vCPU/RAM are not included.
func SpotQuoteHourly(t GPUTarget) float64 {
	per := spotQuotePerGPU(t)
	if per <= 0 {
		return 0
	}
	return scaleSpotHourly(t.MachineType, per)
}

func spotQuotePerGPU(t GPUTarget) float64 {
	key := quoteKey(t)
	if key == "" {
		return 0
	}
	quoteMu.Lock()
	defer quoteMu.Unlock()
	for _, q := range quoteCache {
		if q.Key == key && q.PerGPUUSD > 0 {
			return q.PerGPUUSD
		}
	}
	return 0
}

func quoteKey(t GPUTarget) string {
	mt := strings.ToLower(strings.TrimSpace(t.MachineType))
	n := strings.ToLower(t.Name + " " + t.Reason + " " + t.Accelerator)
	switch {
	case strings.Contains(mt, "a4x") || strings.Contains(n, "b300") || strings.Contains(n, "gb300"):
		return "b300"
	case strings.Contains(mt, "a4-") || strings.Contains(n, "b200"):
		return "b200"
	case strings.Contains(mt, "a3-mega") || strings.Contains(n, "h100") && strings.Contains(n, "mega"):
		return "h100-mega"
	case strings.Contains(mt, "a3-ultra") || strings.Contains(n, "h200"):
		return "h200"
	case strings.Contains(mt, "a3-") || strings.Contains(n, "h100"):
		if strings.Contains(n, "plus") {
			return "h100-plus"
		}
		return "h100-80"
	case strings.Contains(mt, "a2-ultra") || strings.Contains(n, "a100") && strings.Contains(n, "80"):
		return "a100-80"
	case strings.Contains(mt, "a2-") || strings.Contains(n, "a100"):
		return "a100-40"
	case strings.Contains(mt, "g4-") || strings.Contains(n, "rtx") && strings.Contains(n, "6000"):
		return "rtx6000"
	case strings.Contains(mt, "g2-") || strings.Contains(n, "l4"):
		return "l4"
	case strings.Contains(n, "v100"):
		return "v100"
	case strings.Contains(n, "p100"):
		return "p100"
	case strings.Contains(n, "p4"):
		return "p4"
	case strings.Contains(mt, "n1-") || strings.Contains(n, "t4"):
		return "t4"
	default:
		return ""
	}
}

func skuQuoteKey(desc string) string {
	d := strings.ToLower(desc)
	if !isSpotGPUDesc(d) {
		return ""
	}
	switch {
	case strings.Contains(d, "b300") || strings.Contains(d, "gb300"):
		return "b300"
	case strings.Contains(d, "b200"):
		return "b200"
	case strings.Contains(d, "h200"):
		return "h200"
	case strings.Contains(d, "h100") && strings.Contains(d, "mega"):
		return "h100-mega"
	case strings.Contains(d, "h100") && strings.Contains(d, "plus"):
		return "h100-plus"
	case strings.Contains(d, "h100"):
		return "h100-80"
	case strings.Contains(d, "a100") && strings.Contains(d, "80"):
		return "a100-80"
	case strings.Contains(d, "a100"):
		return "a100-40"
	case strings.Contains(d, "rtx") && strings.Contains(d, "6000"):
		return "rtx6000"
	case strings.Contains(d, "nvidia l4"):
		return "l4"
	case strings.Contains(d, "tesla t4"):
		return "t4"
	case strings.Contains(d, "tesla v100"):
		return "v100"
	case strings.Contains(d, "tesla p100"):
		return "p100"
	case strings.Contains(d, "tesla p4"):
		return "p4"
	default:
		return ""
	}
}

func isSpotGPUDesc(d string) bool {
	if strings.Contains(d, "dws") || strings.Contains(d, "commitment") ||
		strings.Contains(d, "reserved") || strings.Contains(d, "calendar") {
		return false
	}
	return strings.Contains(d, "spot") || strings.Contains(d, "preemptible")
}

func skuInRegion(regions []string) bool {
	for _, r := range regions {
		rl := strings.ToLower(r)
		if rl == "us-central1" || rl == "americas" {
			return true
		}
	}
	return false
}

func skuHourlyUSD(s billingSKU) float64 {
	if len(s.PricingInfo) == 0 {
		return 0
	}
	info := s.PricingInfo[len(s.PricingInfo)-1]
	rates := info.PricingExpression.TieredRates
	if len(rates) == 0 {
		return 0
	}
	p := rates[0].UnitPrice
	units := 0.0
	fmt.Sscanf(p.Units, "%f", &units)
	return units + float64(p.Nanos)/1e9
}

func parseSpotGPUQuotes(skus []billingSKU, fetched time.Time) []SpotGPUQuote {
	best := map[string]SpotGPUQuote{}
	rank := func(s billingSKU) int {
		d := strings.ToLower(s.Description)
		n := 0
		if s.Category.UsageType == "Preemptible" {
			n += 2
		}
		if strings.Contains(d, "attached to spot") {
			n += 3
		}
		if strings.Contains(d, "spot preemptible") {
			n += 1
		}
		return n
	}
	prevRank := map[string]int{}
	for _, s := range skus {
		if s.Category.ResourceGroup != "GPU" {
			continue
		}
		if !skuInRegion(s.ServiceRegions) {
			continue
		}
		key := skuQuoteKey(s.Description)
		if key == "" {
			continue
		}
		usd := skuHourlyUSD(s)
		if usd <= 0 {
			continue
		}
		r := rank(s)
		if old, ok := prevRank[key]; ok && r < old {
			continue
		}
		prevRank[key] = r
		best[key] = SpotGPUQuote{
			Key:         key,
			PerGPUUSD:   usd,
			SKUID:       s.SKUId,
			Description: s.Description,
			FetchedAt:   fetched,
		}
	}
	out := make([]SpotGPUQuote, 0, len(best))
	for _, q := range best {
		out = append(out, q)
	}
	return out
}

type quoteFile struct {
	Quotes    []SpotGPUQuote `json:"quotes"`
	FetchedAt time.Time      `json:"fetched_at"`
}

func quoteCachePath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, spotQuoteCacheName), nil
}

func loadQuoteFile() []SpotGPUQuote {
	path, err := quoteCachePath()
	if err != nil {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f quoteFile
	if json.Unmarshal(raw, &f) != nil {
		return nil
	}
	if time.Since(f.FetchedAt) > spotQuoteTTL {
		return f.Quotes // stale still usable if refresh fails
	}
	return f.Quotes
}

func saveQuoteFile(q []SpotGPUQuote, at time.Time) {
	path, err := quoteCachePath()
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	raw, err := json.MarshalIndent(quoteFile{Quotes: q, FetchedAt: at}, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, raw, 0o600)
}

func ensureQuoteCache() {
	quoteMu.Lock()
	if quoteLoaded {
		quoteMu.Unlock()
		return
	}
	quoteMu.Unlock()
	if q := loadQuoteFile(); len(q) > 0 {
		quoteMu.Lock()
		if !quoteLoaded {
			quoteCache = q
			quoteLoaded = true
		}
		quoteMu.Unlock()
	}
}

// RefreshSpotQuotes pulls Compute Engine GPU SKUs from Cloud Billing (ADC).
// Tokens are not logged. On 429/failure, a disk cache is kept if present.
func (c *Client) RefreshSpotQuotes(ctx context.Context) error {
	ensureQuoteCache()
	tok, err := c.accessToken(ctx)
	if err != nil {
		if SpotQuoteHourly(GPUTarget{Name: GPUTypeL4, MachineType: MachineTypeL4}) > 0 {
			return nil
		}
		return fmt.Errorf("Google Cloud Billing SKUs need ADC — gcloud auth application-default login")
	}
	now := time.Now().UTC()
	skus, err := fetchGPUSkus(ctx, tok)
	if err != nil {
		if len(loadQuoteFile()) > 0 || spotQuotePerGPU(GPUTarget{Name: GPUTypeL4, MachineType: MachineTypeL4}) > 0 {
			return nil
		}
		return err
	}
	q := parseSpotGPUQuotes(skus, now)
	if len(q) == 0 {
		return fmt.Errorf("Cloud Billing returned no Spot GPU SKUs for us-central1/Americas")
	}
	quoteMu.Lock()
	quoteCache = q
	quoteLoaded = true
	quoteMu.Unlock()
	saveQuoteFile(q, now)
	return nil
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	out, err := c.runner().Run(ctx, "auth", "application-default", "print-access-token")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func fetchGPUSkus(ctx context.Context, token string) ([]billingSKU, error) {
	client := &http.Client{Timeout: 45 * time.Second}
	var all []billingSKU
	page := ""
	for i := 0; i < 40; i++ {
		u := fmt.Sprintf("https://cloudbilling.googleapis.com/v1/services/%s/skus?pageSize=5000", computeEngineServiceID)
		if page != "" {
			u += "&pageToken=" + url.QueryEscape(page)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 32<<20))
		_ = res.Body.Close()
		if res.StatusCode == http.StatusTooManyRequests {
			return nil, fmt.Errorf("Cloud Billing catalog rate-limited")
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return nil, fmt.Errorf("Cloud Billing catalog HTTP %d", res.StatusCode)
		}
		var p skuPage
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("Cloud Billing catalog json")
		}
		for _, s := range p.SKUs {
			if s.Category.ResourceGroup == "GPU" {
				all = append(all, s)
			}
		}
		page = strings.TrimSpace(p.NextPageToken)
		if page == "" {
			break
		}
	}
	return all, nil
}
