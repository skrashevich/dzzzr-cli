package main

import (
	"bytes"
	"encoding/json/v2"
	"flag"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// dzzrEnv lists every environment variable the CLI reads, so a test can
// neutralize a developer's own settings.
var dzzrEnv = []string{
	"DZZZR_CITY", "DZZZR_LOGIN", "DZZZR_PASSWORD", "DZZZR_CAPTAIN", "DZZZR_PIN",
	"DZZZR_ADMIN_LOGIN", "DZZZR_ADMIN_PASSWORD", "DZZZR_BASE_URL",
	"DZZZR_INSECURE", "DZZZR_DEBUG", "DZZZR_HAR", "DZZZR_HAR_OUT",
	"DZZZR_LLM_API_KEY", "DZZZR_LLM_BASE_URL", "DZZZR_LLM_MODEL", "DZZZR_FILES_ROOT",
	"DZZZR_LLM_PROVIDER", "DZZZR_CODEX_AUTH_FILE", "DZZZR_CONFIG_DIR",
	"DZZZR_LLM_SOURCE_CONTEXT_BYTES",
	"DZZZR_LLM_REQUEST_TIMEOUT_SECONDS",
	"LLM_API_KEY", "LLM_BASE_URL", "LLM_MODEL",
	"OPENROUTER_API_KEY", "OPENROUTER_BASE_URL", "OPENROUTER_MODEL",
}

// isolate gives the test its own HOME and an empty DZZZR_* environment, and
// returns that home. DZZZR_CONFIG_DIR pins the configuration under it: HOME
// alone would not, because os.UserHomeDir reads %USERPROFILE% on Windows and
// the test would write into the real profile there.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, k := range dzzrEnv {
		t.Setenv(k, "")
	}
	t.Setenv("DZZZR_CONFIG_DIR", filepath.Join(home, ".config", "dzzzr"))
	return home
}

// checkFileMode asserts the permissions of a path where the filesystem keeps
// any: on Windows a file carries access rules instead of mode bits, nothing
// there honours the 0600 the code asks for, and os.Stat reports a mode no
// chmod ever set. Everything else about the file is still checked there.
func checkFileMode(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	if perm := info.Mode().Perm(); perm != want {
		t.Errorf("права %s = %04o, ожидались %04o", path, perm, want)
	}
}

// runCLI executes one invocation and captures both streams.
func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestEveryCommandHasHelpAndRun(t *testing.T) {
	if len(commands) == 0 {
		t.Fatal("таблица команд пуста")
	}
	seen := map[string]bool{}
	for _, c := range commands {
		if c.Name == "" {
			t.Fatal("команда без имени")
		}
		if seen[c.Name] {
			t.Errorf("команда %q объявлена дважды", c.Name)
		}
		seen[c.Name] = true
		if c.Usage == "" {
			t.Errorf("%s: пустая строка использования", c.Name)
		}
		if !strings.HasPrefix(c.Usage, c.Name) {
			t.Errorf("%s: строка использования %q не начинается с имени команды", c.Name, c.Usage)
		}
		if c.Help == "" {
			t.Errorf("%s: пустая справка", c.Name)
		}
		if c.Run == nil {
			t.Errorf("%s: не задан обработчик", c.Name)
		}
		if c.Auth < authNone || c.Auth > authAdmin {
			t.Errorf("%s: неизвестный уровень доступа %d", c.Name, c.Auth)
		}
		if findCommand(c.Name) == nil {
			t.Errorf("%s: не находится через findCommand", c.Name)
		}
	}
	if findCommand("no-such-command") != nil {
		t.Error("findCommand вернул несуществующую команду")
	}
}

