package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/adamsiwiec1/runhug/internal/hf"
)

// Index manages the local model search database.
type Index struct {
	db   *sql.DB
	path string
}

// IndexedModel represents a model stored in the local index.
type IndexedModel struct {
	ID           string
	Author       string
	Description  string
	Tags         []string
	Likes        int64
	Downloads    int64
	LibraryName  string
	License      string
	PipelineTag  string
	LastModified time.Time
	IndexedAt    time.Time
}

// Open opens or creates the search index at the given path.
func Open(path string) (*Index, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create index directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	idx := &Index{db: db, path: path}
	if err := idx.createTables(); err != nil {
		db.Close()
		return nil, err
	}

	return idx, nil
}

// Close closes the index database.
func (idx *Index) Close() error {
	return idx.db.Close()
}

func (idx *Index) createTables() error {
	schema := `
CREATE TABLE IF NOT EXISTS models (
	id TEXT PRIMARY KEY,
	author TEXT,
	description TEXT,
	tags TEXT,
	likes INTEGER DEFAULT 0,
	downloads INTEGER DEFAULT 0,
	library_name TEXT,
	license TEXT,
	pipeline_tag TEXT,
	last_modified INTEGER,
	indexed_at INTEGER DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_models_author ON models(author);
CREATE INDEX IF NOT EXISTS idx_models_likes ON models(likes DESC);
CREATE INDEX IF NOT EXISTS idx_models_downloads ON models(downloads DESC);
CREATE INDEX IF NOT EXISTS idx_models_library ON models(library_name);
CREATE INDEX IF NOT EXISTS idx_models_license ON models(license);
CREATE INDEX IF NOT EXISTS idx_models_pipeline ON models(pipeline_tag);
CREATE INDEX IF NOT EXISTS idx_models_id_lower ON models(LOWER(id));

CREATE TABLE IF NOT EXISTS metadata (
	key TEXT PRIMARY KEY,
	value TEXT
);

CREATE TABLE IF NOT EXISTS pack_membership (
	model_id TEXT NOT NULL,
	pack_id TEXT NOT NULL,
	PRIMARY KEY (model_id, pack_id)
);
CREATE INDEX IF NOT EXISTS idx_pack_membership_pack ON pack_membership(pack_id);
`
	_, err := idx.db.Exec(schema)
	return err
}

// InsertModel adds or updates a model in the index.
func (idx *Index) InsertModel(m hf.Model) error {
	id := m.RepoID()
	if id == "" {
		return nil
	}
	tagsJSON, _ := json.Marshal(m.Tags)
	lastMod := time.Time{}
	if m.LastModified != "" {
		lastMod, _ = time.Parse(time.RFC3339, m.LastModified)
	}

	_, err := idx.db.Exec(`
		INSERT INTO models (id, author, description, tags, likes, downloads, library_name, license, pipeline_tag, last_modified)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			author = excluded.author,
			description = excluded.description,
			tags = excluded.tags,
			likes = excluded.likes,
			downloads = excluded.downloads,
			library_name = excluded.library_name,
			license = excluded.license,
			pipeline_tag = excluded.pipeline_tag,
			last_modified = excluded.last_modified,
			indexed_at = strftime('%s', 'now')
	`, id, m.Author, m.CardDescription(), string(tagsJSON), m.Likes, m.Downloads,
		m.LibraryName, m.License(), m.PipelineTag, lastMod.Unix())

	return err
}

