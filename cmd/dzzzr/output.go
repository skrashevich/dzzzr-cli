package main

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// cliError ends the run with a chosen exit code. An empty message means the
// command already told the user what happened, so only the code matters.
type cliError struct {
	msg  string
	code int
}

// Error implements error.
func (e *cliError) Error() string { return e.msg }

// fatal builds the error that stops the command with exit code 1 and the
// given Russian message. Commands return it instead of exiting so that run
// stays callable from tests.
func fatal(format string, args ...any) error {
	return &cliError{msg: fmt.Sprintf(format, args...), code: 1}
}

// errRejected ends the run with exit code 2 after the result of a rejected
// engine action has already been printed.
var errRejected = &cliError{code: 2}

// reportError turns a command error into an exit code, translating the
// library's authentication failures into advice about the missing
// credential.
func reportError(cfg *config, err error) int {
	if err == nil {
		return 0
	}
	if hint := authHint(err); hint != nil {
		err = hint
	}
	if dzzzr.IsUndecodableAccepted(err) {
		printError(cfg, "движок принял запрос, но ответ не разобран: %v. Проверьте состояние командой «dzzzr status» и не отправляйте код повторно", err)
		return 1
	}
	if ce, ok := errors.AsType[*cliError](err); ok {
		if ce.msg != "" {
			printError(cfg, "%s", ce.msg)
		}
		return ce.code
	}
	printError(cfg, "%v", err)
	return 1
}

// authHint replaces a library authentication error with a message naming the
// credential the user has to fix, or returns nil when err is not one.
func authHint(err error) error {
	if !dzzzr.IsAuthError(err) {
		return nil
	}
	switch dzzzr.AuthErrorKindOf(err) {
	case dzzzr.AuthSession:
		return fatal("сессия сайта отсутствует или истекла. Выполните «dzzzr login»")
	case dzzzr.AuthBasic:
		return fatal("движок отклонил игровую авторизацию. Проверьте логин капитана и PIN: -captain/-pin (DZZZR_CAPTAIN/DZZZR_PIN)")
	case dzzzr.AuthAdmin:
		return fatal("движок отклонил учётные данные организатора. Проверьте -admin-login/-admin-password (DZZZR_ADMIN_LOGIN/DZZZR_ADMIN_PASSWORD)")
	case dzzzr.AuthAdminScope:
		// The library names the refused page or form verb; the wording
		// around it lives here, so the two cannot stutter into "нет
		// доступа к разделу: нет доступа к разделу teams".
		refused := ""
		var ae *dzzzr.AuthError
		if errors.As(err, &ae) && ae.Message != "" {
			refused = " (" + ae.Message + ")"
		}
		return fatal("учётные данные организатора верны, но у этого аккаунта нет доступа%s. Проверьте, назначен ли он организатором игры (author= в admin-game-info)", refused)
	}
	return fatal("ошибка авторизации: %v", err)
}

// printError writes the failure: a JSON object on stdout in -json mode so a
// caller parsing stdout always gets JSON, a Russian line on stderr otherwise.
func printError(cfg *config, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if cfg.jsonOut {
		_ = outputJSON(cfg, errorOutput{Error: msg})
		return
	}
	fmt.Fprintln(cfg.stderr, "Ошибка: "+msg)
}

// errorOutput is the -json form of a failure.
type errorOutput struct {
	Error string `json:"error"`
}

// outputJSON writes v to stdout as indented JSON followed by a newline.
func outputJSON(cfg *config, v any) error {
	if err := json.MarshalWrite(cfg.stdout, v, jsontext.WithIndent("  ")); err != nil {
		return fatal("не удалось сформировать JSON: %v", err)
	}
	if _, err := io.WriteString(cfg.stdout, "\n"); err != nil {
		return fatal("не удалось записать вывод: %v", err)
	}
	return nil
}

// newTable returns a tabwriter for the human-readable tables.
func newTable(w io.Writer) *tabwriter.Writer { return tabwriter.NewWriter(w, 0, 4, 2, ' ', 0) }

// formatDuration renders a number of seconds the way the engine's own
// countdowns read: "45с", "2м05с", "1ч02м03с".
func formatDuration(seconds int) string {
	if seconds < 0 {
		seconds = 0
	}
	h := seconds / 3600
	m := seconds % 3600 / 60
	s := seconds % 60
	switch {
	case h > 0:
		return fmt.Sprintf("%dч%02dм%02dс", h, m, s)
	case m > 0:
		return fmt.Sprintf("%dм%02dс", m, s)
	default:
		return fmt.Sprintf("%dс", s)
	}
}

// indent prefixes every line of a block of text, keeping multi-line level
// questions readable under a heading.
func indent(s, prefix string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}

// dash returns a placeholder for an empty value so tables stay aligned.
func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
