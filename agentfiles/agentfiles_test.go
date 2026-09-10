package agentfiles

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// TestSchemasDeclareArrayItems guards the tool schemas against a rejection that
// only surfaces at the provider. Google translates a schema into its own form
// and requires every array to say what it holds, so an array without "items"
// fails the whole request with INVALID_ARGUMENT before the model ever runs.
func TestSchemasDeclareArrayItems(t *testing.T) {
	for name, tool := range toolsFor(t, t.TempDir()) {
		walkSchema(t, name, tool.Parameters())
	}
}

// walkSchema reports every array-typed subschema that does not declare items.
func walkSchema(t *testing.T, path string, node any) {
	t.Helper()
	switch value := node.(type) {
	case map[string]any:
		if isArrayType(value["type"]) {
			if _, ok := value["items"]; !ok {
				t.Errorf("%s: тип array без items — Google отклонит запрос", path)
			}
		}
		for key, child := range value {
			walkSchema(t, path+"."+key, child)
		}
	case []any:
		for i, child := range value {
			walkSchema(t, fmt.Sprintf("%s[%d]", path, i), child)
		}
	}
}

// isArrayType reports whether a schema's "type" admits an array, in either the
// single-type or the union spelling.
func isArrayType(value any) bool {
	switch typed := value.(type) {
	case string:
		return typed == "array"
	case []string:
		return slices.Contains(typed, "array")
	case []any:
		return slices.Contains(typed, any("array"))
	}
	return false
}

// toolsFor builds the catalog over a fresh directory and returns it by name.
func toolsFor(t *testing.T, root string) map[string]*Tool {
	t.Helper()
	list, err := Tools(root)
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	byName := make(map[string]*Tool, len(list))
	for _, tool := range list {
		byName[tool.Name()] = tool
	}
	return byName
}

// call runs a tool and decodes its JSON payload, failing on a refusal.
func call(t *testing.T, tool *Tool, args map[string]any) map[string]any {
	t.Helper()
	res := tool.Execute(t.Context(), args)
	if res.IsError {
		t.Fatalf("%s вернул ошибку: %s", tool.Name(), res.Content)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Content), &out); err != nil {
		t.Fatalf("%s вернул не JSON (%v): %s", tool.Name(), err, res.Content)
	}
	return out
}

func TestReadLocalFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("код на щите"), 0o600); err != nil {
		t.Fatal(err)
	}
	tools := toolsFor(t, root)

	out := call(t, tools["read_local_file"], map[string]any{"path": "notes.md"})
	if out["content"] != "код на щите" {
		t.Errorf("содержимое = %v", out["content"])
	}
	if out["truncated"] != false {
		t.Errorf("короткий файл помечен обрезанным: %v", out["truncated"])
	}
}

func TestReadLocalFileTruncatesAtMaxBytes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "big.txt"), []byte("abcdefghij"), 0o600); err != nil {
		t.Fatal(err)
	}
	tools := toolsFor(t, root)

	out := call(t, tools["read_local_file"], map[string]any{"path": "big.txt", "max_bytes": float64(4)})
	if out["content"] != "abcd" {
		t.Errorf("содержимое = %v, ожидалось \"abcd\"", out["content"])
	}
	if out["truncated"] != true {
		t.Error("обрезка не отмечена")
	}

	// A file that ends exactly at the limit is complete, not truncated.
	exact := call(t, tools["read_local_file"], map[string]any{"path": "big.txt", "max_bytes": float64(10)})
	if exact["truncated"] != false {
		t.Error("файл ровно по лимиту помечен обрезанным")
	}

	// The request is bounded whatever the model asks for.
	huge := call(t, tools["read_local_file"], map[string]any{"path": "big.txt", "max_bytes": float64(1 << 30)})
	if huge["truncated"] != false {
		t.Error("маленький файл помечен обрезанным при огромном max_bytes")
	}
}

func TestReadLocalFileHonoursOffset(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	tools := toolsFor(t, root)

	out := call(t, tools["read_local_file"], map[string]any{"path": "f.txt", "offset": float64(6)})
	if out["content"] != "6789" {
		t.Errorf("содержимое со смещения = %v", out["content"])
	}
}

func TestPathsOutsideRootAreRefused(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("не для агента"), 0o600); err != nil {
		t.Fatal(err)
	}
	tools := toolsFor(t, root)

	for _, path := range []string{"../secret.txt", outside, "sub/../../secret.txt"} {
		res := tools["read_local_file"].Execute(t.Context(), map[string]any{"path": path})
		if !res.IsError {
			t.Errorf("путь %q прочитан, хотя он вне корня: %s", path, res.Content)
			continue
		}
		if !strings.Contains(res.Content, RootEnv) {
			t.Errorf("отказ для %q не объясняет причину: %s", path, res.Content)
		}
	}
}

func TestSymlinkOutOfRootIsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("символические ссылки на Windows требуют отдельных прав")
	}
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("не для агента"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	tools := toolsFor(t, root)

	res := tools["read_local_file"].Execute(t.Context(), map[string]any{"path": "link.txt"})
	if !res.IsError {
		t.Errorf("файл вне корня прочитан по символической ссылке: %s", res.Content)
	}
}