// Search searches the index using pattern matching.
func (idx *Index) Search(ctx context.Context, query string, filters SearchFilters) ([]hf.Model, error) {
	// Build search tokens
	tokens := buildSearchTokens(query)

	// Build SQL query with filters
	sqlQuery := `
		SELECT m.id, m.author, m.description, m.tags, m.likes, m.downloads,
		       m.library_name, m.license, m.pipeline_tag, m.last_modified
		FROM models m
		WHERE 1=1
	`
	var args []interface{}

	// Add text search conditions
	if len(tokens) > 0 {
		var conditions []string
		for _, tok := range tokens {
			likePattern := "%" + tok + "%"
			conditions = append(conditions, "(LOWER(m.id) LIKE ? OR LOWER(m.description) LIKE ? OR LOWER(m.tags) LIKE ?)")
			args = append(args, likePattern, likePattern, likePattern)
		}
		sqlQuery += " AND (" + strings.Join(conditions, " OR ") + ")"
	}

	if filters.Author != "" {
		sqlQuery += " AND m.author = ?"
		args = append(args, filters.Author)
	}
	if filters.Library != "" {
		sqlQuery += " AND m.library_name = ?"
		args = append(args, filters.Library)
	}
	if filters.License != "" {
		sqlQuery += " AND m.license = ?"
		args = append(args, filters.License)
	}
	membershipActive := false
	if filters.PackType != "" {
		var n int
		_ = idx.db.QueryRow(`SELECT COUNT(*) FROM pack_membership WHERE pack_id = ?`, filters.PackType).Scan(&n)
		if n > 0 {
			membershipActive = true
			sqlQuery += " AND EXISTS (SELECT 1 FROM pack_membership pm WHERE pm.model_id = m.id AND pm.pack_id = ?)"
			args = append(args, filters.PackType)
		}
	}
	if !membershipActive {
		if filters.PipelineTag != "" {
			sqlQuery += " AND m.pipeline_tag = ?"
			args = append(args, filters.PipelineTag)
		}
		if len(filters.PipelineTags) > 0 {
			holders := make([]string, len(filters.PipelineTags))
			for i, p := range filters.PipelineTags {
				holders[i] = "?"
				args = append(args, p)
			}
			sqlQuery += " AND m.pipeline_tag IN (" + strings.Join(holders, ",") + ")"
		}
	}
	if filters.Engine != "" {
		// Engine maps to library_name: gguf → "gguf", vllm → "transformers"
		if strings.EqualFold(filters.Engine, "gguf") {
			sqlQuery += " AND m.library_name = 'gguf'"
		} else if strings.EqualFold(filters.Engine, "vllm") {
			sqlQuery += " AND (m.library_name = 'transformers' OR m.library_name = 'safetensors')"
		}
	}
	if filters.Filter != "" {
		sqlQuery += " AND LOWER(m.tags) LIKE ?"
		args = append(args, "%"+strings.ToLower(filters.Filter)+"%")
	}

	// Apply sort order
	switch strings.ToLower(filters.Sort) {
	case "likes":
		sqlQuery += " ORDER BY m.likes DESC"
	case "downloads":
		sqlQuery += " ORDER BY m.downloads DESC"
	default:
		// Relevance: prefer exact matches in id, then by popularity
		sqlQuery += " ORDER BY (CASE WHEN LOWER(m.id) LIKE ? THEN 1 ELSE 2 END), m.likes DESC"
		if len(tokens) > 0 {
			args = append(args, "%"+tokens[0]+"%")
		} else {
			args = append(args, "%")
		}
	}

	if filters.Limit > 0 {
		sqlQuery += fmt.Sprintf(" LIMIT %d", filters.Limit)
	}

	rows, err := idx.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	var models []hf.Model
	for rows.Next() {
		var m hf.Model
		var tagsJSON string
		var lastMod int64
		var desc string

		var license string
		err := rows.Scan(&m.ID, &m.Author, &desc, &tagsJSON, &m.Likes, &m.Downloads,
			&m.LibraryName, &license, &m.PipelineTag, &lastMod)
		if err != nil {
			return nil, fmt.Errorf("scan model: %w", err)
		}

		json.Unmarshal([]byte(tagsJSON), &m.Tags)
		m.Description = desc
		if lastMod > 0 {
			m.LastModified = time.Unix(lastMod, 0).Format(time.RFC3339)
		}
		// Store license in tags for Model.License() to work
		if license != "" {
			m.Tags = append(m.Tags, "license:"+license)
		}

		models = append(models, m)
	}

	return models, rows.Err()
}

// SearchFilters specifies filters for index search.
type SearchFilters struct {
	Author       string
	Library      string
	License      string
	PipelineTag  string   // exact single tag
	PipelineTags []string // OR of pipeline tags (from --type expand)
	PackType     string   // canonical pack/type id; prefers pack_membership when populated
	Engine       string
	Filter       string
	Sort         string
	Limit        int
}

// AddMembership records that modelID belongs to packID (idempotent).
func (idx *Index) AddMembership(modelID, packID string) error {
	modelID = strings.TrimSpace(modelID)
	packID = strings.TrimSpace(packID)
	if modelID == "" || packID == "" {
		return nil
	}
	_, err := idx.db.Exec(`
		INSERT INTO pack_membership (model_id, pack_id) VALUES (?, ?)
		ON CONFLICT(model_id, pack_id) DO NOTHING
	`, modelID, packID)
	return err
}

// InsertModelWithPack upserts the model and records pack membership.
func (idx *Index) InsertModelWithPack(m hf.Model, packID string) error {
	if err := idx.InsertModel(m); err != nil {
		return err
	}
	return idx.AddMembership(m.RepoID(), packID)
}

