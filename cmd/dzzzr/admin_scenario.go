package main

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"path/filepath"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
	"github.com/skrashevich/dzzzr-cli/gamesource"
)

func init() {
	register(
		command{Name: "admin-export-scenario", Usage: "admin-export-scenario ИГРА ФАЙЛ [linked]", Auth: authAdmin, Run: cmdAdminExportScenario, Help: "Экспорт полного сценария; файлы встроены в Base64, linked сохраняет ссылки"},
		command{Name: "admin-import-scenario", Usage: "admin-import-scenario ФАЙЛ [ИГРА [КАРТА-УРОВНЕЙ.json]]", Auth: authAdmin, Run: cmdAdminImportScenario, Help: "Импорт полного сценария в новую или явно указанную игру; для непустой игры нужна карта ключ -> ID"},
		command{Name: "admin-validate-scenario", Usage: "admin-validate-scenario ФАЙЛ", Auth: authNone, Run: cmdAdminValidateScenario, Help: "Проверить полный сценарий без обращения к движку; файлы проверяются при импорте"},
	)
}

func cmdAdminExportScenario(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if len(args) < 2 || len(args) > 3 {
		return fatal("admin-export-scenario ИГРА ФАЙЛ [linked]")
	}
	id, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	linked := len(args) == 3
	if linked && args[2] != "linked" {
		return fatal("неизвестный параметр %s", args[2])
	}
	s, err := gamesource.ExportScenario(ctx, c, id, gamesource.ExportOptions{BaseURL: c.BaseURL(), LinkedAssets: linked})
	if err != nil {
		return err
	}
	data, err := gamesource.EncodeScenario(s)
	if err != nil {
		return err
	}
	if args[1] == "-" {
		_, err = cfg.stdout.Write(append(data, '\n'))
		return err
	}
	if err := gamesource.WriteScenarioFile(filepath.Dir(args[1]), filepath.Base(args[1]), data); err != nil {
		return err
	}
	return outputJSON(cfg, map[string]any{"path": args[1], "levels": len(s.Levels), "assets": len(s.Assets), "embedded_assets": !linked})
}

func readScenario(path string) (*gamesource.Scenario, error) {
	data, err := readAdminLocalFile(path, gamesource.MaxScenarioBytes)
	if err != nil {
		return nil, err
	}
	return gamesource.DecodeScenario(data)
}

func cmdAdminValidateScenario(_ context.Context, cfg *config, _ *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 1, "admin-validate-scenario ФАЙЛ"); err != nil {
		return err
	}
	s, err := readScenario(args[0])
	if err != nil {
		return err
	}
	if err := gamesource.ValidateScenario(s); err != nil {
		return err
	}
	return outputJSON(cfg, map[string]any{"valid": true, "levels": len(s.Levels), "assets": len(s.Assets), "payloads_checked": false})
}

func cmdAdminImportScenario(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if len(args) < 1 || len(args) > 3 {
		return fatal("admin-import-scenario ФАЙЛ [ИГРА [КАРТА-УРОВНЕЙ.json]]")
	}
	s, err := readScenario(args[0])
	if err != nil {
		return err
	}
	opts := gamesource.ImportOptions{Files: gamesource.ScenarioFiles{Root: filepath.Dir(args[0])}}
	if len(args) > 1 {
		opts.GameID, err = parseID("номер игры", args[1])
		if err != nil {
			return err
		}
	}
	if len(args) > 2 {
		data, err := readAdminLocalFile(args[2], 1<<20)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &opts.LevelIDs); err != nil {
			return fmt.Errorf("карта уровней: %w", err)
		}
	}
	result, applyErr := gamesource.ImportScenario(ctx, c, s, opts)
	if err := outputJSON(cfg, result); err != nil {
		return err
	}
	return applyErr
}