// parseFor runs the argument splitter over a fresh FlagSet.
func parseFor(t *testing.T, args []string) (*config, []string) {
	t.Helper()
	cfg := &config{stdout: io.Discard, stderr: io.Discard}
	fs := flag.NewFlagSet("dzzzr", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	cfg.registerFlags(fs)
	positional, err := splitArgs(fs, args)
	if err != nil {
		t.Fatalf("splitArgs(%q): %v", args, err)
	}
	return cfg, positional
}

func TestParseArgsFlagsBeforeAndAfterCommand(t *testing.T) {
	isolate(t)
	tests := []struct {
		name       string
		args       []string
		positional []string
		check      func(*testing.T, *config)
	}{
		{
			name:       "flags before command",
			args:       []string{"-city", "spb", "-json", "status"},
			positional: []string{"status"},
			check: func(t *testing.T, cfg *config) {
				if cfg.city != "spb" || !cfg.jsonOut {
					t.Errorf("city=%q json=%v", cfg.city, cfg.jsonOut)
				}
			},
		},
		{
			name:       "flags after command",
			args:       []string{"status", "-city", "spb", "-json"},
			positional: []string{"status"},
			check: func(t *testing.T, cfg *config) {
				if cfg.city != "spb" || !cfg.jsonOut {
					t.Errorf("city=%q json=%v", cfg.city, cfg.jsonOut)
				}
			},
		},
		{
			name:       "flags between arguments",
			args:       []string{"send-bonus", "3", "-json", "B1", "-city", "spb"},
			positional: []string{"send-bonus", "3", "B1"},
			check: func(t *testing.T, cfg *config) {
				if cfg.city != "spb" || !cfg.jsonOut {
					t.Errorf("city=%q json=%v", cfg.city, cfg.jsonOut)
				}
			},
		},
		{
			name:       "command flags",
			args:       []string{"games", "-new", "-archive"},
			positional: []string{"games"},
			check: func(t *testing.T, cfg *config) {
				if !cfg.newOnly || !cfg.archive {
					t.Errorf("new=%v archive=%v", cfg.newOnly, cfg.archive)
				}
			},
		},
		{
			name:       "double dash stops flag parsing",
			args:       []string{"send-message", "--", "-не флаг", "-json"},
			positional: []string{"send-message", "-не флаг", "-json"},
			check: func(t *testing.T, cfg *config) {
				if cfg.jsonOut {
					t.Error("-json после -- не должен разбираться как флаг")
				}
			},
		},
		{
			name:       "no arguments",
			args:       nil,
			positional: nil,
			check:      func(*testing.T, *config) {},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, positional := parseFor(t, tc.args)
			if len(positional) != len(tc.positional) {
				t.Fatalf("позиционные аргументы = %q, ожидались %q", positional, tc.positional)
			}
			for i := range positional {
				if positional[i] != tc.positional[i] {
					t.Fatalf("позиционные аргументы = %q, ожидались %q", positional, tc.positional)
				}
			}
			tc.check(t, cfg)
		})
	}
}

func TestParseArgsEnvironmentDefaults(t *testing.T) {
	isolate(t)
	t.Setenv("DZZZR_CITY", "ekb")
	t.Setenv("DZZZR_CAPTAIN", "cap")
	t.Setenv("DZZZR_DEBUG", "1")
	cfg, positional := parseFor(t, []string{"status"})
	if cfg.city != "ekb" || cfg.captain != "cap" || !cfg.debug {
		t.Fatalf("город=%q капитан=%q debug=%v", cfg.city, cfg.captain, cfg.debug)
	}
	// A flag still wins over the environment.
	cfg2, _ := parseFor(t, []string{"-city", "moscow", "status"})
	if cfg2.city != "moscow" {
		t.Fatalf("флаг -city не переопределил DZZZR_CITY: %q", cfg2.city)
	}
	if len(positional) != 1 {
		t.Fatalf("позиционные аргументы = %q", positional)
	}
}

func TestSessionFilePath(t *testing.T) {
	home := isolate(t)
	path, err := sessionPath("moscow")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config", "dzzzr", "moscow.json")
	if path != want {
		t.Fatalf("путь сессии = %q, ожидался %q", path, want)
	}
}

func TestSessionDirHonoursOverride(t *testing.T) {
	isolate(t)
	custom := filepath.Join(t.TempDir(), "настройки")
	t.Setenv("DZZZR_CONFIG_DIR", custom)
	dir, err := sessionDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != custom {
		t.Fatalf("каталог настроек = %q, ожидался %q", dir, custom)
	}
}

// Without the override the platform decides. Unix follows XDG_CONFIG_HOME
// and ignores it when it is relative, as the specification requires.
func TestSessionDirFollowsXDGOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("на Windows настройки лежат в %AppData%")
	}
	home := isolate(t)
	t.Setenv("DZZZR_CONFIG_DIR", "")

	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	dir, err := sessionDir()
	if want := filepath.Join(xdg, "dzzzr"); err != nil || dir != want {
		t.Fatalf("каталог настроек = %q (%v), ожидался %q", dir, err, want)
	}

	t.Setenv("XDG_CONFIG_HOME", filepath.Join("относительный", "путь"))
	dir, err = sessionDir()
	if want := filepath.Join(home, ".config", "dzzzr"); err != nil || dir != want {
		t.Fatalf("относительный XDG_CONFIG_HOME не проигнорирован: %q (%v)", dir, err)
	}
}

