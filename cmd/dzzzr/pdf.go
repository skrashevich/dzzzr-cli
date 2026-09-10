package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
	"github.com/skrashevich/dzzzr-cli/pdfsource"
)

func init() {
	register(
		command{Name: "pdf-index", Usage: "pdf-index PDF [НАЧАЛО КОНЕЦ]", Auth: authNone, Run: cmdPDFIndex,
			Help: "Индекс строк локального PDF в JSON; пути относительно DZZR_FILES_ROOT (по умолчанию текущий каталог)"},
		command{Name: "pdf-extract", Usage: "pdf-extract PDF СХЕМА.json РЕЗУЛЬТАТ.json", Auth: authNone, Run: cmdPDFExtract,
			Help: "Скопировать строки PDF в JSON по схеме ссылок на источник; пути относительно DZZR_FILES_ROOT"},
	)
}

func cmdPDFIndex(ctx context.Context, cfg *config, _ *dzzzr.Client, args []string) error {
	if len(args) != 1 && len(args) != 3 {
		return &cliError{msg: "использование: pdf-index PDF [НАЧАЛО КОНЕЦ]", code: 2}
	}
	var opts pdfsource.Options
	if len(args) == 3 {
		start, err := strconv.Atoi(args[1])
		if err != nil || start < 1 {
			return &cliError{msg: "начальная страница должна быть положительным целым числом", code: 2}
		}
		end, err := strconv.Atoi(args[2])
		if err != nil || end < start || end-start >= pdfsource.MaxPages {
			return &cliError{msg: fmt.Sprintf("конечная страница должна быть не меньше начальной; не более %d страниц за вызов", pdfsource.MaxPages), code: 2}
		}
		opts = pdfsource.Options{StartPage: start, EndPage: end}
	}
	indexed, err := pdfsource.IndexFile(ctx, envOr("DZZR_FILES_ROOT", "."), args[0], opts)
	if err != nil {
		return err
	}
	return outputJSON(cfg, indexed)
}

func cmdPDFExtract(ctx context.Context, cfg *config, _ *dzzzr.Client, args []string) error {
	if len(args) != 3 {
		return &cliError{msg: "использование: pdf-extract PDF СХЕМА.json РЕЗУЛЬТАТ.json", code: 2}
	}
	result, err := pdfsource.ExtractFiles(ctx, envOr("DZZR_FILES_ROOT", "."), args[0], args[1], args[2])
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, result)
	}
	_, err = fmt.Fprintf(cfg.stdout, "%s: %d байт, страниц: %d, строк скопировано: %d, пропущено явно: %d\n", result.Path, result.Bytes, result.PageCount, result.ReferencedLines, result.IgnoredLines)
	return err
}
