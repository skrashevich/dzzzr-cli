package agentfiles

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MaxJSONBytes bounds a generated local JSON artifact.
const MaxJSONBytes = 4 << 20

func saveJSONTool() *Tool {
	return &Tool{
		name:        "save_local_json",
		description: "Сохранить подготовленный JSON в НОВЫЙ локальный файл .json, только когда пользователь явно попросил сохранить результат. Существующие файлы не заменяются. Доступно и при readonly движка: это локальный документ, не изменение игры. Путь внутри " + RootEnv + "; максимум 4 МиБ. Предпочитайте data: готовый объект или массив без второго слоя JSON-экранирования. content — совместимый строковый вариант; укажите ровно одно из data/content.",
		parameters:  saveJSONSchema(),
		run:         saveJSON,
	}
}

func saveJSONSchema() map[string]any {
	s := schema(map[string]any{
		"path":    strProp("Путь нового файла .json внутри корня; родительский каталог должен существовать"),
		"content": strProp("Полный JSON как строка UTF-8; объект или массив, без Markdown-ограждений"),
		// items stays empty on purpose: the elements are arbitrary JSON. It
		// cannot be omitted, because Google translates the array half of this
		// union into its own schema and rejects the whole request when an
		// array does not say what it holds.
		"data": map[string]any{
			"type":        []string{"object", "array"},
			"items":       map[string]any{},
			"description": "Готовый JSON-объект или массив; предпочтительный способ",
		},
	}, "path")
	s["oneOf"] = []any{map[string]any{"required": []string{"data"}}, map[string]any{"required": []string{"content"}}}
	return s
}

func saveJSON(root string, a args) (any, error) {
	path, err := a.requireString("path")
	if err != nil {
		return nil, err
	}
	_, hasContent := a["content"]
	data, hasData := a["data"]
	if hasContent == hasData {
		return nil, errors.New("укажите ровно одно из data или content")
	}
	var content []byte
	if hasData {
		// Some tool-calling models stringify an object despite its schema. Apply
		// the same strict validation as legacy content instead of another retry.
		if encoded, ok := data.(string); ok {
			content = []byte(encoded)
		} else {
			content, err = json.Marshal(data)
		}
	} else {
		var value string
		value, err = a.requireString("content")
		content = []byte(value)
	}
	if err != nil {
		return nil, err
	}
	if err := validateJSON(content); err != nil {
		return nil, err
	}
	return writeNewJSON(root, path, content)
}

func validateJSON(content []byte) error {
	if len(content) > MaxJSONBytes {
		return fmt.Errorf("JSON превышает лимит %d байт", MaxJSONBytes)
	}
	var value jsontext.Value
	if err := json.Unmarshal(content, &value); err != nil {
		if syntax, ok := errors.AsType[*jsontext.SyntacticError](err); ok {
			return fmt.Errorf("некорректный JSON: byte_offset=%d JSON_pointer=%q: %w", syntax.ByteOffset, syntax.JSONPointer, syntax.Err)
		}
		return fmt.Errorf("некорректный JSON: %w", err)
	}
	trimmed := strings.TrimSpace(string(content))
	if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
		return errors.New("JSON должен быть объектом или массивом")
	}
	return nil
}

func localJSONPath(root, path string) (string, error) {
	if filepath.IsAbs(path) {
		// Accept alternate absolute spellings of the same root (for example
		// macOS /var and /private/var), resolving only the existing parent.
		parentPath, err := filepath.EvalSymlinks(filepath.Dir(path))
		if err != nil {
			return "", err
		}
		path, err = filepath.Rel(root, filepath.Join(parentPath, filepath.Base(path)))
		if err != nil {
			return "", errOutsideRoot
		}
	}
	if !filepath.IsLocal(path) {
		return "", errOutsideRoot
	}
	if !strings.EqualFold(filepath.Ext(path), ".json") {
		return "", errors.New("имя файла должно иметь расширение .json")
	}
	return path, nil
}

func writeNewJSON(root, path string, content []byte) (map[string]any, error) {
	path, err := localJSONPath(root, path)
	if err != nil {
		return nil, err
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }()
	// Pin the parent directory so a renamed intermediate directory cannot redirect
	// either creation or cleanup. os.Root checks symlinks against the capability.
	parent, err := dir.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("каталог внутри %s: %w", RootEnv, err)
	}
	defer func() { _ = parent.Close() }()
	name := filepath.Base(path)
	file, err := parent.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("создать новый JSON без перезаписи: %w", err)
	}
	_, writeErr := file.Write(content)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return nil, errors.Join(err, parent.Remove(name))
	}
	return map[string]any{"path": filepath.Join(root, path), "bytes": len(content), "saved": true}, nil
}
