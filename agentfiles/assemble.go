package agentfiles

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const maxJSONParts = 128

func assembleJSONTool() *Tool {
	return &Tool{
		name:        "assemble_local_json",
		description: "Собрать НОВЫЙ JSON из заранее сохранённого manifest и отдельных JSON-уровней внутри " + RootEnv + ". Только по просьбе сохранить результат. manifest содержит levels с уникальными key; каждая часть — объект с таким key. Все ключи должны быть покрыты ровно один раз, иначе сборка откажет: нельзя выдать неполный набор уровней за полный сценарий. Порядок берётся из manifest. До 128 частей и 4 МиБ суммарно. Файлы не перезаписываются; движок не изменяется.",
		parameters: schema(map[string]any{
			"path":     strProp("Путь нового итогового .json"),
			"manifest": strProp("Путь сохранённого .json с levels:[{key,title,...}], необязательными source и metadata"),
			"parts":    map[string]any{"type": "array", "items": strProp("Путь сохранённой JSON-части с key"), "minItems": 1, "maxItems": maxJSONParts},
		}, "path", "manifest", "parts"),
		run: assembleJSON,
	}
}

func assembleJSON(root string, a args) (any, error) {
	path, err := a.requireString("path")
	if err != nil {
		return nil, err
	}
	manifestPath, err := a.requireString("manifest")
	if err != nil {
		return nil, err
	}
	var paths []string
	switch raw := a["parts"].(type) {
	case []string:
		paths = raw
	case []any:
		for _, value := range raw {
			part, ok := value.(string)
			if !ok || strings.TrimSpace(part) == "" {
				return nil, errors.New("parts должен содержать пути строками")
			}
			paths = append(paths, part)
		}
	default:
		return nil, errors.New("parts должен быть массивом путей")
	}
	if len(paths) < 1 || len(paths) > maxJSONParts {
		return nil, fmt.Errorf("нужно от 1 до %d частей", maxJSONParts)
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }()
	remaining := MaxJSONBytes
	readObject := func(path string) (map[string]jsontext.Value, error) {
		rel, err := localJSONPath(root, path)
		if err != nil {
			return nil, err
		}
		info, err := dir.Stat(rel)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("JSON-часть должна быть обычным файлом")
		}
		if info.Size() > int64(remaining) {
			return nil, errors.New("manifest и части превышают суммарный лимит 4 МиБ")
		}
		file, err := dir.Open(rel)
		if err != nil {
			return nil, err
		}
		defer func() { _ = file.Close() }()
		info, err = file.Stat()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("JSON-часть должна быть обычным файлом")
		}
		content, err := io.ReadAll(io.LimitReader(file, int64(remaining)+1))
		if err != nil {
			return nil, err
		}
		if len(content) > remaining {
			return nil, errors.New("manifest и части превышают суммарный лимит 4 МиБ")
		}
		remaining -= len(content)
		if err := validateJSON(content); err != nil {
			return nil, err
		}
		if strings.TrimSpace(string(content))[0] != '{' {
			return nil, errors.New("manifest и каждая часть должны быть JSON-объектами")
		}
		var object map[string]jsontext.Value
		if err := json.Unmarshal(content, &object); err != nil {
			return nil, err
		}
		return object, nil
	}
	manifest, err := readObject(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	var inventory []map[string]jsontext.Value
	if err := json.Unmarshal(manifest["levels"], &inventory); err != nil {
		return nil, fmt.Errorf("manifest.levels: %w", err)
	}
	if len(inventory) < 1 || len(inventory) > maxJSONParts {
		return nil, fmt.Errorf("manifest.levels должен содержать от 1 до %d уровней", maxJSONParts)
	}
	keyOf := func(object map[string]jsontext.Value) (string, error) {
		var key string
		if err := json.Unmarshal(object["key"], &key); err != nil || strings.TrimSpace(key) == "" {
			return "", errors.New("каждый уровень должен содержать непустой строковый key")
		}
		return key, nil
	}
	positions := map[string]int{}
	ordered := make([]map[string]jsontext.Value, len(inventory))
	for i, item := range inventory {
		key, err := keyOf(item)
		if err != nil {
			return nil, fmt.Errorf("manifest.levels[%d]: %w", i, err)
		}
		if _, exists := positions[key]; exists {
			return nil, fmt.Errorf("повторяющийся key в manifest: %q", key)
		}
		positions[key] = i
	}
	if metadata, ok := manifest["metadata"]; ok && (!strings.HasPrefix(strings.TrimSpace(string(metadata)), "{")) {
		return nil, errors.New("manifest.metadata должен быть объектом")
	}
	for i, partPath := range paths {
		part, err := readObject(partPath)
		if err != nil {
			return nil, fmt.Errorf("parts[%d]: %w", i, err)
		}
		key, err := keyOf(part)
		if err != nil {
			return nil, fmt.Errorf("parts[%d]: %w", i, err)
		}
		index, known := positions[key]
		if !known {
			return nil, fmt.Errorf("неизвестный key части: %q", key)
		}
		if ordered[index] != nil {
			return nil, fmt.Errorf("повторяющаяся часть key=%q", key)
		}
		ordered[index] = part
	}
	var missing []string
	for i, item := range inventory {
		if ordered[i] == nil {
			key, _ := keyOf(item)
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("сборка неполна: отсутствуют %d из %d уровней: %s", len(missing), len(inventory), strings.Join(missing, ", "))
	}
	output := map[string]any{"levels": ordered}
	for _, key := range []string{"source", "metadata"} {
		if value, ok := manifest[key]; ok {
			output[key] = value
		}
	}
	content, err := json.Marshal(output)
	if err != nil {
		return nil, err
	}
	if err := validateJSON(content); err != nil {
		return nil, err
	}
	result, err := writeNewJSON(root, path, content)
	if err != nil {
		return nil, err
	}
	result["level_count"] = len(ordered)
	return result, nil
}
