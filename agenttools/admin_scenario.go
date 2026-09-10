package agenttools

import (
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"path/filepath"

	"github.com/skrashevich/dzzzr-cli/gamesource"
)

func scenarioLocalPath(root, name string) (string, error) {
	if filepath.IsAbs(name) {
		var err error
		name, err = filepath.Rel(root, name)
		if err != nil {
			return "", err
		}
	}
	if !filepath.IsLocal(name) {
		return "", fmt.Errorf("path must stay inside DZZZR_FILES_ROOT")
	}
	return name, nil
}

func toolScenario(args arguments, root string) (*gamesource.Scenario, string, error) {
	name, err := args.requireString("path")
	if err != nil {
		return nil, "", err
	}
	name, err = scenarioLocalPath(root, name)
	if err != nil {
		return nil, "", err
	}
	data, err := readUploadLimited(root, name, gamesource.MaxScenarioBytes)
	if err != nil {
		return nil, "", err
	}
	s, err := gamesource.DecodeScenario(data)
	if err != nil {
		return nil, "", err
	}
	// Resolve package assets through the same capability root, not a newly
	// opened subdirectory that could be redirected by a symlink.
	total := 0
	for key, a := range s.Assets {
		if a.Path != "" {
			data, err := readUploadLimited(root, filepath.Join(filepath.Dir(name), filepath.FromSlash(a.Path)), 32<<20)
			if err != nil {
				return nil, "", fmt.Errorf("asset %s: %w", key, err)
			}
			// Keep validation and SHA checks in the common importer.
			total += len(data)
			if total > 64<<20 {
				return nil, "", fmt.Errorf("scenario assets exceed 64 MiB")
			}
			a.DataBase64 = new(base64.StdEncoding.EncodeToString(data))
			a.Path = ""
			s.Assets[key] = a
		}
	}
	return s, name, nil
}

func adminScenarioTools(e Engine, g *gate, root string) []*Tool {
	return []*Tool{
		{
			name:        "admin_export_scenario",
			description: "Экспорт полного сценария игры в новый JSON-файл внутри DZZZR_FILES_ROOT. По умолчанию изображения и вложения встроены в Base64. linked_assets=true оставляет URL. Существующий файл не перезаписывается; возвращаются путь и счётчики без содержимого сценария.",
			parameters:  schema(map[string]any{"game_id": intProp("ID игры"), "path": strProp("Новый файл внутри DZZZR_FILES_ROOT"), "linked_assets": boolProp("Сохранить ссылки без скачивания")}, "game_id", "path"),
			gate:        g, noCache: true, writesLocal: true,
			run: func(ctx context.Context, args arguments) (any, error) {
				id, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				name, err := args.requireString("path")
				if err != nil {
					return nil, err
				}
				name, err = scenarioLocalPath(root, name)
				if err != nil {
					return nil, err
				}
				linked := false
				if value, ok := args["linked_assets"]; ok {
					var valid bool
					linked, valid = value.(bool)
					if !valid {
						return nil, fmt.Errorf("linked_assets must be boolean")
					}
				}
				base := ""
				if provider, ok := e.(interface{ BaseURL() string }); ok {
					base = provider.BaseURL()
				}
				s, err := gamesource.ExportScenario(ctx, e, id, gamesource.ExportOptions{BaseURL: base, LinkedAssets: linked})
				if err != nil {
					return nil, err
				}
				data, err := gamesource.EncodeScenario(s)
				if err != nil {
					return nil, err
				}
				if err := gamesource.WriteScenarioFile(root, name, data); err != nil {
					return nil, err
				}
				return map[string]any{"path": name, "levels": len(s.Levels), "assets": len(s.Assets), "embedded_assets": !linked}, nil
			},
		},
		{
			name:        "admin_validate_scenario",
			description: "Проверить полный JSON-сценарий из файла внутри DZZZR_FILES_ROOT. Без записи в движок; удалённые файлы не скачиваются.",
			parameters:  schema(map[string]any{"path": strProp("Файл полного сценария")}, "path"), gate: g, noCache: true,
			run: func(_ context.Context, args arguments) (any, error) {
				s, _, err := toolScenario(args, root)
				if err != nil {
					return nil, err
				}
				if err := gamesource.ValidateScenario(s); err != nil {
					return nil, err
				}
				return map[string]any{"valid": true, "levels": len(s.Levels), "assets": len(s.Assets), "payloads_checked": false}, nil
			},
		},
		{
			name:        "admin_import_scenario",
			description: "Импорт полного сценария из path. Без game_id создаёт новую игру; с game_id пишет в явно указанную игру. Для непустой игры обязательна полная level_ids: ключ сценария -> ID уровня назначения, с равным числом уровней. Публикация восстанавливается последней. При ошибке возвращает этап и уже записанные ID; автоматически повторять импорт нельзя.",
			parameters:  schema(map[string]any{"path": strProp("Файл сценария внутри DZZZR_FILES_ROOT"), "game_id": intProp("Положительный ID назначения; не задан -> новая игра"), "level_ids": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "integer", "minimum": 1}}}, "path"),
			gate:        g, mutating: true,
			run: func(ctx context.Context, args arguments) (any, error) {
				engine, ok := e.(gamesource.ScenarioEngine)
				if !ok {
					return nil, fmt.Errorf("engine does not support full game replacement")
				}
				opts := gamesource.ImportOptions{Files: gamesource.ScenarioFiles{Root: root}}
				if _, ok := args["game_id"]; ok {
					id, err := requireAdminInt(args, "game_id")
					if err != nil {
						return nil, err
					}
					if id <= 0 {
						return nil, fmt.Errorf("game_id must be positive")
					}
					opts.GameID = id
				}
				if value, ok := args["level_ids"]; ok {
					data, err := json.Marshal(value)
					if err != nil {
						return nil, err
					}
					if err := json.Unmarshal(data, &opts.LevelIDs); err != nil {
						return nil, err
					}
					if opts.LevelIDs == nil {
						return nil, fmt.Errorf("level_ids must be an object")
					}
				}
				s, _, err := toolScenario(args, root)
				if err != nil {
					return nil, err
				}
				return gamesource.ImportScenario(ctx, engine, s, opts)
			},
		},
	}
}