// Windows has no ~/.config: the configuration belongs under %AppData%.
func TestSessionDirUsesAppDataOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("проверяется только на Windows")
	}
	isolate(t)
	t.Setenv("DZZZR_CONFIG_DIR", "")
	appData := t.TempDir()
	t.Setenv("AppData", appData)
	dir, err := sessionDir()
	if want := filepath.Join(appData, "dzzzr"); err != nil || dir != want {
		t.Fatalf("каталог настроек = %q (%v), ожидался %q", dir, err, want)
	}
}

func TestSanitizeCity(t *testing.T) {
	tests := map[string]string{
		"moscow":                  "moscow",
		"":                        "default",
		"  spb  ":                 "spb",
		"http://host:8080/moscow": "http___host_8080_moscow",
		"../../etc/passwd":        ".._.._etc_passwd",
		`a\b`:                     "a_b",
		`a*b?c"d<e>f|g`:           "a_b_c_d_e_f_g",
	}
	for in, want := range tests {
		if got := sanitizeCity(in); got != want {
			t.Errorf("sanitizeCity(%q) = %q, ожидалось %q", in, got, want)
		}
	}
}

func TestSessionFilePermissions(t *testing.T) {
	home := isolate(t)
	cfg := &config{city: "moscow", stdout: io.Discard, stderr: io.Discard}
	c := newClient(cfg)
	c.SetSession("TOKEN")
	c.SetCredentials(dzzzr.Credentials{Captain: "demo", Pin: "1234"})

	path, err := saveSession(cfg, c)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".config", "dzzzr", "moscow.json"); path != want {
		t.Fatalf("сессия записана в %q, ожидалось %q", path, want)
	}
	checkFileMode(t, path, sessionFilePerm)
	checkFileMode(t, filepath.Dir(path), sessionDirPerm)

	// A restored client sees the same credentials.
	restored := newClient(cfg)
	if _, loaded, err := loadSession(cfg, restored); err != nil || !loaded {
		t.Fatalf("loadSession: loaded=%v err=%v", loaded, err)
	}
	if restored.Session() != "TOKEN" || restored.Credentials().Captain != "demo" || restored.Credentials().Pin != "1234" {
		t.Fatalf("восстановлено %q/%+v", restored.Session(), restored.Credentials())
	}
}

func TestFatalJSONMode(t *testing.T) {
	var out, errOut bytes.Buffer
	cfg := &config{jsonOut: true, stdout: &out, stderr: &errOut}
	code := reportError(cfg, fatal("что-то пошло не так: %d", 42))
	if code != 1 {
		t.Fatalf("код выхода = %d, ожидался 1", code)
	}
	if errOut.Len() != 0 {
		t.Errorf("в режиме -json stderr должен быть пуст, получено %q", errOut.String())
	}
	var got errorOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("вывод не является JSON (%q): %v", out.String(), err)
	}
	if got.Error != "что-то пошло не так: 42" {
		t.Fatalf("сообщение = %q", got.Error)
	}
}

