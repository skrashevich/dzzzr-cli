package pdfsource

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"path/filepath"
)

// MappingManifest orders independently saved structural mappings. Parts are
// relative to the manifest directory and cannot themselves be manifests.
type MappingManifest struct {
	Version      int      `json:"version"`
	SourceSHA256 string   `json:"source_sha256"`
	Parts        []string `json:"parts"`
}

func loadMapping(root, path string) ([]byte, error) {
	raw, err := readRootFile(root, path, MaxMappingBytes)
	if err != nil {
		return nil, err
	}
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, err
	}
	if header.Version != 2 {
		return raw, nil
	}
	var manifest MappingManifest
	if err := json.Unmarshal(raw, &manifest, json.RejectUnknownMembers(true)); err != nil {
		return nil, err
	}
	if len(manifest.Parts) == 0 || len(manifest.Parts) > 512 || len(manifest.SourceSHA256) != 64 {
		return nil, errors.New("mapping manifest requires source_sha256 and 1..512 ordered parts")
	}
	merged := Mapping{Version: 1, SourceSHA256: manifest.SourceSHA256, ImageURLs: map[string]string{}}
	output := map[string]any{}
	seen := map[string]bool{}
	total := len(raw)
	for _, name := range manifest.Parts {
		if !filepath.IsLocal(name) {
			return nil, fmt.Errorf("mapping part %q must be relative to the manifest directory", name)
		}
		name = filepath.Clean(name)
		if seen[name] {
			return nil, fmt.Errorf("duplicate mapping part %q", name)
		}
		seen[name] = true
		partJSON, err := readRootFile(root, filepath.Join(filepath.Dir(path), name), MaxMappingBytes)
		if err != nil {
			return nil, fmt.Errorf("mapping part %q: %w", name, err)
		}
		total += len(partJSON)
		if total > MaxMappingBytes {
			return nil, errors.New("combined mapping parts exceed 4 MiB")
		}
		var part Mapping
		if err := json.Unmarshal(partJSON, &part, json.RejectUnknownMembers(true)); err != nil {
			return nil, fmt.Errorf("mapping part %q: %w", name, err)
		}
		if part.Version != 1 || part.SourceSHA256 != manifest.SourceSHA256 {
			return nil, fmt.Errorf("mapping part %q: version or source_sha256 mismatch", name)
		}
		var tree map[string]any
		if err := json.Unmarshal(part.Output, &tree); err != nil || tree == nil {
			return nil, fmt.Errorf("mapping part %q output must be an object", name)
		}
		if err := mergeMappingObjects(output, tree, 0); err != nil {
			return nil, fmt.Errorf("mapping part %q: %w", name, err)
		}
		merged.Ignored = append(merged.Ignored, part.Ignored...)
		for id, url := range part.ImageURLs {
			if prior, ok := merged.ImageURLs[id]; ok && prior != url {
				return nil, fmt.Errorf("mapping part %q: conflicting image URL for %s", name, id)
			}
			merged.ImageURLs[id] = url
		}
	}
	merged.Output, err = json.Marshal(output)
	if err != nil {
		return nil, err
	}
	return json.Marshal(merged)
}

// Objects merge recursively, arrays append in manifest order. Leaves are
// atomic: merging selectors or overwriting a field is always an error.
func mergeMappingObjects(dst, src map[string]any, depth int) error {
	if depth > 64 {
		return errors.New("mapping nesting exceeds 64")
	}
	for key, value := range src {
		prior, exists := dst[key]
		if !exists {
			dst[key] = value
			continue
		}
		if key == "$source" || key == "$concat" {
			return fmt.Errorf("conflicting selector %s", key)
		}
		switch incoming := value.(type) {
		case map[string]any:
			previous, ok := prior.(map[string]any)
			if ok {
				if err := mergeMappingObjects(previous, incoming, depth+1); err != nil {
					return fmt.Errorf("%s: %w", key, err)
				}
				continue
			}
		case []any:
			if previous, ok := prior.([]any); ok {
				dst[key] = append(previous, incoming...)
				continue
			}
		}
		return fmt.Errorf("conflicting mapping field %s", key)
	}
	return nil
}
