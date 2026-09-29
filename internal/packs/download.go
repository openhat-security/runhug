package packs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/version"
)

// ReleaseClient downloads pack assets from GitHub Releases.
type ReleaseClient struct {
	HTTP    *http.Client
	Repo    string // owner/name
	Token   string // optional GITHUB_TOKEN for higher rate limits
	BaseAPI string // default https://api.github.com
}

func NewReleaseClient(repo string) *ReleaseClient {
	if repo == "" {
		repo = ReleaseRepo()
	}
	return &ReleaseClient{
		HTTP:    &http.Client{Timeout: 5 * time.Minute},
		Repo:    repo,
		BaseAPI: "https://api.github.com",
	}
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// LatestReleaseTag returns the latest published release tag (or empty).
func (c *ReleaseClient) LatestReleaseTag(ctx context.Context) (string, *ghRelease, error) {
	rel, err := c.fetchLatest(ctx)
	if err != nil {
		return "", nil, err
	}
	return rel.TagName, rel, nil
}

func (c *ReleaseClient) fetchLatest(ctx context.Context) (*ghRelease, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimRight(c.BaseAPI, "/"), c.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", version.Name+"/"+version.Version)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no GitHub release found for %s", c.Repo)
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("github releases: HTTP %d: %s", res.StatusCode, trimErr(body))
	}
	var rel ghRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// FetchManifest downloads index-manifest.json from the latest release.
func (c *ReleaseClient) FetchManifest(ctx context.Context) (*Manifest, string, error) {
	rel, err := c.fetchLatest(ctx)
	if err != nil {
		return nil, "", err
	}
	url, err := assetURL(rel, ManifestFilename)
	if err != nil {
		return nil, "", err
	}
	data, err := c.downloadBytes(ctx, url)
	if err != nil {
		return nil, "", err
	}
	m, err := ParseManifest(data)
	if err != nil {
		return nil, "", err
	}
	return m, rel.TagName, nil
}

// DownloadPack downloads a pack DB into destPath and verifies sha256.
func (c *ReleaseClient) DownloadPack(ctx context.Context, info PackInfo, destPath string) error {
	rel, err := c.fetchLatest(ctx)
	if err != nil {
		return err
	}
	name := info.DBFilename
	if name == "" {
		name = DBFilenameFor(info.ID)
	}
	url, err := assetURL(rel, name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}
	tmp := destPath + ".tmp"
	if err := c.downloadFile(ctx, url, tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if info.SHA256 != "" {
		if err := VerifySHA256(tmp, info.SHA256); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	return os.Rename(tmp, destPath)
}

// TryDownloadDelta downloads index-<id>-delta.jsonl if present on the latest
// release. Returns (path, true, nil) when found; ( "", false, nil) when absent.
func (c *ReleaseClient) TryDownloadDelta(ctx context.Context, id, destPath string) (bool, error) {
	rel, err := c.fetchLatest(ctx)
	if err != nil {
		return false, err
	}
	url, err := assetURL(rel, DeltaFilenameFor(id))
	if err != nil {
		return false, nil // asset missing is OK
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return false, err
	}
	tmp := destPath + ".tmp"
	if err := c.downloadFile(ctx, url, tmp); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	if err := os.Rename(tmp, destPath); err != nil {
		return false, err
	}
	return true, nil
}

// FetchAssetBytes downloads a named asset from the latest release.
// Returns (data, tag, err). Missing asset → error mentioning the name.
func (c *ReleaseClient) FetchAssetBytes(ctx context.Context, name string) ([]byte, string, error) {
	rel, err := c.fetchLatest(ctx)
	if err != nil {
		return nil, "", err
	}
	url, err := assetURL(rel, name)
	if err != nil {
		return nil, "", err
	}
	data, err := c.downloadBytes(ctx, url)
	if err != nil {
		return nil, "", err
	}
	return data, rel.TagName, nil
}

func assetURL(rel *ghRelease, name string) (string, error) {
	for _, a := range rel.Assets {
		if a.Name == name {
			if a.BrowserDownloadURL == "" {
				return "", fmt.Errorf("asset %s has empty download URL", name)
			}
			return a.BrowserDownloadURL, nil
		}
	}
	return "", fmt.Errorf("release %s has no asset %q", rel.TagName, name)
}

func (c *ReleaseClient) downloadBytes(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", version.Name+"/"+version.Version)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("download: HTTP %d: %s", res.StatusCode, trimErr(body))
	}
	return body, nil
}

func (c *ReleaseClient) downloadFile(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", version.Name+"/"+version.Version)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return fmt.Errorf("download: HTTP %d: %s", res.StatusCode, trimErr(body))
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, io.LimitReader(res.Body, 512<<20))
	return err
}

func trimErr(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
