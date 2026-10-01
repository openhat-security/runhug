package packs

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/hf"
	"github.com/adamsiwiec1/runhug/internal/index"
)

// UpsertModels inserts/updates models and returns count + max lastModified.
func UpsertModels(dest *index.Index, models []hf.Model) (int, time.Time, error) {
	return UpsertModelsWithPack(dest, models, "")
}

// UpsertModelsWithPack upserts models and optionally tags pack membership.
func UpsertModelsWithPack(dest *index.Index, models []hf.Model, packID string) (int, time.Time, error) {
	var maxLM time.Time
	n := 0
	for _, m := range models {
		var err error
		if packID != "" {
			err = dest.InsertModelWithPack(m, packID)
		} else {
			err = dest.InsertModel(m)
		}
		if err != nil {
			return n, maxLM, fmt.Errorf("upsert %s: %w", m.RepoID(), err)
		}
		n++
		if lm := parseLM(m.LastModified); lm.After(maxLM) {
			maxLM = lm
		}
	}
	return n, maxLM, nil
}

// MergePackDB upserts all models from packPath into dest Index.
// Returns rows upserted and the max lastModified watermark (RFC3339 or empty).
func MergePackDB(dest *index.Index, packPath string) (int, string, error) {
	return MergePackDBWithID(dest, packPath, "")
}

// MergePackDBWithID merges a pack and records membership under packID when set.
func MergePackDBWithID(dest *index.Index, packPath, packID string) (int, string, error) {
	src, err := index.OpenReadOnly(packPath)
	if err != nil {
		return 0, "", err
	}
	defer src.Close()

	models, err := src.AllModels()
	if err != nil {
		return 0, "", err
	}
	n, maxLM, err := UpsertModelsWithPack(dest, models, packID)
	if err != nil {
		return n, "", err
	}
	return n, formatWatermark(maxLM), nil
}

// ApplyDeltaJSONL reads newline-delimited hf.Model JSON and upserts into dest.
func ApplyDeltaJSONL(dest *index.Index, path string) (int, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()

	var models []hf.Model
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var m hf.Model
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			return 0, "", fmt.Errorf("delta jsonl: %w", err)
		}
		models = append(models, m)
	}
	if err := sc.Err(); err != nil {
		return 0, "", err
	}
	n, maxLM, err := UpsertModels(dest, models)
	if err != nil {
		return n, "", err
	}
	return n, formatWatermark(maxLM), nil
}

// MetadataKeyWatermark is the metadata key for a category watermark.
func MetadataKeyWatermark(categoryID string) string {
	return "pack:" + categoryID + ":watermark"
}

// MetadataKeyInstalled marks a category as selected/installed.
func MetadataKeyInstalled(categoryID string) string {
	return "pack:" + categoryID + ":installed"
}

func parseLM(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	return time.Time{}
}

func formatWatermark(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
