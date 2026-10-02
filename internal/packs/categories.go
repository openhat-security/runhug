package packs

import "strings"

// Category describes one downloadable index pack / search type.
type Category struct {
	ID       string // stable id used in filenames and metadata keys
	Title    string
	Pipeline string // Hub pipeline_tag (empty = any / filter-only)
	Filter   string // Hub filter= tag (e.g. gguf)
	// ExtraPipelines are fetched and merged when Pipeline alone is insufficient
	// (e.g. video = text-to-video + image-to-video).
	ExtraPipelines []string
}

// TypeSpec is a broad --type bucket or Hub pipeline alias.
type TypeSpec struct {
	ID          string
	Title       string
	Pipelines   []string // Hub pipeline_tag values (OR)
	Filter      string   // Hub filter= (e.g. gguf)
	Aliases     []string // alternate --type / lookup names
	ReleasePack bool     // included in DefaultCategories / release builds
}

// catalog is the shared type taxonomy for search --type and pack builds.
func catalog() []TypeSpec {
	return []TypeSpec{
		{
			ID: "text-generation", Title: "Text Generation (LLMs)",
			Pipelines:   []string{"text-generation"},
			Aliases:     []string{"llm", "llms", "text-to-text", "chat"},
			ReleasePack: true,
		},
		{
			ID: "vision", Title: "Vision / Multimodal",
			Pipelines: []string{
				"image-text-to-text", "visual-question-answering",
				"image-to-text", "any-to-any", "document-question-answering",
			},
			Aliases:     []string{"multimodal", "vlm", "vision-language"},
			ReleasePack: true,
		},
		{
			ID: "text-to-image", Title: "Text to Image",
			Pipelines:   []string{"text-to-image", "image-to-image"},
			Aliases:     []string{"image", "diffusion", "img"},
			ReleasePack: true,
		},
		{
			ID: "video", Title: "Video (text/image → video)",
			Pipelines:   []string{"text-to-video", "image-to-video"},
			Aliases:     []string{"text-to-video"},
			ReleasePack: true,
		},
		{
			ID: "audio", Title: "Audio (TTS / ASR / music)",
			Pipelines: []string{
				"text-to-speech", "automatic-speech-recognition", "text-to-audio",
			},
			Aliases:     []string{"speech", "tts", "asr"},
			ReleasePack: true,
		},
		{
			ID: "embeddings", Title: "Embeddings / feature extraction",
			Pipelines:   []string{"feature-extraction", "sentence-similarity"},
			Aliases:     []string{"embedding", "feature-extraction", "sentence-similarity"},
			ReleasePack: true,
		},
		{
			ID: "gguf", Title: "GGUF (local weights)",
			Filter: "gguf", Aliases: []string{"llama-cpp", "llamacpp"},
			ReleasePack: true,
		},
		// First-class Hub pipeline tags (search --type; not separate release packs by default)
		hubType("fill-mask", "Fill-Mask"),
		hubType("token-classification", "Token Classification"),
		hubType("text-classification", "Text Classification"),
		hubType("question-answering", "Question Answering"),
		hubType("summarization", "Summarization"),
		hubType("translation", "Translation"),
		hubType("text2text-generation", "Text2Text Generation"),
		hubType("reinforcement-learning", "Reinforcement Learning"),
		hubType("tabular-classification", "Tabular Classification"),
		hubType("tabular-regression", "Tabular Regression"),
		hubType("object-detection", "Object Detection"),
		hubType("image-classification", "Image Classification"),
		hubType("image-segmentation", "Image Segmentation"),
		hubType("depth-estimation", "Depth Estimation"),
		hubType("image-to-image", "Image to Image"),
		hubType("unconditional-image-generation", "Unconditional Image Generation"),
		hubType("zero-shot-classification", "Zero-Shot Classification"),
		hubType("zero-shot-image-classification", "Zero-Shot Image Classification"),
		hubType("zero-shot-object-detection", "Zero-Shot Object Detection"),
		hubType("text-to-3d", "Text to 3D"),
		hubType("image-to-3d", "Image to 3D"),
		hubType("mask-generation", "Mask Generation"),
		hubType("table-question-answering", "Table Question Answering"),
	}
}

func hubType(id, title string) TypeSpec {
	return TypeSpec{ID: id, Title: title, Pipelines: []string{id}}
}

