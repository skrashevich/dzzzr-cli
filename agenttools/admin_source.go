package agenttools

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
	"github.com/skrashevich/dzzzr-cli/gamesource"
)

func uploadRoot(root string) (string, error) {
	if root == "" {
		root = os.Getenv("DZZR_FILES_ROOT")
	}
	if root == "" {
		root = "."
	}
	return filepath.Abs(root)
}

// readUpload uses os.Root so symlinks and concurrent path changes cannot escape
// the configured file capability. Mutating tools read after mutation approval.
func readUpload(root, path string) ([]byte, error) {
	return readUploadLimited(root, path, dzzzr.MaxAdminFileBytes)
}

func readUploadLimited(root, path string, maxBytes int64) ([]byte, error) {
	if filepath.IsAbs(path) {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil, err
		}
		path = rel
	}
	if !filepath.IsLocal(path) {
		return nil, fmt.Errorf("path must stay inside DZZR_FILES_ROOT")
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }()
	before, err := dir.Stat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("path must be a regular file")
	}
	f, err := dir.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("path must be a regular file")
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("file exceeds %d bytes", maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file exceeds %d bytes", maxBytes)
	}
	return data, nil
}

func sourcePlanSchema() map[string]any {
	mode := strProp("create — добавить уровни; update — полностью заменить все поля существующих уровней, включая нули, пустой текст и списки; отсутствующие флаги выключаются")
	mode["enum"] = []string{"create", "update"}
	level := schema(map[string]any{"id": intProp("ID уровня: 0 для create, положительный для update"), "params": levelParamsSchema()}, "params")
	level["additionalProperties"] = false
	s := schema(map[string]any{
		"game_id": intProp("ID игры"), "mode": mode,
		"levels": map[string]any{"type": "array", "minItems": 1, "items": level},
	}, "game_id", "mode", "levels")
	s["additionalProperties"] = false
	return s
}

func sourceInputSchema() map[string]any {
	s := schema(map[string]any{
		"plan":    sourcePlanSchema(),
		"path":    strProp("Путь к локальному JSON исходника внутри DZZR_FILES_ROOT, до 4 МиБ; передать только plan или path"),
		"game_id": intProp("Положительный ID целевой игры; переопределение только для path, из запроса пользователя"),
		"mode":    map[string]any{"type": "string", "enum": []string{"create", "update"}, "description": "Режим только для path; переопределяет режим файла"},
	})
	s["oneOf"] = []any{
		map[string]any{"required": []string{"plan"}, "not": map[string]any{"anyOf": []any{map[string]any{"required": []string{"path"}}, map[string]any{"required": []string{"game_id"}}, map[string]any{"required": []string{"mode"}}}}},
		map[string]any{"required": []string{"path"}, "not": map[string]any{"required": []string{"plan"}}},
	}
	return s
}

