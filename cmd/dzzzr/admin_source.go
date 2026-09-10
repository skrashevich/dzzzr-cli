package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
	"github.com/skrashevich/dzzzr-cli/gamesource"
)

func init() {
	register(
		command{Name: "admin-upload-file", Usage: "admin-upload-file ИГРА ФАЙЛ [ИМЯ]", Auth: authAdmin, Run: cmdAdminUploadFile,
			Help: "Загрузить локальный файл до 32 МиБ; по умолчанию используется имя файла"},
		command{Name: "admin-create-technical-level", Usage: "admin-create-technical-level ИГРА", Auth: authAdmin, Run: cmdAdminCreateTechnicalLevel,
			Help: "Явно добавить необязательное техническое задание where.games"},
		command{Name: "admin-validate-source", Usage: "admin-validate-source ФАЙЛ", Auth: authNone, Run: cmdAdminValidateSource,
			Help: "Проверить локальный JSON-план заданий без подключения к движку"},
		command{Name: "admin-upload-source", Usage: "admin-upload-source ФАЙЛ", Auth: authAdmin, Run: cmdAdminUploadSource,
			Help: "Проверить и применить JSON-план; update заменяет все моделируемые поля, create не добавляет техническое задание"},
	)
}

// readAdminLocalFile bounds both the initial file size and the actual read if
// another process grows it. Checking before opening also rejects device files.
func readAdminLocalFile(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: требуется обычный файл", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: требуется обычный файл", path)
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("%s: размер превышает %d байт", path, limit)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s: размер превышает %d байт", path, limit)
	}
	return data, nil
}

func cmdAdminUploadFile(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if len(args) < 2 || len(args) > 3 {
		return fatal("использование: dzzzr admin-upload-file ИГРА ФАЙЛ [ИМЯ]")
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	data, err := readAdminLocalFile(args[1], dzzzr.MaxAdminFileBytes)
	if err != nil {
		return err
	}
	name := filepath.Base(args[1])
	if len(args) == 3 {
		name = args[2]
	}
	result, err := c.AdminUploadFile(ctx, gameID, name, data)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, result)
	}
	_, err = fmt.Fprintf(cfg.stdout, "%s\nУже существует: %s\n", result.URL, yesNo(result.AlreadyExists))
	return err
}

func cmdAdminCreateTechnicalLevel(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 1, "admin-create-technical-level ИГРА"); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	id, err := c.AdminCreateTechnicalLevel(ctx, gameID)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, idOutput{ID: id})
	}
	_, err = fmt.Fprintf(cfg.stdout, "Техническое задание %d добавлено в игру %d.\n", id, gameID)
	return err
}

func readAdminSource(args []string, usage string) (*gamesource.Plan, error) {
	if err := needExactArgs(args, 1, usage); err != nil {
		return nil, err
	}
	data, err := readAdminLocalFile(args[0], 32<<20)
	if err != nil {
		return nil, err
	}
	return gamesource.Decode(data)
}

func cmdAdminValidateSource(_ context.Context, cfg *config, _ *dzzzr.Client, args []string) error {
	plan, err := readAdminSource(args, "admin-validate-source ФАЙЛ")
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, plan)
	}
	_, err = fmt.Fprintf(cfg.stdout, "План корректен: игра %d, режим %s, заданий %d.\n", plan.GameID, plan.Mode, len(plan.Levels))
	return err
}

func cmdAdminUploadSource(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	plan, err := readAdminSource(args, "admin-upload-source ФАЙЛ")
	if err != nil {
		return err
	}
	result, applyErr := plan.Apply(ctx, c)
	// Always emit structured progress, including on failure. Creates cannot be
	// rolled back, and the failed row may need inspection before a retry.
	if cfg.jsonOut && applyErr != nil {
		if err := outputJSON(cfg, struct {
			*gamesource.Result
			Error string `json:"error"`
		}{Result: result, Error: applyErr.Error()}); err != nil {
			return err
		}
		return &cliError{code: 1}
	}
	if err := outputJSON(cfg, result); err != nil {
		return err
	}
	return applyErr
}