// WalkDir hands a symlinked file over as an ordinary one, so the search has
// to check the link's target itself or it reads straight out of the root.
func TestSearchDoesNotFollowSymlinkOutOfRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("символические ссылки на Windows требуют отдельных прав")
	}
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	mustWrite(t, outside, "искомая тайна\n")
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	// A link that stays inside the root is legitimate and must still be read.
	mustWrite(t, filepath.Join(root, "real.md"), "искомая тайна\n")
	if err := os.Symlink(filepath.Join(root, "real.md"), filepath.Join(root, "inside.md")); err != nil {
		t.Fatal(err)
	}
	tools := toolsFor(t, root)

	out := call(t, tools["search_local_files"], map[string]any{"pattern": "искомая тайна"})
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "link.txt") {
		t.Errorf("поиск прочитал файл вне корня по символической ссылке: %s", raw)
	}
	if !strings.Contains(string(raw), "real.md") || !strings.Contains(string(raw), "inside.md") {
		t.Errorf("поиск потерял файлы внутри корня: %s", raw)
	}
	if out["count"] != float64(2) {
		t.Errorf("найдено %v совпадений, ожидалось 2 (real.md и ссылка на него)", out["count"])
	}
}

func TestListLocalDir(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.md"), "a")
	mustWrite(t, filepath.Join(root, "sub", "b.md"), "b")
	mustWrite(t, filepath.Join(root, "sub", "deep", "deeper", "toofar", "c.md"), "c")
	tools := toolsFor(t, root)

	flat := call(t, tools["list_local_dir"], map[string]any{})
	names := entryNames(t, flat)
	if !contains(names, "a.md") || !contains(names, "sub") {
		t.Errorf("плоский список = %v", names)
	}
	if contains(names, "b.md") {
		t.Errorf("плоский список включил вложенный файл: %v", names)
	}

	deep := call(t, tools["list_local_dir"], map[string]any{"recursive": true})
	deepNames := entryNames(t, deep)
	if !contains(deepNames, filepath.Join("sub", "b.md")) {
		t.Errorf("рекурсивный список не нашёл вложенный файл: %v", deepNames)
	}
	// The walk stops at three levels, so the fourth is never listed.
	if contains(deepNames, filepath.Join("sub", "deep", "deeper", "toofar")) {
		t.Errorf("рекурсивный список ушёл глубже трёх уровней: %v", deepNames)
	}
}

func TestListLocalDirRefusesFile(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.md"), "a")
	tools := toolsFor(t, root)

	res := tools["list_local_dir"].Execute(t.Context(), map[string]any{"path": "a.md"})
	if !res.IsError {
		t.Errorf("файл принят как каталог: %s", res.Content)
	}
}

func TestSearchLocalFiles(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "notes.md"), "первая строка\nискомый КОД здесь\n")
	mustWrite(t, filepath.Join(root, "other.txt"), "искомый код в другом файле\n")
	tools := toolsFor(t, root)

	all := call(t, tools["search_local_files"], map[string]any{"pattern": "искомый код"})
	if all["count"] != float64(2) {
		t.Errorf("найдено %v совпадений, ожидалось 2", all["count"])
	}

	filtered := call(t, tools["search_local_files"], map[string]any{"pattern": "искомый код", "glob": "*.md"})
	if filtered["count"] != float64(1) {
		t.Errorf("с маской *.md найдено %v совпадений, ожидалось 1", filtered["count"])
	}
	matches, _ := filtered["matches"].([]any)
	if len(matches) != 1 {
		t.Fatalf("совпадения = %v", filtered["matches"])
	}
	m, _ := matches[0].(map[string]any)
	if m["line"] != float64(2) {
		t.Errorf("номер строки = %v, ожидалось 2", m["line"])
	}
}

func TestSearchLocalFilesHonoursLimit(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "many.txt"), strings.Repeat("код\n", 20))
	tools := toolsFor(t, root)

	out := call(t, tools["search_local_files"], map[string]any{"pattern": "код", "max_matches": float64(5)})
	if out["count"] != float64(5) {
		t.Errorf("найдено %v совпадений, ожидалось 5", out["count"])
	}
	if out["truncated"] != true {
		t.Error("обрезка результатов поиска не отмечена")
	}
}

func TestSearchLocalFilesRequiresPattern(t *testing.T) {
	tools := toolsFor(t, t.TempDir())
	res := tools["search_local_files"].Execute(t.Context(), map[string]any{})
	if !res.IsError {
		t.Errorf("поиск без строки принят: %s", res.Content)
	}
}

func TestRootFromEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(RootEnv, dir)
	root, err := RootFromEnv()
	if err != nil {
		t.Fatalf("RootFromEnv: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		resolved = dir
	}
	if root != resolved {
		t.Errorf("корень = %q, ожидался %q", root, resolved)
	}

	t.Setenv(RootEnv, "")
	wd, err := RootFromEnv()
	if err != nil {
		t.Fatalf("RootFromEnv без переменной: %v", err)
	}
	if wd == "" {
		t.Error("без переменной корнем должен стать рабочий каталог")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func entryNames(t *testing.T, out map[string]any) []string {
	t.Helper()
	raw, _ := out["entries"].([]any)
	names := make([]string, 0, len(raw))
	for _, e := range raw {
		m, _ := e.(map[string]any)
		name, _ := m["name"].(string)
		names = append(names, name)
	}
	return names
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
