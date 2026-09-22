package agentfiles

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/skrashevich/dzzzr-cli/agenttools"
)

// Limits on one call. They exist so a single tool result stays something a
// model can hold and a team can afford.
const (
	defaultReadBytes = 64 << 10
	maxReadBytes     = 512 << 10
	maxListEntries   = 500
	maxListDepth     = 3
	defaultMatches   = 50
	maxMatches       = 200
	snippetRunes     = 240
)

// Tool is one file capability. It carries the same shape as an engine tool, so
// the agent loop takes both from one slice.
type Tool struct {
	name        string
	description string
	parameters  map[string]any
	run         func(root string, args args) (any, error)
	root        string
}

// Name returns the tool's name as the model sees it.
func (t *Tool) Name() string { return t.name }

// Description returns the tool's description.
func (t *Tool) Description() string { return t.description }

// Parameters returns the JSON Schema of the tool's arguments.
func (t *Tool) Parameters() map[string]any { return t.parameters }

// Execute runs the tool and encodes what it found as JSON.
func (t *Tool) Execute(ctx context.Context, raw map[string]any) agenttools.Result {
	if err := ctx.Err(); err != nil {
		return agenttools.Result{Content: fmt.Sprintf("%s failed: %v", t.name, err), IsError: true}
	}
	value, err := t.run(t.root, args(raw))
	if err != nil {
		return agenttools.Result{Content: fmt.Sprintf("%s failed: %v", t.name, err), IsError: true}
	}
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	// Tool results are JSON, not inline HTML. Escaping every HTML tag
	// needlessly expands saved scenarios before they reach the model.
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return agenttools.Result{Content: fmt.Sprintf("%s: cannot encode result: %v", t.name, err), IsError: true}
	}
	return agenttools.Result{Content: strings.TrimSuffix(data.String(), "\n")}
}

// Tools returns the file tools bound to one root directory.
func Tools(root string) ([]*Tool, error) {
	resolved, err := resolveRoot(root)
	if err != nil {
		return nil, err
	}
	list := []*Tool{
		saveJSONTool(),
		assembleJSONTool(),
		{
			name: "read_local_file",
			description: "Прочитать файл на этом компьютере: заметки, черновик сценария, выгрузку. " +
				"Путь относительно каталога " + RootEnv + ".",
			parameters: schema(map[string]any{
				"path":      strProp("Путь к файлу относительно корня"),
				"max_bytes": intProp("Сколько байт прочитать, по умолчанию 65536, максимум 524288"),
				"offset":    intProp("С какого байта читать, по умолчанию 0"),
			}, "path"),
			run: readFile,
		},
		{
			name:        "list_local_dir",
			description: "Показать содержимое каталога на этом компьютере.",
			parameters: schema(map[string]any{
				"path":      strProp("Путь к каталогу относительно корня; по умолчанию сам корень"),
				"recursive": boolProp("Обойти вложенные каталоги на три уровня вглубь"),
			}),
			run: listDir,
		},
		{
			name:        "search_local_files",
			description: "Найти строку в файлах на этом компьютере. Поиск без учёта регистра.",
			parameters: schema(map[string]any{
				"path":        strProp("Каталог, в котором искать; по умолчанию сам корень"),
				"pattern":     strProp("Искомая подстрока"),
				"glob":        strProp("Маска имени файла, например *.md"),
				"max_matches": intProp("Сколько совпадений вернуть, по умолчанию 50, максимум 200"),
			}, "pattern"),
			run: searchFiles,
		},
	}
	for _, t := range list {
		t.root = resolved
	}
	return list, nil
}

// entry is one item of a directory listing.
type entry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size,omitempty"`
}

// match is one hit of a search.
type match struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

func readFile(root string, a args) (any, error) {
	path, err := a.requireString("path")
	if err != nil {
		return nil, err
	}
	abs, err := resolve(root, path)
	if err != nil {
		return nil, err
	}

	maxBytes := clamp(a.intOr("max_bytes", defaultReadBytes), 1, maxReadBytes)
	offset := max(a.intOr("offset", 0), 0)

	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, errors.New("это каталог, используйте list_local_dir")
	}

	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	if offset > 0 {
		if _, err := f.Seek(int64(offset), 0); err != nil {
			return nil, err
		}
	}
	// One byte past the limit is read, so a file that ends exactly at it is
	// not reported as truncated.
	data, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)+1))
	if err != nil {
		return nil, err
	}
	truncated := len(data) > maxBytes
	if truncated {
		data = data[:maxBytes]
	}

	return map[string]any{
		"path":      abs,
		"size":      info.Size(),
		"offset":    offset,
		"read":      len(data),
		"truncated": truncated,
		"content":   string(data),
	}, nil
}