func decodeSourcePlan(args arguments, root string) (*gamesource.Plan, error) {
	_, hasPlan := args["plan"]
	_, hasPath := args["path"]
	if hasPlan == hasPath {
		return nil, fmt.Errorf("exactly one of plan or path is required")
	}
	if hasPath {
		path, err := args.requireString("path")
		if err != nil {
			return nil, err
		}
		data, err := readUploadLimited(root, path, 4<<20)
		if err != nil {
			return nil, err
		}
		// Keep every source field as raw JSON: decoding numbers through any would
		// round large integers before the strict plan decoder can validate them.
		var fields map[string]jsontext.Value
		if err := json.Unmarshal(data, &fields); err != nil {
			return nil, fmt.Errorf("game source JSON: %w", err)
		}
		if fields == nil {
			return nil, fmt.Errorf("game source JSON must be an object")
		}
		if _, present := args["game_id"]; present {
			id, err := requireAdminInt(args, "game_id")
			if err != nil {
				return nil, err
			}
			if id <= 0 {
				return nil, fmt.Errorf("game_id must be positive")
			}
			fields["game_id"], err = json.Marshal(id)
			if err != nil {
				return nil, err
			}
		}
		if _, present := args["mode"]; present {
			mode, ok := args["mode"].(string)
			if !ok || (mode != "create" && mode != "update") {
				return nil, fmt.Errorf("mode must be create or update")
			}
			fields["mode"], err = json.Marshal(mode)
			if err != nil {
				return nil, err
			}
		}
		data, err = json.Marshal(fields)
		if err != nil {
			return nil, err
		}
		return gamesource.Decode(data)
	}
	for _, key := range []string{"game_id", "mode"} {
		if _, present := args[key]; present {
			return nil, fmt.Errorf("%s override requires path", key)
		}
	}

	raw, ok := args["plan"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("plan must be an object")
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	return gamesource.Decode(data)
}

func adminSourceTools(e Engine, g *gate, root string) []*Tool {
	return []*Tool{
		{
			name:        "admin_upload_file",
			description: "Загрузить локальную картинку или вложение в файловый менеджер игры и получить URL для HTML задания. Нужен аккаунт игротехника. Уже существующий файл не перезаписывается. Путь внутри DZZR_FILES_ROOT (по умолчанию рабочий каталог).",
			parameters:  schema(map[string]any{"game_id": intProp("ID игры"), "path": strProp("Путь к локальному файлу внутри корня"), "name": strProp("Имя в движке; по умолчанию имя файла")}, "game_id", "path"),
			mutating:    true, gate: g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID <= 0 {
					return nil, fmt.Errorf("game_id must be positive")
				}
				path, err := args.requireString("path")
				if err != nil {
					return nil, err
				}
				name := filepath.Base(path)
				if raw, present := args["name"]; present {
					var ok bool
					name, ok = raw.(string)
					if !ok || strings.TrimSpace(name) == "" {
						return nil, fmt.Errorf("name must be a non-empty string")
					}
				}
				data, err := readUpload(root, path)
				if err != nil {
					return nil, err
				}
				result, err := e.AdminUploadFile(ctx, gameID, name, data)
				if err != nil {
					return nil, err
				}
				return result, nil
			},
		},
		{
			name:        "admin_create_technical_level",
			description: "По отдельной просьбе добавить технический сквозной бонусный уровень DzrSourceHelper для интеграции where.games (загружает её внешние скрипты). Используется не во всех играх; не вызывать автоматически после заливки.",
			parameters:  schema(map[string]any{"game_id": intProp("ID игры")}, "game_id"),
			mutating:    true, gate: g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID <= 0 {
					return nil, fmt.Errorf("game_id must be positive")
				}
				id, err := e.AdminCreateTechnicalLevel(ctx, gameID)
				if err != nil {
					return nil, err
				}
				return map[string]any{"id": id}, nil
			},
		},
		{
			name:        "admin_validate_source",
			noCache:     true,
			description: "Проверить JSON исходника игры из plan или локального path (до 4 МиБ внутри DZZR_FILES_ROOT), без переписывания полей моделью: типы, кодировку Windows-1251, коды, повторы, тайминги. Проверка локальная; существование ID и права проверяются при заливке. HTML передаётся как есть.",
			parameters:  sourceInputSchema(), gate: g,
			run: func(_ context.Context, args arguments) (any, error) {
				plan, err := decodeSourcePlan(args, root)
				if err != nil {
					return nil, err
				}
				if err := plan.Validate(); err != nil {
					return nil, err
				}
				return map[string]any{"valid": true, "game_id": plan.GameID, "mode": plan.Mode, "levels": len(plan.Levels)}, nil
			},
		},
		{
			name:        "admin_upload_source",
			description: "Залить уровни из JSON plan или локального path (до 4 МиБ внутри DZZR_FILES_ROOT) после проверки всего исходника; поля из файла передаются напрямую без переписывания моделью. create добавляет, update полностью заменяет поля уровней (неуказанные значения очищаются). Файлы загружаются отдельно через admin_upload_file, их URL вставляются в HTML. Технический уровень НЕ добавляется. При ошибке возвращаются уже записанные ID; не повторять весь create, иначе будут дубликаты.",
			parameters:  sourceInputSchema(), mutating: true, gate: g,
			run: func(ctx context.Context, args arguments) (any, error) {
				plan, err := decodeSourcePlan(args, root)
				if err != nil {
					return nil, err
				}
				result, err := plan.Apply(ctx, e)
				if err != nil {
					return result, err
				}
				return result, nil
			},
		},
	}
}