// DefaultCategories is the release/install pack set (ReleasePack types).
func DefaultCategories() []Category {
	var out []Category
	for _, t := range catalog() {
		if !t.ReleasePack {
			continue
		}
		out = append(out, typeToCategory(t))
	}
	return out
}

func typeToCategory(t TypeSpec) Category {
	c := Category{ID: t.ID, Title: t.Title, Filter: t.Filter}
	if len(t.Pipelines) > 0 {
		c.Pipeline = t.Pipelines[0]
		if len(t.Pipelines) > 1 {
			c.ExtraPipelines = append([]string{}, t.Pipelines[1:]...)
		}
	}
	return c
}

// AllTypes returns every type spec (release packs + Hub pipeline ids).
func AllTypes() []TypeSpec {
	return catalog()
}

// TypeIDs returns all type ids (canonical, not aliases).
func TypeIDs() []string {
	specs := catalog()
	out := make([]string, len(specs))
	for i, t := range specs {
		out[i] = t.ID
	}
	return out
}

// LookupType resolves a --type id or alias (case-insensitive).
func LookupType(id string) (TypeSpec, bool) {
	key := strings.ToLower(strings.TrimSpace(id))
	if key == "" {
		return TypeSpec{}, false
	}
	for _, t := range catalog() {
		if strings.EqualFold(t.ID, key) {
			return t, true
		}
		for _, a := range t.Aliases {
			if strings.EqualFold(a, key) {
				return t, true
			}
		}
	}
	return TypeSpec{}, false
}

// ExpandType returns Hub pipelines and filter for a type id/alias.
func ExpandType(id string) (pipelines []string, filter string, ok bool) {
	t, ok := LookupType(id)
	if !ok {
		return nil, "", false
	}
	return append([]string{}, t.Pipelines...), t.Filter, true
}

// ResolveTypeAndTask combines --type (broad) with --task (exact pipeline_tag).
// task should already be resolved (not "auto"). Empty type → task-only.
// Incompatible pairs return an error message as the third return (ok=false).
func ResolveTypeAndTask(typeID, task string) (pipelines []string, filter string, errMsg string) {
	task = strings.ToLower(strings.TrimSpace(task))
	typeID = strings.TrimSpace(typeID)

	if typeID == "" {
		if task == "" || task == "any" || task == "auto" {
			return nil, "", ""
		}
		return []string{task}, "", ""
	}

	pipes, filt, ok := ExpandType(typeID)
	if !ok {
		return nil, "", "unknown --type " + typeID + " (try: runhug packs list)"
	}
	if task == "" || task == "any" || task == "auto" {
		return pipes, filt, ""
	}
	if filt != "" && len(pipes) == 0 {
		// filter-only type (gguf): task further constrains via pipeline if set
		return []string{task}, filt, ""
	}
	for _, p := range pipes {
		if strings.EqualFold(p, task) {
			return []string{task}, filt, ""
		}
	}
	return nil, "", "--task " + task + " is not in --type " + typeID + " pipelines"
}

// LookupCategory returns a release pack category by id or alias.
// Non-release Hub pipeline types are not returned here (use LookupType).
func LookupCategory(id string) (Category, bool) {
	t, ok := LookupType(id)
	if !ok || !t.ReleasePack {
		return Category{}, false
	}
	return typeToCategory(t), true
}

// CategoryFromType returns a Category for any type (release or Hub tag), for indexing.
func CategoryFromType(id string) (Category, bool) {
	t, ok := LookupType(id)
	if !ok {
		return Category{}, false
	}
	return typeToCategory(t), true
}

// MatchLabel describes Hub pipelines / filters for a category (list / help).
func MatchLabel(c Category) string {
	if c.Filter != "" {
		return "filter=" + c.Filter
	}
	parts := make([]string, 0, 1+len(c.ExtraPipelines))
	if c.Pipeline != "" {
		parts = append(parts, c.Pipeline)
	}
	parts = append(parts, c.ExtraPipelines...)
	return strings.Join(parts, "+")
}

// CategoryIDs returns stable release-pack ids in default order.
func CategoryIDs() []string {
	cats := DefaultCategories()
	out := make([]string, len(cats))
	for i, c := range cats {
		out[i] = c.ID
	}
	return out
}

// CanonicalTypeID returns the canonical pack/type id for an alias, or "".
func CanonicalTypeID(id string) string {
	t, ok := LookupType(id)
	if !ok {
		return ""
	}
	return t.ID
}
