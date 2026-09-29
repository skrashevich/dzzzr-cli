// Package browserapp exposes portable dzzzr capabilities without filesystem or engine credentials.
package browserapp

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"slices"
	"sync"

	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/gamesource"
	"github.com/skrashevich/dzzzr-cli/gamestats"
	"github.com/skrashevich/dzzzr-cli/pdfsource"
	"github.com/skrashevich/dzzzr-cli/statstore"
)

type App struct {
	Store statstore.Memory
	mu    sync.Mutex
	files map[string][]byte
}
type Request struct {
	Action   string            `json:"action"`
	Name     string            `json:"name"`
	Data     []byte            `json:"data"`
	Args     map[string]any    `json:"args"`
	LogID    string            `json:"log_id"`
	Config   *gamestats.Config `json:"cfg"`
	Revision int               `json:"revision"`
}

func (a *App) read(name string) ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	b, ok := a.files[name]
	if !ok {
		return nil, fmt.Errorf("файл %q не загружен", name)
	}
	return slices.Clone(b), nil
}
func (a *App) put(name string, data []byte) error {
	if len(data) > 96<<20 {
		return fmt.Errorf("файл превышает 96 МБ")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.files == nil {
		a.files = map[string][]byte{}
	}
	a.files[name] = slices.Clone(data)
	return nil
}
func (a *App) Dispatch(ctx context.Context, r Request) (any, error) {
	switch r.Action {
	case "upload":
		if err := a.put(r.Name, r.Data); err != nil {
			return nil, err
		}
		return map[string]any{"name": r.Name, "size": len(r.Data)}, nil
	case "files":
		a.mu.Lock()
		defer a.mu.Unlock()
		names := []string{}
		for n := range a.files {
			names = append(names, n)
		}
		slices.Sort(names)
		return names, nil
	case "download":
		b, err := a.read(r.Name)
		return map[string]any{"data": b}, err
	case "import":
		l, err := a.Store.Create(r.Name, r.Data, r.Config)
		if err != nil {
			return nil, err
		}
		return logReport(l)
	case "log_source":
		l, err := a.Store.Load(r.LogID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"name": l.Name, "data": l.Data, "revision": l.Revision}, nil
	case "report":
		l, err := a.Store.Load(r.LogID)
		if err != nil {
			return nil, err
		}
		return logReport(l)
	case "configure":
		if r.Config == nil {
			return nil, fmt.Errorf("cfg обязателен")
		}
		l, err := a.Store.Update(r.LogID, r.Revision, *r.Config)
		if err != nil {
			return nil, err
		}
		return logReport(l)
	case "tool":
		for _, t := range a.Tools() {
			if t.Name() == r.Name {
				result := t.Execute(ctx, r.Args)
				if result.IsError {
					return nil, fmt.Errorf("%s", result.Content)
				}
				var v any
				if err := json.Unmarshal([]byte(result.Content), &v); err != nil {
					return result.Content, nil
				}
				return v, nil
			}
		}
		return nil, fmt.Errorf("нет инструмента %s", r.Name)
	case "schema":
		return map[string]any{"game": agenttools.GameParamsSchema(), "level": agenttools.LevelParamsSchema()}, nil
	}
	return nil, fmt.Errorf("неизвестное действие")
}
func logReport(l *statstore.Log) (any, error) {
	r, err := l.Report()
	if err != nil {
		return nil, err
	}
	return map[string]any{"log_id": l.ID, "revision": l.Revision, "name": l.Name, "report": r, "defaults": gamestats.DefaultConfig(r.Game)}, nil
}

type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Execute(context.Context, map[string]any) agenttools.Result
}
type localTool struct {
	name, desc string
	params     map[string]any
	run        func(context.Context, map[string]any) (any, error)
}

