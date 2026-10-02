package packs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

const ManifestVersion = 1
const ManifestFilename = "index-manifest.json"

// Manifest lists category packs attached to a GitHub Release.
type Manifest struct {
	Version     int        `json:"version"`
	GeneratedAt string     `json:"generated_at"` // RFC3339
	SourceRepo  string     `json:"source_repo,omitempty"`
	Packs       []PackInfo `json:"packs"`
}

// PackInfo describes one release asset DB.
type PackInfo struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Pipeline       string `json:"pipeline,omitempty"`
	Filter         string `json:"filter,omitempty"`
	Rows           int    `json:"rows"`
	HubTotal       int    `json:"hub_total,omitempty"`       // Hub models matching category (approx at build)
	QualitySkipped int    `json:"quality_skipped,omitempty"` // below min likes/downloads during build
	SizeBytes      int64  `json:"size_bytes"`
	SHA256         string `json:"sha256"`
	DBFilename     string `json:"db_filename"`
	Watermark      string `json:"watermark"` // ISO8601 max lastModified indexed
}

// DBFilenameFor returns the release asset name for a category id.
func DBFilenameFor(id string) string {
	return "index-" + id + ".db"
}

// DeltaFilenameFor returns the optional delta JSONL asset name.
func DeltaFilenameFor(id string) string {
	return "index-" + id + "-delta.jsonl"
}

// ParseManifest decodes a manifest from JSON bytes.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if m.Version == 0 {
		m.Version = ManifestVersion
	}
	return &m, nil
}

// LoadManifest reads and parses a manifest file.
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseManifest(data)
}

// WriteManifest writes the manifest as indented JSON.
func WriteManifest(path string, m *Manifest) error {
	if m.Version == 0 {
		m.Version = ManifestVersion
	}
	if m.GeneratedAt == "" {
		m.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

// FindPack returns the pack entry for id, or false.
func (m *Manifest) FindPack(id string) (PackInfo, bool) {
	for _, p := range m.Packs {
		if p.ID == id {
			return p, true
		}
	}
	return PackInfo{}, false
}

// FileSHA256 returns the hex-encoded SHA-256 of a file.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifySHA256 checks path against an expected hex digest.
func VerifySHA256(path, want string) error {
	got, err := FileSHA256(path)
	if err != nil {
		return err
	}
	if !equalFoldHex(got, want) {
		return fmt.Errorf("sha256 mismatch for %s: got %s want %s", path, got, want)
	}
	return nil
}

func equalFoldHex(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'F' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'F' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
