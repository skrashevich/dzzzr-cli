package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// stubGUILaunch makes the launch detector answer gui for one test.
func stubGUILaunch(t *testing.T, gui bool) {
	t.Helper()
	old := guiDetector
	t.Cleanup(func() { guiDetector = old })
	guiDetector = func() bool { return gui }
}

func TestAutoStartWebIfGUI(t *testing.T) {
	for _, tc := range []struct {
		name    string
		gui     bool
		killEnv string
		want    bool
	}{
		{name: "двойной клик", gui: true, want: true},
		{name: "запуск из терминала", gui: false, want: false},
		{name: "выключено переменной", gui: true, killEnv: "1", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubGUILaunch(t, tc.gui)
			t.Setenv(autoWebEnv, tc.killEnv)
			if got := autoStartWebIfGUI(); got != tc.want {
				t.Errorf("autoStartWebIfGUI() = %v, ожидалось %v", got, tc.want)
			}
		})
	}
}

// stubWebCommand replaces what «dzzzr web» does, so the wiring can be checked
// without binding a socket. It reports how many times the command ran and
// makes it fail with failure when that is not nil.
func stubWebCommand(t *testing.T, failure error) *int {
	t.Helper()
	c := findCommand("web")
	if c == nil {
		t.Fatal("команда web не зарегистрирована")
	}
	old := c.Run
	t.Cleanup(func() { c.Run = old })
	calls := 0
	c.Run = func(context.Context, *config, *dzzzr.Client, []string) error {
		calls++
		return failure
	}
	return &calls
}

// A double-click gives no command: the browser chat takes over instead of
// flashing the usage text in a console window that closes immediately.
func TestRunWithoutCommandOpensWebOnGUILaunch(t *testing.T) {
	isolate(t)
	stubGUILaunch(t, true)
	t.Setenv(autoWebEnv, "")
	calls := stubWebCommand(t, nil)

	var errOut strings.Builder
	if code := run(nil, io.Discard, &errOut); code != 0 {
		t.Errorf("код возврата = %d, ожидался 0", code)
	}
	if *calls != 1 {
		t.Errorf("команда web вызвана %d раз, ожидался 1", *calls)
	}
	if !strings.Contains(errOut.String(), "браузерный чат") {
		t.Errorf("пользователю не сказали, что происходит: %q", errOut.String())
	}
}

// Run from a shell, the caller keeps the usage text and the exit code.
func TestRunWithoutCommandKeepsUsageFromTerminal(t *testing.T) {
	isolate(t)
	stubGUILaunch(t, false)
	calls := stubWebCommand(t, nil)

	var errOut strings.Builder
	if code := run(nil, io.Discard, &errOut); code != 2 {
		t.Errorf("код возврата = %d, ожидался 2", code)
	}
	if *calls != 0 {
		t.Errorf("команда web вызвана %d раз, ожидался 0", *calls)
	}
	if !strings.Contains(errOut.String(), "Использование") && !strings.Contains(errOut.String(), "Команды") {
		t.Errorf("не напечатана справка: %q", errOut.String())
	}
}

// The console a double-click gets closes with the process. Reporting a failure
// and exiting would take the report down with the window, so the GUI path has
// to hold the window open until it is acknowledged.
func TestRunHoldsTheWindowWhenAutoWebFails(t *testing.T) {
	isolate(t)
	stubGUILaunch(t, true)
	t.Setenv(autoWebEnv, "")
	stubWebCommand(t, errors.New("порт занят"))

	var errOut strings.Builder
	if code := run(nil, io.Discard, &errOut); code == 0 {
		t.Error("провал команды web не отражён в коде возврата")
	}
	out := errOut.String()
	if !strings.Contains(out, "порт занят") {
		t.Errorf("ошибка не показана пользователю: %q", out)
	}
	if !strings.Contains(out, "Нажмите Enter") {
		t.Errorf("окно не удержано, сообщение закроется вместе с консолью: %q", out)
	}
}

// A successful run must not wait for anything: «dzzzr web» ends when the user
// stops it, and a prompt after that would hang the process.
func TestRunDoesNotHoldTheWindowOnSuccess(t *testing.T) {
	isolate(t)
	stubGUILaunch(t, true)
	t.Setenv(autoWebEnv, "")
	stubWebCommand(t, nil)

	var errOut strings.Builder
	if code := run(nil, io.Discard, &errOut); code != 0 {
		t.Errorf("код возврата = %d", code)
	}
	if strings.Contains(errOut.String(), "Нажмите Enter") {
		t.Error("удержание окна сработало на успешном запуске")
	}
}