func (t localTool) Name() string               { return t.name }
func (t localTool) Description() string        { return t.desc }
func (t localTool) Parameters() map[string]any { return t.params }
func (t localTool) Execute(ctx context.Context, args map[string]any) agenttools.Result {
	v, err := t.run(ctx, args)
	if err != nil {
		return agenttools.Result{Content: err.Error(), IsError: true}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return agenttools.Result{Content: err.Error(), IsError: true}
	}
	return agenttools.Result{Content: string(b)}
}
func str(a map[string]any, k string) string { v, _ := a[k].(string); return v }
func integer(a map[string]any, k string, def int) int {
	v, ok := a[k].(float64)
	if ok {
		return int(v)
	}
	if n, ok := a[k].(int); ok {
		return n
	}
	return def
}
func (a *App) Tools() []Tool {
	list := []Tool{}
	for _, t := range agenttools.BrowserStatisticsTools(&a.Store, a.read) {
		list = append(list, t)
	}
	add := func(name, desc string, props map[string]any, required []string, run func(context.Context, map[string]any) (any, error)) {
		list = append(list, localTool{name, desc, map[string]any{"type": "object", "properties": props, "required": required}, run})
	}
	stringProp := map[string]any{"type": "string"}
	intProp := map[string]any{"type": "integer"}
	add("list_files", "Загруженные и созданные файлы текущей вкладки.", nil, nil, func(ctx context.Context, _ map[string]any) (any, error) {
		return a.Dispatch(ctx, Request{Action: "files"})
	})
	add("read_local_file", "Читать текстовый файл по диапазону байтов (максимум 24000). Для журналов используйте stats_load, для PDF — read_pdf.", map[string]any{"path": stringProp, "offset": intProp, "limit": intProp}, []string{"path"}, func(_ context.Context, args map[string]any) (any, error) {
		b, err := a.read(str(args, "path"))
		if err != nil {
			return nil, err
		}
		off, n := integer(args, "offset", 0), integer(args, "limit", 12000)
		if off < 0 || n < 1 || n > 24000 {
			return nil, fmt.Errorf("offset >=0, limit 1..24000")
		}
		end := min(len(b), min(off, len(b))+n)
		return map[string]any{"text": string(b[min(off, len(b)):end]), "next_offset": end, "total_bytes": len(b)}, nil
	})
	add("save_local_json", "Сохранить структурированные данные в JSON-файл браузерной вкладки. Можно скачать на вкладке Файлы.", map[string]any{"path": stringProp, "data": map[string]any{}}, []string{"path", "data"}, func(_ context.Context, args map[string]any) (any, error) {
		name := str(args, "path")
		if name == "" {
			return nil, fmt.Errorf("path обязателен")
		}
		b, err := json.Marshal(args["data"])
		if err != nil {
			return nil, err
		}
		return map[string]any{"path": name, "size": len(b)}, a.put(name, b)
	})
	for _, name := range []string{"read_pdf", "index_pdf"} {
		add(name, "Извлечь PDF без OCR: текст, ссылки и изображения / индекс строк. Не более 10 страниц за вызов, проверяйте has_more.", map[string]any{"path": stringProp, "start_page": intProp, "end_page": intProp}, []string{"path"}, func(ctx context.Context, args map[string]any) (any, error) {
			b, err := a.read(str(args, "path"))
			if err != nil {
				return nil, err
			}
			o := pdfsource.Options{StartPage: integer(args, "start_page", 1), EndPage: integer(args, "end_page", 0)}
			if name == "index_pdf" {
				return pdfsource.Index(ctx, b, o)
			}
			return pdfsource.Read(ctx, b, o)
		})
	}
	add("extract_pdf", "Применить точную mapping-схему к PDF; извлекает исходный текст без пересказа. Сначала index_pdf.", map[string]any{"path": stringProp, "mapping_path": stringProp, "output_path": stringProp}, []string{"path", "mapping_path", "output_path"}, func(ctx context.Context, args map[string]any) (any, error) {
		b, err := a.read(str(args, "path"))
		if err != nil {
			return nil, err
		}
		m, err := a.read(str(args, "mapping_path"))
		if err != nil {
			return nil, err
		}
		out, err := pdfsource.Extract(ctx, b, m)
		if err != nil {
			return nil, err
		}
		if err = a.put(str(args, "output_path"), out.Data); err != nil {
			return nil, err
		}
		return map[string]any{"path": str(args, "output_path"), "pages": out.PageCount, "referenced_lines": out.ReferencedLines, "ignored_lines": out.IgnoredLines}, nil
	})
	add("validate_scenario", "Проверить полный JSON-сценарий или пакет уровней теми же Go-валидаторами, что CLI; сохранить нормализованный JSON без публикации в движок.", map[string]any{"path": stringProp, "output_path": stringProp}, []string{"path"}, func(_ context.Context, args map[string]any) (any, error) {
		b, err := a.read(str(args, "path"))
		if err != nil {
			return nil, err
		}
		var meta map[string]any
		if err = json.Unmarshal(b, &meta); err != nil {
			return nil, err
		}
		var normalized []byte
		var count int
		if _, ok := meta["format"]; ok {
			s, err := gamesource.DecodeScenario(b)
			if err != nil {
				return nil, err
			}
			if err = gamesource.ValidateScenario(s); err != nil {
				return nil, err
			}
			count = len(s.Levels)
			normalized, err = gamesource.EncodeScenario(s)
			if err != nil {
				return nil, err
			}
		} else {
			p, err := gamesource.Decode(b)
			if err != nil {
				return nil, err
			}
			count = len(p.Levels)
			normalized, err = json.Marshal(p)
			if err != nil {
				return nil, err
			}
		}
		out := str(args, "output_path")
		if out != "" {
			if err = a.put(out, normalized); err != nil {
				return nil, err
			}
		}
		return map[string]any{"valid": true, "levels": count, "output_path": out}, nil
	})
	return list
}
