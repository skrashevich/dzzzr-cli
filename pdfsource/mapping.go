package pdfsource

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const MaxMappingBytes = 4 << 20

// Range selects inclusive 1-based lines. Optional columns are 1-based rune
// positions, inclusive, on the first/last line. Zero means the whole line.
type Range struct {
	Page        int `json:"page"`
	StartLine   int `json:"start_line"`
	EndLine     int `json:"end_line"`
	StartColumn int `json:"start_column,omitzero"`
	EndColumn   int `json:"end_column,omitzero"`
}

type Selector struct {
	Ranges  []Range `json:"ranges,omitempty"`
	Format  string  `json:"format,omitempty"`
	Pattern string  `json:"pattern,omitempty"`
	Group   int     `json:"group,omitzero"`
	// Link and image address PDF objects directly, never model-written URLs.
	Page    int    `json:"page,omitzero"`
	Link    int    `json:"link,omitzero"` // 1-based annotation index
	ImageID string `json:"image_id,omitempty"`
}

type Mapping struct {
	Version      int               `json:"version"`
	SourceSHA256 string            `json:"source_sha256"`
	Output       jsontext.Value    `json:"output"`
	Ignored      []Range           `json:"ignored,omitempty"`
	ImageURLs    map[string]string `json:"image_urls,omitempty"` // IDs bound to URLs returned by the upload tool.
}

type Extraction struct {
	Data            jsontext.Value
	SourceSHA256    string
	PageCount       int
	ReferencedLines int
	IgnoredLines    int
}