func listDir(root string, a args) (any, error) {
	abs, err := resolve(root, a.stringOr("path", "."))
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("это не каталог")
	}

	recursive := a.boolOr("recursive", false)
	if !recursive {
		items, err := os.ReadDir(abs)
		if err != nil {
			return nil, err
		}
		entries := make([]entry, 0, len(items))
		for _, item := range items {
			fi, err := item.Info()
			if err != nil {
				continue
			}
			entries = append(entries, entry{
				Name:  item.Name(),
				Path:  filepath.Join(abs, item.Name()),
				IsDir: fi.IsDir(),
				Size:  fi.Size(),
			})
		}
		return map[string]any{"path": abs, "count": len(entries), "entries": entries}, nil
	}

	var entries []entry
	// errEnough stops the walk once the listing is as large as it may get.
	errEnough := errors.New("enough")
	err = filepath.WalkDir(abs, func(full string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if full == abs {
			return nil
		}
		rel, err := filepath.Rel(abs, full)
		if err != nil {
			return err
		}
		if strings.Count(rel, string(os.PathSeparator))+1 > maxListDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		entries = append(entries, entry{Name: rel, Path: full, IsDir: fi.IsDir(), Size: fi.Size()})
		if len(entries) >= maxListEntries {
			return errEnough
		}
		return nil
	})
	if err != nil && !errors.Is(err, errEnough) {
		return nil, err
	}

	return map[string]any{
		"path":      abs,
		"recursive": true,
		"count":     len(entries),
		"entries":   entries,
		"truncated": len(entries) >= maxListEntries,
	}, nil
}

func searchFiles(root string, a args) (any, error) {
	pattern, err := a.requireString("pattern")
	if err != nil {
		return nil, err
	}
	abs, err := resolve(root, a.stringOr("path", "."))
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	searchRoot := abs
	if !info.IsDir() {
		searchRoot = filepath.Dir(abs)
	}

	glob := a.stringOr("glob", "")
	limit := clamp(a.intOr("max_matches", defaultMatches), 1, maxMatches)
	needle := strings.ToLower(pattern)

	var matches []match
	errEnough := errors.New("enough")
	err = filepath.WalkDir(searchRoot, func(full string, d fs.DirEntry, walkErr error) error {
		// An unreadable corner of the tree is skipped rather than failing the
		// whole search.
		if walkErr != nil || d.IsDir() {
			return nil //nolint:nilerr // a directory we cannot enter is not a failed search
		}
		if glob != "" {
			if ok, _ := filepath.Match(glob, d.Name()); !ok {
				return nil
			}
		}
		// WalkDir does not descend into a symlinked directory, but it hands a
		// symlinked file over as an ordinary one — and os.Open would then
		// follow it out of the root. d.Info is an lstat, so even the size
		// check below would measure the link rather than its target.
		if d.Type()&fs.ModeSymlink != 0 {
			if _, err := resolve(root, full); err != nil {
				return nil //nolint:nilerr // a link out of the root is skipped, not an error
			}
		}
		fi, err := os.Stat(full)
		if err != nil || fi.Size() > maxReadBytes {
			return nil //nolint:nilerr // an oversized or vanished file is skipped
		}
		f, err := os.Open(full)
		if err != nil {
			return nil //nolint:nilerr // same
		}
		defer func() { _ = f.Close() }()

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for line := 1; scanner.Scan(); line++ {
			text := scanner.Text()
			if !strings.Contains(strings.ToLower(text), needle) {
				continue
			}
			matches = append(matches, match{
				Path:    full,
				Line:    line,
				Snippet: truncate(strings.TrimSpace(text), snippetRunes),
			})
			if len(matches) >= limit {
				return errEnough
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, errEnough) {
		return nil, err
	}

	return map[string]any{
		"root":      searchRoot,
		"pattern":   pattern,
		"glob":      glob,
		"count":     len(matches),
		"truncated": len(matches) >= limit,
		"matches":   matches,
	}, nil
}

func clamp(v, lo, hi int) int {
	return min(max(v, lo), hi)
}

func truncate(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}
