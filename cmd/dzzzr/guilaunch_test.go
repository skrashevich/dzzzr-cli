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

func TestAutoStartEditor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		gui     bool
		killEnv string
		want    bool
	}{
		{name: "двойной клик", gui: true, want: true},
		{name: "запуск из терминала", gui: false, want: false},
		{name: "выключено переменной", gui: true, killEnv: "1", want: false},
		{name: "выключено нулём", gui: true, killEnv: "0", want: false},
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

// stubEditorCommand replaces what «dzzzr editor» does, so the wiring can be checked
// without binding a socket. It reports how many times the command ran and
// makes it fail with failure when that is not nil.
func stubEditorCommand(t *testing.T, failure error) *int {
	t.Helper()
	c := findCommand("editor")
	if c == nil {
		t.Fatal("команда editor не зарегистрирована")
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

// A double-click gives no command: the browser editor takes over instead of
// flashing the usage text in a console window that closes immediately.
func TestRunWithoutCommandOpensEditorOnGUILaunch(t *testing.T) {
	isolate(t)
	stubGUILaunch(t, true)
	t.Setenv(autoWebEnv, "")
	calls := stubEditorCommand(t, nil)
	web := findCommand("web")
	old := web.Run
	t.Cleanup(func() { web.Run = old })
	web.Run = func(context.Context, *config, *dzzzr.Client, []string) error {
		return errors.New("unexpected web command")
	}

	var errOut strings.Builder
	if code := run(nil, io.Discard, &errOut); code != 0 {
		t.Errorf("код возврата = %d, ожидался 0", code)
	}
	if *calls != 1 {
		t.Errorf("команда editor вызвана %d раз, ожидался 1", *calls)
	}
	if !strings.Contains(errOut.String(), "редактор") {
		t.Errorf("пользователю не сказали, что происходит: %q", errOut.String())
	}
}

// A terminal launch keeps the usage text and the exit code.
func TestRunWithoutCommandKeepsUsageFromTerminal(t *testing.T) {
	isolate(t)
	stubGUILaunch(t, false)
	calls := stubEditorCommand(t, nil)

	var errOut strings.Builder
	if code := run(nil, io.Discard, &errOut); code != 2 {
		t.Errorf("код возврата = %d, ожидался 2", code)
	}
	if *calls != 0 {
		t.Errorf("команда editor вызвана %d раз, ожидался 0", *calls)
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
	stubEditorCommand(t, errors.New("порт занят"))

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

// A successful run must not wait for anything: «dzzzr editor» ends when the user
// stops it, and a prompt after that would hang the process.
func TestRunDoesNotHoldTheWindowOnSuccess(t *testing.T) {
	isolate(t)
	stubGUILaunch(t, true)
	t.Setenv(autoWebEnv, "")
	stubEditorCommand(t, nil)

	var errOut strings.Builder
	if code := run(nil, io.Discard, &errOut); code != 0 {
		t.Errorf("код возврата = %d", code)
	}
	if strings.Contains(errOut.String(), "Нажмите Enter") {
		t.Error("удержание окна сработало на успешном запуске")
	}
}

func TestExplicitCommandsBypassAutoEditor(t *testing.T) {
	isolate(t)
	stubGUILaunch(t, true)
	t.Setenv(autoWebEnv, "")
	calls := stubEditorCommand(t, nil)
	for _, args := range [][]string{{"-h"}, {"-version"}, {"games", "-h"}} {
		if code := run(args, io.Discard, io.Discard); code != 0 {
			t.Errorf("run(%v) = %d", args, code)
		}
	}
	t.Setenv(autoWebEnv, "1")
	if code := run(nil, io.Discard, io.Discard); code != 2 {
		t.Errorf("disabled auto-start: exit = %d", code)
	}
	if *calls != 0 {
		t.Errorf("editor unexpectedly started %d times", *calls)
	}
}

func TestGUIFromLaunchContext(t *testing.T) {
	for _, parent := range []string{"explorer.exe", "EXPLORER.EXE", "cmd.exe", "powershell.exe", "pwsh.exe", "bash.exe", "wsl.exe"} {
		for _, count := range []int{0, 1, 2, 3} {
			want := strings.EqualFold(parent, "explorer.exe")
			if got := guiFromLaunchContext(parent, count); got != want {
				t.Errorf("parent %s, console count %d: got %v, want %v", parent, count, got, want)
			}
		}
	}
	for _, count := range []int{0, 1, 2} {
		if got := guiFromLaunchContext("", count); got != (count == 1) {
			t.Errorf("unknown parent, count %d: %v", count, got)
		}
	}
}