// HasMembership reports whether modelID is tagged with packID.
func (idx *Index) HasMembership(modelID, packID string) (bool, error) {
	var one int
	err := idx.db.QueryRow(`
		SELECT 1 FROM pack_membership WHERE model_id = ? AND pack_id = ? LIMIT 1
	`, modelID, packID).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// MembershipCount returns how many models are tagged with packID.
func (idx *Index) MembershipCount(packID string) (int, error) {
	var n int
	err := idx.db.QueryRow(`SELECT COUNT(*) FROM pack_membership WHERE pack_id = ?`, packID).Scan(&n)
	return n, err
}

// HasModel reports whether a Hugging Face repo id already exists in the index.
// models.id is the unique primary key (HF repo id, e.g. org/name).
func (idx *Index) HasModel(id string) (bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return false, nil
	}
	var one int
	err := idx.db.QueryRow(`SELECT 1 FROM models WHERE id = ? LIMIT 1`, id).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Count returns the number of models in the index.
func (idx *Index) Count() (int, error) {
	var count int
	err := idx.db.QueryRow("SELECT COUNT(*) FROM models").Scan(&count)
	return count, err
}

// LastUpdate returns the timestamp of the most recent indexed model.
func (idx *Index) LastUpdate() (time.Time, error) {
	var ts int64
	err := idx.db.QueryRow("SELECT MAX(indexed_at) FROM models").Scan(&ts)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(ts, 0), nil
}

// SetMetadata stores a metadata key-value pair.
func (idx *Index) SetMetadata(key, value string) error {
	_, err := idx.db.Exec(`
		INSERT INTO metadata (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}

// GetMetadata retrieves a metadata value.
func (idx *Index) GetMetadata(key string) (string, error) {
	var value string
	err := idx.db.QueryRow("SELECT value FROM metadata WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// buildSearchTokens converts a user query into search tokens.
func buildSearchTokens(query string) []string {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return nil
	}

	// Split into tokens
	tokens := strings.Fields(query)
	var result []string
	for _, tok := range tokens {
		tok = strings.Trim(tok, ",.?!:;\"'+()[]{}")
		if len(tok) >= 2 {
			result = append(result, tok)
		}
	}

	return result
}

// Exists returns true if the index database file exists.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// OpenReadOnly opens an existing index file for reading/merging.
func OpenReadOnly(path string) (*Index, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	idx := &Index{db: db, path: path}
	if err := idx.createTables(); err != nil {
		db.Close()
		return nil, err
	}
	return idx, nil
}

// AllModels returns every model row as hf.Model (for pack merge).
func (idx *Index) AllModels() ([]hf.Model, error) {
	rows, err := idx.db.Query(`
		SELECT id, author, description, tags, likes, downloads,
		       library_name, license, pipeline_tag, last_modified
		FROM models
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var models []hf.Model
	for rows.Next() {
		var m hf.Model
		var tagsJSON, desc, license string
		var lastMod int64
		if err := rows.Scan(&m.ID, &m.Author, &desc, &tagsJSON, &m.Likes, &m.Downloads,
			&m.LibraryName, &license, &m.PipelineTag, &lastMod); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tagsJSON), &m.Tags)
		m.Description = desc
		if lastMod > 0 {
			m.LastModified = time.Unix(lastMod, 0).UTC().Format(time.RFC3339)
		}
		if license != "" {
			m.Tags = append(m.Tags, "license:"+license)
		}
		models = append(models, m)
	}
	return models, rows.Err()
}

// MaxLastModified returns the max last_modified timestamp among models.
func (idx *Index) MaxLastModified() (time.Time, error) {
	var ts sql.NullInt64
	err := idx.db.QueryRow("SELECT MAX(last_modified) FROM models").Scan(&ts)
	if err != nil {
		return time.Time{}, err
	}
	if !ts.Valid || ts.Int64 <= 0 {
		return time.Time{}, nil
	}
	return time.Unix(ts.Int64, 0).UTC(), nil
}

// Watermark returns the preferred update watermark: metadata last_update /
// pack watermark, else MaxLastModified, else LastUpdate (indexed_at).
func (idx *Index) Watermark() (time.Time, error) {
	for _, key := range []string{"watermark", "last_update"} {
		v, err := idx.GetMetadata(key)
		if err != nil {
			return time.Time{}, err
		}
		if v == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t, nil
		}
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return t, nil
		}
	}
	if t, err := idx.MaxLastModified(); err == nil && !t.IsZero() {
		return t, nil
	}
	return idx.LastUpdate()
}