// Extract interprets a data-only mapping. No generated scalar content, code,
// shell commands, replacement templates or network lookups are accepted.
func Extract(ctx context.Context, data, mappingJSON []byte) (*Extraction, error) {
	if len(mappingJSON) > MaxMappingBytes {
		return nil, errors.New("mapping exceeds 4 MiB")
	}
	var mapping Mapping
	if err := json.Unmarshal(mappingJSON, &mapping, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("mapping: %w", err)
	}
	if mapping.Version != 1 || mapping.SourceSHA256 != SourceHash(data) {
		return nil, errors.New("mapping version or source_sha256 mismatch; index this PDF again")
	}
	for id, rawURL := range mapping.ImageURLs {
		u, err := url.Parse(rawURL)
		if len(id) != 64 || err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
			return nil, errors.New("image_urls must bind SHA-256 image IDs to absolute HTTP(S) upload URLs")
		}
	}
	pages := map[int]IndexedPage{}
	pageCount := 0
	indexedBytes := 0
	for start := 1; ; start += MaxPages {
		end := start + MaxPages - 1
		if pageCount > 0 {
			end = min(end, pageCount)
		}
		// Zero end allows the first read of a short PDF.
		if start == 1 {
			end = 0
		}
		doc, err := Index(ctx, data, Options{StartPage: start, EndPage: end})
		if err != nil {
			return nil, err
		}
		pageCount = doc.PageCount
		if pageCount > 512 {
			return nil, errors.New("mapping supports at most 512 pages")
		}
		for _, p := range doc.Pages {
			for _, line := range p.Lines {
				indexedBytes += len(line.Text)
			}
			pages[p.Number] = p
		}
		if indexedBytes > 8<<20 {
			return nil, errors.New("indexed source exceeds 8 MiB")
		}
		if !doc.HasMore {
			break
		}
	}
	for id := range mapping.ImageURLs {
		found := false
		for _, page := range pages {
			for _, image := range page.Images {
				if image.ImageID == id {
					found = true
				}
			}
		}
		if !found {
			return nil, fmt.Errorf("image_urls references image %s absent from this PDF", id)
		}
	}
	used := map[[2]int]bool{}
	ignored := map[[2]int]bool{}
	selectRange := func(r Range, mark map[[2]int]bool) (string, error) {
		p, ok := pages[r.Page]
		if !ok || r.StartLine < 1 || r.EndLine < r.StartLine || r.EndLine > len(p.Lines) || r.StartColumn < 0 || r.EndColumn < 0 {
			return "", fmt.Errorf("invalid source range: %+v", r)
		}
		if p.RequiresOCR {
			return "", fmt.Errorf("page %d requires OCR", r.Page)
		}
		var lines []string
		for n := r.StartLine; n <= r.EndLine; n++ {
			chars := []rune(p.Lines[n-1].Text)
			from, to := 0, len(chars)
			if n == r.StartLine && r.StartColumn > 0 {
				from = r.StartColumn - 1
			}
			if n == r.EndLine && r.EndColumn > 0 {
				to = r.EndColumn
			}
			if from > to || to > len(chars) || (n == r.StartLine && r.StartColumn > 0 && from >= len(chars)) {
				return "", fmt.Errorf("invalid columns on page %d line %d", r.Page, n)
			}
			lines = append(lines, string(chars[from:to]))
			mark[[2]int{r.Page, n}] = true
		}
		return strings.Join(lines, "\n"), nil
	}
	for _, r := range mapping.Ignored {
		if _, err := selectRange(r, ignored); err != nil {
			return nil, fmt.Errorf("ignored: %w", err)
		}
	}
	var resolve func(jsontext.Value, int) (any, error)
	nodes := 0
	emitted := 0
	selectors := 0
	imagePages := map[int]bool{}
	resolve = func(raw jsontext.Value, depth int) (any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		nodes++
		if depth > 64 || nodes > 20000 {
			return nil, errors.New("mapping complexity limit exceeded")
		}
		text := strings.TrimSpace(string(raw))
		if text == "null" {
			return nil, nil
		}
		if len(text) == 0 {
			return nil, errors.New("missing output")
		}
		switch text[0] {
		case '[':
			var items []jsontext.Value
			if err := json.Unmarshal(raw, &items); err != nil {
				return nil, err
			}
			out := make([]any, len(items))
			for i, item := range items {
				value, err := resolve(item, depth+1)
				if err != nil {
					return nil, fmt.Errorf("[%d]: %w", i, err)
				}
				out[i] = value
			}
			return out, nil
		case '{':
			var object map[string]jsontext.Value
			if err := json.Unmarshal(raw, &object); err != nil {
				return nil, err
			}
			if parts, ok := object["$concat"]; ok {
				if len(object) != 1 {
					return nil, errors.New("$concat cannot have sibling members")
				}
				var items []jsontext.Value
				if err := json.Unmarshal(parts, &items); err != nil {
					return nil, err
				}
				if len(items) == 0 {
					return nil, errors.New("$concat requires source fragments; use null for an absent field")
				}
				var joined strings.Builder
				for _, item := range items {
					v, err := resolve(item, depth+1)
					if err != nil {
						return nil, err
					}
					s, ok := v.(string)
					if !ok {
						return nil, errors.New("$concat accepts source strings only")
					}
					if joined.Len()+len(s) > MaxMappingBytes {
						return nil, errors.New("concatenation exceeds 4 MiB")
					}
					joined.WriteString(s)
				}
				return joined.String(), nil
			}
			if source, ok := object["$source"]; ok {
				selectors++
				if len(object) != 1 {
					return nil, errors.New("$source cannot have sibling members")
				}
				var s Selector
				if err := json.Unmarshal(source, &s, json.RejectUnknownMembers(true)); err != nil {
					return nil, err
				}
				var selected string
				if s.Link != 0 || s.ImageID != "" {
					if len(s.Ranges) != 0 || (s.Link != 0 && s.ImageID != "") || s.Pattern != "" || s.Group != 0 {
						return nil, errors.New("ambiguous link/image selector")
					}
					p, ok := pages[s.Page]
					if !ok {
						return nil, errors.New("unknown media page")
					}
					if s.Link != 0 {
						if s.Link < 1 || s.Link > len(p.Links) {
							return nil, errors.New("unknown link index")
						}
						selected = p.Links[s.Link-1].URI
					} else {
						found := false
						for _, img := range p.Images {
							if img.ImageID == s.ImageID {
								found = true
								break
							}
						}
						if !found {
							return nil, errors.New("unknown image_id")
						}
						imagePages[s.Page] = true
						// Stable source reference; upload is a separate authorized action.
						selected = fmt.Sprintf("pdf-image://%s/%d/%s", mapping.SourceSHA256, s.Page, s.ImageID)
						if bound, ok := mapping.ImageURLs[s.ImageID]; ok {
							selected = bound
						}
					}
				} else {
					if len(s.Ranges) == 0 || s.Page != 0 {
						return nil, errors.New("source ranges required")
					}
					var chunks []string
					selectedBytes := 0
					for _, r := range s.Ranges {
						value, err := selectRange(r, used)
						if err != nil {
							return nil, err
						}
						selectedBytes += len(value) + 1
						if selectedBytes > MaxMappingBytes {
							return nil, errors.New("selected text exceeds 4 MiB")
						}
						chunks = append(chunks, value)
					}
					selected = strings.Join(chunks, "\n")
					if s.Pattern != "" {
						re, err := regexp.Compile(s.Pattern)
						if err != nil {
							return nil, fmt.Errorf("pattern: %w", err)
						}
						matches := re.FindAllStringSubmatch(selected, 2)
						if len(matches) != 1 || s.Group < 0 || s.Group >= len(matches[0]) {
							return nil, errors.New("pattern must match exactly once and group must exist")
						}
						selected = matches[0][s.Group]
						if selected == "" {
							return nil, errors.New("pattern capture must select nonempty source text; use null for an absent field")
						}
					} else if s.Group != 0 {
						return nil, errors.New("group requires pattern")
					}
				}
				emitted += len(selected)
				if emitted > MaxMappingBytes {
					return nil, errors.New("extracted text exceeds 4 MiB")
				}
				switch s.Format {
				case "", "text":
					return selected, nil
				case "trim":
					return strings.TrimSpace(selected), nil
				case "integer":
					n, err := strconv.Atoi(strings.TrimSpace(selected))
					if err != nil {
						return nil, fmt.Errorf("integer: %w", err)
					}
					return n, nil
				case "boolean":
					switch strings.ToLower(strings.TrimSpace(selected)) {
					case "да", "true", "yes", "1":
						return true, nil
					case "нет", "false", "no", "0":
						return false, nil
					default:
						return nil, errors.New("invalid source boolean")
					}
				case "html":
					return strings.ReplaceAll(html.EscapeString(selected), "\n", "<br>\n"), nil
				case "image_html":
					if s.ImageID == "" {
						return nil, errors.New("image_html requires an image_id")
					}
					return `<img src="` + html.EscapeString(selected) + `">`, nil
				default:
					return nil, fmt.Errorf("unknown source format %q", s.Format)
				}
			}
			out := map[string]any{}
			for key, item := range object {
				value, err := resolve(item, depth+1)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", key, err)
				}
				out[key] = value
			}
			return out, nil
		default:
			return nil, errors.New("literal scalar forbidden: use a $source selector, null or an empty container; do not regenerate document content")
		}
	}
	value, err := resolve(mapping.Output, 0)
	if err != nil {
		return nil, err
	}
	if selectors == 0 {
		return nil, errors.New("mapping output must contain at least one $source selector")
	}
	var missing []string
	for p := 1; p <= pageCount; p++ {
		if pages[p].RequiresOCR && len(pages[p].Images) > 0 && !imagePages[p] {
			return nil, fmt.Errorf("page %d has image content without readable text: OCR required, or explicitly preserve it using an image selector", p)
		}
		for _, line := range pages[p].Lines {
			key := [2]int{p, line.Number}
			if strings.TrimSpace(line.Text) != "" && !used[key] && !ignored[key] {
				if len(missing) < 20 {
					missing = append(missing, fmt.Sprintf("%d:%d", p, line.Number))
				}
			}
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("unmapped nonempty lines (page:line): %s; map fields or explicitly mark structural/unused lines in ignored", strings.Join(missing, ", "))
	}
	out, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(out) > MaxMappingBytes {
		return nil, errors.New("extracted output exceeds 4 MiB")
	}
	return &Extraction{Data: out, SourceSHA256: mapping.SourceSHA256, PageCount: pageCount, ReferencedLines: len(used), IgnoredLines: len(ignored)}, nil
}