func TestFatalTextMode(t *testing.T) {
	var out, errOut bytes.Buffer
	cfg := &config{stdout: &out, stderr: &errOut}
	if code := reportError(cfg, fatal("сбой")); code != 1 {
		t.Fatalf("код выхода = %d", code)
	}
	if out.Len() != 0 {
		t.Errorf("stdout должен быть пуст, получено %q", out.String())
	}
	if !strings.Contains(errOut.String(), "Ошибка: сбой") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestRejectedActionExitsWithTwo(t *testing.T) {
	cfg := &config{stdout: io.Discard, stderr: io.Discard}
	if code := reportError(cfg, errRejected); code != 2 {
		t.Fatalf("код выхода = %d, ожидался 2", code)
	}
	if code := reportError(cfg, nil); code != 0 {
		t.Fatalf("код выхода без ошибки = %d", code)
	}
}

func TestVersionCommandAndFlags(t *testing.T) {
	isolate(t)
	for _, args := range [][]string{{"version"}, {"-v"}, {"--version"}} {
		code, out, errOut := runCLI(t, args...)
		if code != 0 {
			t.Fatalf("%q: код выхода = %d, stderr=%q", args, code, errOut)
		}
		if strings.TrimSpace(out) != "dzzzr "+version {
			t.Fatalf("%q: вывод = %q", args, out)
		}
	}
}

func TestUsageAndUnknownCommand(t *testing.T) {
	isolate(t)

	code, out, _ := runCLI(t, "-h")
	if code != 0 {
		t.Fatalf("код выхода -h = %d", code)
	}
	if !strings.Contains(out, "Команды игрока") || !strings.Contains(out, "send-code") {
		t.Fatalf("справка не содержит списка команд: %q", out)
	}

	code, out, _ = runCLI(t, "send-code", "-h")
	if code != 0 {
		t.Fatalf("код выхода send-code -h = %d", code)
	}
	if !strings.Contains(out, "send-code КОД") {
		t.Fatalf("справка команды = %q", out)
	}

	code, _, errOut := runCLI(t, "nope")
	if code != 2 {
		t.Fatalf("код выхода неизвестной команды = %d", code)
	}
	if !strings.Contains(errOut, "неизвестная команда") {
		t.Fatalf("stderr = %q", errOut)
	}

	if code, _, _ := runCLI(t); code != 2 {
		t.Fatalf("код выхода без аргументов = %d", code)
	}

	if code, _, _ := runCLI(t, "-nosuchflag"); code != 2 {
		t.Fatalf("код выхода при неизвестном флаге = %d", code)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := map[int]string{
		0:     "0с",
		45:    "45с",
		125:   "2м05с",
		3723:  "1ч02м03с",
		-10:   "0с",
		3600:  "1ч00м00с",
		86399: "23ч59м59с",
	}
	for in, want := range tests {
		if got := formatDuration(in); got != want {
			t.Errorf("formatDuration(%d) = %q, ожидалось %q", in, got, want)
		}
	}
}

func TestIndentAndDash(t *testing.T) {
	if got := indent("a\n\nb", "  "); got != "  a\n\n  b" {
		t.Errorf("indent = %q", got)
	}
	if indent("", ">") != "" {
		t.Error("indent пустой строки должен остаться пустым")
	}
	if dash("  ") != "—" || dash("x") != "x" {
		t.Error("dash работает неверно")
	}
}

func TestRequireAuthWithoutSessionExplainsLogin(t *testing.T) {
	isolate(t)
	code, _, errOut := runCLI(t, "-base-url", "http://127.0.0.1:1/moscow/", "status")
	if code != 1 {
		t.Fatalf("код выхода = %d, ожидался 1", code)
	}
	if !strings.Contains(errOut, "dzzzr login") {
		t.Fatalf("подсказка не упоминает вход: %q", errOut)
	}
}

func TestRequireAdminAuthWithoutCredentials(t *testing.T) {
	isolate(t)
	cfg := &config{city: "moscow", stdout: io.Discard, stderr: io.Discard}
	c := newClient(cfg)
	err := requireAdminAuth(cfg, c)
	if err == nil || !strings.Contains(err.Error(), "DZZZR_ADMIN_LOGIN") {
		t.Fatalf("ошибка = %v", err)
	}
	cfg.adminLogin, cfg.adminPassword = "org", "secret"
	if err := requireAdminAuth(cfg, c); err != nil {
		t.Fatalf("с учётными данными организатора: %v", err)
	}
	if !c.HasAdminCredentials() {
		t.Fatal("учётные данные организатора не переданы клиенту")
	}
}

func TestLogoutRemovesSessionFile(t *testing.T) {
	home := isolate(t)
	dir := filepath.Join(home, ".config", "dzzzr")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "moscow.json")
	if err := os.WriteFile(path, []byte(`{"token":"T"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runCLI(t, "logout")
	if code != 0 {
		t.Fatalf("код выхода = %d, stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "Сессия удалена") {
		t.Fatalf("вывод = %q", out)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("файл сессии не удалён: %v", err)
	}

	code, out, _ = runCLI(t, "logout")
	if code != 0 || !strings.Contains(out, "Сохранённой сессии не было") {
		t.Fatalf("повторный logout: код=%d вывод=%q", code, out)
	}
}
