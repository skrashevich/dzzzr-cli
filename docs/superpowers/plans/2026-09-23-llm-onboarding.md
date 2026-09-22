# Онбординг и настройка LLM — план реализации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Мастер первого запуска и панель настроек LLM в веб-интерфейсе dzzzr с паритетом провайдеров encx-cli (Polza.ai, вход ChatGPT, OpenAI-совместимый).

**Architecture:** Порт слоя из encx-cli (`/Users/svk/Documents/dev/en-app-research/cmd/encli`) в `cmd/dzzzr` (package main): JSON-файлы состояния под `sessionDir()`, резолвинг LLM «env → файл → умолчание», веб-API `/api/v1/llm/*` и `/api/v1/onboarding*`, фронтенд отдельными файлами `llm.js`/`onboarding.js`/`onboarding.css`.

**Tech Stack:** Go 1.26 (`encoding/json/v2` в новых файлах dzzzr), picoclaw v0.3.1 (`pkg/auth`, `pkg/providers`, `pkg/fileutil`), vanilla JS, `node --test`.

**Spec:** `docs/superpowers/specs/2026-09-23-llm-onboarding-design.md`

## Global Constraints

- Портирование: исходник encx — источник истины для логики; переносить код целиком, затем применять таблицу адаптации ниже. Удалять всё про GigaChat, local inference, engine settings, `-llm-auth` флаг, `AuthRegistry`, домены en.cx.
- Сообщения для пользователя — на русском, в стиле существующих `fatal(...)`/`webError(...)` dzzzr. Комментарии — английские, как в dzzzr.
- Тесты НИКОГДА не подменяют `$HOME` и не делают `rm -rf` по переменной. Изоляция только через `t.Setenv("DZZZR_CONFIG_DIR", t.TempDir())`, `DZZZR_LLM_SETTINGS_FILE`, `DZZZR_ONBOARDING_FILE`, `DZZZR_CODEX_AUTH_FILE`, `CODEX_HOME`.
- Файлы состояния: 0600, каталоги 0700, атомарная запись (`fileutil.WriteFileAtomic`).
- Коммиты — без AI-трейлеров (`Co-Authored-By` и т.п.).
- Проверка каждой задачи: `go test ./cmd/dzzzr/ -run '<паттерн>' -count=1` и в конце задачи `go test ./... -count=1` + `go vet ./cmd/dzzzr/`.

### Таблица адаптации encx → dzzzr

| encx | dzzzr |
|---|---|
| `sessionDir() string` | `sessionDir() (string, error)` — `stateFilePath` возвращает `(string, error)` |
| `writeJSON(w, code, v)` | `webWriteJSON(w, code, v)` |
| `writeJSON(w, code, map{"error": err.Error()})` | `webError(w, code, "%v", err)` |
| `readJSONBody(w, r, &v)` | `webReadJSON(w, r, &v)` |
| `AgentConfig{AuthMethod, APIKey, BaseURL, Model}` | `agentloop.Config{APIKey, BaseURL, Model, Provider, ...}` + локальный `authMethod string` там, где нужен |
| `resolveAgentConfig(cfg)` | `resolveLLMConfig()` (вызывается из `llmConfig()`) |
| `ENCLI_LLM_SETTINGS_FILE` / `ENCLI_ONBOARDING_FILE` / `ENCLI_CODEX_AUTH_FILE` | `DZZZR_LLM_SETTINGS_FILE` / `DZZZR_ONBOARDING_FILE` / `DZZZR_CODEX_AUTH_FILE` |
| `llmAuthEnvVars = {"LLM_AUTH"}` | `{"DZZZR_LLM_PROVIDER"}` |
| `llmAPIKeyEnvVars` | `{"DZZZR_LLM_API_KEY", "LLM_API_KEY", "OPENROUTER_API_KEY"}` |
| `llmBaseURLEnvVars` | `{"DZZZR_LLM_BASE_URL", "LLM_BASE_URL", "OPENROUTER_BASE_URL"}` |
| `llmModelEnvVars` | `{"DZZZR_LLM_MODEL", "LLM_MODEL", "OPENROUTER_MODEL"}` |
| `"encli/"+version` | `"dzzzr-cli/"+version` |
| `warnIfSessionGlobCollision` | не нужен (в dzzzr нет глоба `sessionDir()/*.json`); удалить вместе с тестами про коллизию |
| текст «encli codex-login» в ошибках | «войдите через ChatGPT в веб-интерфейсе (⚙ Настройки LLM)» |

Значения auth_method в dzzzr: `""`, `apikey`, `openai`, `openrouter` → путь API-ключа; `codex`, `chatgpt` → Codex. Polza.ai хранится как в encx: `auth_method=apikey`, `base_url=https://polza.ai/api/v1`; отдельного значения `polza` нет (уточнение spec по факту кода encx).

## Review Focus

1. Пользователь, у которого уже экспортированы `DZZZR_LLM_PROVIDER=codex` и есть `~/.codex/auth.json` от Codex CLI, после обновления продолжает работать без повторного входа (фолбэк на `CODEX_HOME`).
2. Файл настроек повреждён: агент отвечает понятной ошибкой с путём к файлу, а `GET /api/v1/onboarding` и `GET /api/v1/llm/settings` возвращают 200 с полем `error` — мастер остаётся доступным.
3. `DZZZR_LLM_MODEL` задан в env, пользователь сохраняет модель в UI: env побеждает, ответ API содержит override с `env_var: "DZZZR_LLM_MODEL"`, UI показывает плашку.
4. Неверный пароль организатора в мастере: 401, состояние онбординга не сохранено, прежние организаторские учётные данные восстановлены.
5. Выход игрока (`/api/v1/auth/logout` удаляет файл сессии города) не удаляет `llm/settings.json`, `onboarding/state.json`, `codex/auth.json`.

---

### Task 1: Слой JSON-состояния

**Files:**
- Create: `cmd/dzzzr/state_file.go`
- Test: `cmd/dzzzr/state_file_test.go`

**Interfaces:**
- Produces:
  - `func stateFilePath(envVar, subdir, name string) (string, error)`
  - `func loadJSONState[T any](path, what string) (T, error)`
  - `func saveJSONState[T any](path, what string, v T) error`
  - `func deleteJSONState(path string) error`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testState struct {
	Name string `json:"name"`
}

func TestStateFilePathDefaultsUnderConfigDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DZZZR_CONFIG_DIR", dir)
	t.Setenv("DZZZR_TEST_STATE_FILE", "")
	got, err := stateFilePath("DZZZR_TEST_STATE_FILE", "sub", "x.json")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "sub", "x.json"); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	t.Setenv("DZZZR_TEST_STATE_FILE", "/elsewhere/y.json")
	if got, _ := stateFilePath("DZZZR_TEST_STATE_FILE", "sub", "x.json"); got != "/elsewhere/y.json" {
		t.Fatalf("override ignored: %q", got)
	}
}

func TestJSONStateRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "s.json")
	if v, err := loadJSONState[testState](path, "test state"); err != nil || v.Name != "" {
		t.Fatalf("missing file: %+v %v", v, err)
	}
	if err := saveJSONState(path, "test state", testState{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
	v, err := loadJSONState[testState](path, "test state")
	if err != nil || v.Name != "x" {
		t.Fatalf("round trip: %+v %v", v, err)
	}
	if err := deleteJSONState(path); err != nil {
		t.Fatal(err)
	}
	if err := deleteJSONState(path); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}

func TestJSONStateBrokenFileNamesThePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(path, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadJSONState[testState](path, "test state")
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("err = %v", err)
	}
	if err := os.WriteFile(path, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadJSONState[testState](path, "test state"); err != nil {
		t.Fatalf("blank file: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./cmd/dzzzr/ -run 'StateFile|JSONState' -count=1` → FAIL: `undefined: stateFilePath`.

- [ ] **Step 3: Implement** — скопировать `en-app-research/cmd/encli/state_file.go`, применить таблицу адаптации: `stateFilePath` возвращает `(string, error)` (ошибка `sessionDir()` пробрасывается), из `saveJSONState` убрать параметр `envVar` и вызов `warnIfSessionGlobCollision`; ошибки — по-русски: `fmt.Errorf("не удалось прочитать %s %s: %w", what, path, err)`, `fmt.Errorf("не удалось разобрать %s %s: %w (удалите файл, чтобы начать заново)", what, path, err)`, `fmt.Errorf("не удалось записать %s %s: %w", ...)`. `what` передаётся по-русски («настройки LLM», «состояние мастера»). Использовать `encoding/json/v2` (`json.Marshal(v, jsontext.Multiline(true))` не требуется — достаточно `json.Marshal`).

- [ ] **Step 4: Run to verify pass** — та же команда → PASS.

- [ ] **Step 5: Commit** — `git add cmd/dzzzr/state_file*.go && git commit -m "feat: add JSON state file layer"`

---

### Task 2: Сохранённые настройки LLM и новый резолвинг

**Files:**
- Create: `cmd/dzzzr/llm_settings.go`, `cmd/dzzzr/llm_settings_test.go`
- Modify: `cmd/dzzzr/agentsetup.go:120-172` (`resolveLLMConfig`)

**Interfaces:**
- Consumes: Task 1.
- Produces:
  - `type llmSettings struct{ AuthMethod, BaseURL, APIKey, Model string }` (json `auth_method`, `base_url`, `api_key`, `model`, все `omitempty`), `(s llmSettings) trimmed() llmSettings`
  - `func llmSettingsFile() (string, error)`, `loadLLMSettings() (llmSettings, error)`, `saveLLMSettings(llmSettings) error`, `deleteLLMSettings() error`
  - `var llmAuthEnvVars, llmAPIKeyEnvVars, llmBaseURLEnvVars, llmModelEnvVars []string`
  - `type llmFieldSource struct{ Value, Source, EnvVar string }`; константы `llmSourceEnv="env"`, `llmSourceSettings="settings"`, `llmSourceDefault="default"`, `llmSourceUnset="unset"`
  - `func resolveLLMField(envNames []string, stored, def string) llmFieldSource` (без параметра флага — в dzzzr флага нет)
  - `func firstEnv(names []string) (value, from string)`, `validateLLMBaseURL(string) error`, `maskAPIKey(string) string`
  - `const authMethodAPIKey = "apikey"`, `authMethodCodex = "codex"`, `func normalizeAuthMethod(string) (string, error)` — `""` → `""`, `apikey|openai|openrouter` → `apikey`, `codex|chatgpt` → `codex`, иное → ошибка `неизвестный способ подключения %q: используйте openai, openrouter или codex`.

- [ ] **Step 1: Port tests** — перенести из `encx/llm_settings_test.go`: `RoundTrip`, `FilePermissions`, `MissingFileIsEmpty`, `EmptyFileIsEmpty`, `RejectsBrokenJSON`, `ValidateBaseURL`, `MaskAPIKey`, `FirstEnvNamesTheWinner`, `ResolveLLMFieldPrecedence` (без флаговых кейсов), и все `TestResolveAgentConfig*` кроме Flag/Codex (они в Task 3) — переименовать в `TestResolveLLMConfig*`, вызывать `resolveLLMConfig()`. Каждый тест начинается с `isolateLLMEnv(t)`. Добавить dzzzr-специфичные тесты:

```go
// isolateLLMEnv clears every variable the resolver reads and points all state
// at a fresh directory, so a developer's own configuration never leaks in.
func isolateLLMEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DZZZR_CONFIG_DIR", dir)
	for _, name := range []string{
		"DZZZR_LLM_PROVIDER", "DZZZR_LLM_API_KEY", "LLM_API_KEY", "OPENROUTER_API_KEY",
		"DZZZR_LLM_BASE_URL", "LLM_BASE_URL", "OPENROUTER_BASE_URL",
		"DZZZR_LLM_MODEL", "LLM_MODEL", "OPENROUTER_MODEL",
		"DZZZR_LLM_SETTINGS_FILE", "DZZZR_CODEX_AUTH_FILE",
	} {
		t.Setenv(name, "")
	}
	t.Setenv("CODEX_HOME", filepath.Join(dir, "no-codex"))
	return dir
}

func TestResolveLLMConfigDZZZREnvOutranksGenericAndStored(t *testing.T) {
	isolateLLMEnv(t)
	if err := saveLLMSettings(llmSettings{APIKey: "stored", Model: "stored/model"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LLM_MODEL", "generic/model")
	t.Setenv("DZZZR_LLM_MODEL", "dzzzr/model")
	cfg, err := resolveLLMConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "dzzzr/model" || cfg.APIKey != "stored" || cfg.BaseURL != defaultLLMBaseURL {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestResolveLLMConfigStoredProviderOpenRouterIsAPIKeyPath(t *testing.T) {
	isolateLLMEnv(t)
	if err := saveLLMSettings(llmSettings{AuthMethod: "openrouter", APIKey: "k"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := resolveLLMConfig()
	if err != nil || cfg.APIKey != "k" || cfg.Model != defaultLLMModel {
		t.Fatalf("cfg = %+v, err = %v", cfg, err)
	}
}

func TestResolveLLMConfigBrokenSettingsFileIsAnError(t *testing.T) {
	dir := isolateLLMEnv(t)
	path := filepath.Join(dir, "llm", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DZZZR_LLM_API_KEY", "k")
	if _, err := resolveLLMConfig(); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("err = %v", err)
	}
}

func TestLLMSettingsSurviveAPlayerLogoutDirectory(t *testing.T) {
	dir := isolateLLMEnv(t)
	if err := saveLLMSettings(llmSettings{APIKey: "k"}); err != nil {
		t.Fatal(err)
	}
	path, _ := llmSettingsFile()
	if filepath.Dir(filepath.Dir(path)) != dir || filepath.Base(filepath.Dir(path)) != "llm" {
		t.Fatalf("settings must live in <config>/llm/, got %s", path)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./cmd/dzzzr/ -run 'LLMSettings|ResolveLLM' -count=1` → FAIL: undefined.

- [ ] **Step 3: Implement** — скопировать `encx/llm_settings.go`, адаптировать по таблице; `resolveLLMField(envNames, stored, def)` без ветки флага; убрать `llmSourceFlag` и `llmSourceIsAmbient` заменить на `source == llmSourceEnv`. В `agentsetup.go` переписать `resolveLLMConfig()`:

```go
func resolveLLMConfig() (agentloop.Config, error) {
	stored, err := loadLLMSettings()
	if err != nil {
		return agentloop.Config{}, err
	}
	method, err := normalizeAuthMethod(resolveLLMField(llmAuthEnvVars, stored.AuthMethod, "").Value)
	if err != nil {
		return agentloop.Config{}, fatal("%v", err)
	}
	apiKey := resolveLLMField(llmAPIKeyEnvVars, stored.APIKey, "").Value
	endpoint := resolveLLMField(llmBaseURLEnvVars, stored.BaseURL, "").Value
	model := resolveLLMField(llmModelEnvVars, stored.Model, "").Value

	// Without an explicit choice a stored ChatGPT sign-in is used only when
	// nothing else was configured: a key or an endpoint is a statement of intent.
	if method == "" && apiKey == "" && endpoint == "" && hasCodexCredential() {
		method = authMethodCodex
	}
	if method == authMethodCodex {
		out, err := codexLLMConfig(model)
		if err == nil && agentConfigHook != nil {
			agentConfigHook(&out)
		}
		return out, err
	}
	baseURL := cmp.Or(endpoint, defaultLLMBaseURL)
	if apiKey == "" && !isLocalEndpoint(baseURL) {
		return agentloop.Config{}, fatal(
			"не задан ключ модели: настройте модель в веб-интерфейсе (⚙ Настройки LLM) или укажите DZZZR_LLM_API_KEY (или LLM_API_KEY, OPENROUTER_API_KEY)")
	}
	out := agentloop.Config{
		APIKey:    apiKey,
		Model:     cmp.Or(model, defaultLLMModel),
		BaseURL:   baseURL,
		UserAgent: "dzzzr-cli/" + version,
	}
	if agentConfigHook != nil {
		agentConfigHook(&out)
	}
	return out, nil
}
```

До Task 3 временно: `func hasCodexCredential() bool { return false }` в `agentcodex.go`, а `codexLLMConfig(model string)` принимает модель (внутри `model := cmp.Or(model, p.GetDefaultModel())` вместо чтения env). Сообщение об ошибке для неизвестного провайдера должно по-прежнему содержать `DZZZR_LLM_PROVIDER` (существующие тесты).

- [ ] **Step 4: Run** — `go test ./cmd/dzzzr/ -count=1` → PASS (включая существующие `agentcodex_test.go`, `agentcontext_test.go`, `agentauth_test.go`; если какой-то тест проверял старый текст «не задан ключ модели: укажите …» — обновить ожидание на подстроку `DZZZR_LLM_API_KEY`).

- [ ] **Step 5: Commit** — `git commit -m "feat: persist LLM settings and resolve them after the environment"`

---

### Task 3: Собственный вход ChatGPT (Codex)

**Files:**
- Create: `cmd/dzzzr/llm_auth_codex.go`, `cmd/dzzzr/llm_auth_codex_test.go`
- Modify: `cmd/dzzzr/agentcodex.go` (источник учётных данных), `cmd/dzzzr/agentcodex_test.go`

**Interfaces:**
- Consumes: Task 1, Task 2 (`authMethodCodex`, `codexLLMConfig(model string)`).
- Produces:
  - `type codexCredential` (как в encx), `codexAuthFile() (string, error)` → `DZZZR_CODEX_AUTH_FILE` или `<sessionDir>/codex/auth.json`
  - `loadCodexCredential() (*codexCredential, error)`, `saveCodexCredential(*codexCredential) error`, `deleteCodexCredential() error`, `hasCodexCredential() bool`
  - `codexCredentialFromAuth(*auth.AuthCredential) *codexCredential`
  - `sharedCodexTokenStore(path string, cred *codexCredential) *cliCodexTokenStore`, `(*cliCodexTokenStore).tokenSource() func() (string, string, error)`, `resetCodexTokenStores()`
  - `const defaultCodexModel = "gpt-5.5"`, `func codexModel(configured string) string`
  - `func codexCLIAuthFile() (string, error)` — `$CODEX_HOME/auth.json` или `~/.codex/auth.json` (текущее поведение dzzzr)

- [ ] **Step 1: Port tests** — из `encx/llm_auth_codex_test.go` перенести: `CodexCredentialRoundTrip`, `SaveCodexCredentialTightensAnExistingFile`, `CodexProvidersShareOneTokenStore`, `ConcurrentTurnsRefreshTheTokenOnce`, `LoadCodexCredentialReportsMissingSignIn`, `LoadCodexCredentialRejectsEmptyAccessToken`, все `CodexTokenStore*`, `StalledRefresh…`, `ExpiredCredentialRetries…`, `LateRefresh…`, `FailedRefresh…`, `CodexModel*`, и `ResolveAgentConfigCodex*`/`PrefersAStoredCredential`/`KeepsAnExplicitLocalEndpoint`/`KeepsTheAPIKeyPath`/`APIKeyOptOutIgnoresTheCredential` → переименовать в `TestResolveLLMConfig…`, вызывать `resolveLLMConfig()`, env `LLM_AUTH` → `DZZZR_LLM_PROVIDER`. Не переносить: `CollisionIsDetected`, `WarnsAboutAColliding…`, `NotMistakenForADomainSession`, `AuthFlags…`, `SplitLLMArgs…`, `Pricing…`, `NewPicoProvider…`. Добавить:

```go
func TestCodexFallsBackToTheCodexCLIFile(t *testing.T) {
	dir := isolateLLMEnv(t)
	codexHome := filepath.Join(dir, "codex-home")
	t.Setenv("CODEX_HOME", codexHome)
	writeCodexCLIAuth(t, filepath.Join(codexHome, "auth.json")) // existing helper in agentcodex_test.go; create it there if absent, writing {"tokens":{"access_token":<jwt>,"account_id":"acc"}}
	t.Setenv("DZZZR_LLM_PROVIDER", "codex")
	cfg, err := resolveLLMConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider == nil || cfg.BaseURL != "https://chatgpt.com/backend-api/codex" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestOwnCodexSignInWinsOverTheCodexCLIFile(t *testing.T) {
	dir := isolateLLMEnv(t)
	t.Setenv("CODEX_HOME", filepath.Join(dir, "codex-home"))
	writeCodexCLIAuth(t, filepath.Join(dir, "codex-home", "auth.json"))
	if err := saveCodexCredential(&codexCredential{AccessToken: "own", AccountID: "own-acc", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	src, err := codexTokenSource()
	if err != nil {
		t.Fatal(err)
	}
	token, account, err := src()
	if err != nil || token != "own" || account != "own-acc" {
		t.Fatalf("token=%q account=%q err=%v", token, account, err)
	}
}

func TestCodexWithoutAnySignInExplainsWhereToLogIn(t *testing.T) {
	isolateLLMEnv(t)
	t.Setenv("DZZZR_LLM_PROVIDER", "codex")
	_, err := resolveLLMConfig()
	if err == nil || !strings.Contains(err.Error(), "ChatGPT") {
		t.Fatalf("err = %v", err)
	}
}
```

(Имена полей `codexCredential` — взять из encx `llm_auth_codex.go:67`; если они отличаются от `AccessToken/AccountID/ExpiresAt`, поправить тест под них.)

- [ ] **Step 2: Run** — `go test ./cmd/dzzzr/ -run 'Codex' -count=1` → FAIL.

- [ ] **Step 3: Implement** — скопировать `encx/llm_auth_codex.go` без `registerLLMAuthFlag`, `cmdCodexLogin/Logout/Status`, `runCodexLogin`, `credentialPathCollides`, `warnIf*`, `newCodexProvider`. `errNoCodexCredential = errors.New("вход в ChatGPT не выполнен: войдите в веб-интерфейсе (⚙ Настройки LLM → Подписка ChatGPT) или через Codex CLI")`. В `agentcodex.go`:

```go
// codexTokenSource picks the credential: an explicit file, then dzzzr's own
// sign-in, then the one Codex CLI keeps — the path every earlier release used.
func codexTokenSource() (func() (string, string, error), error) {
	if path := os.Getenv("DZZZR_CODEX_AUTH_FILE"); path != "" {
		return func() (string, string, error) { return readCodexAuth(path) }, nil
	}
	if cred, err := loadCodexCredential(); err == nil {
		path, err := codexAuthFile()
		if err != nil {
			return nil, err
		}
		return sharedCodexTokenStore(path, cred).tokenSource(), nil
	} else if !errors.Is(err, errNoCodexCredential) {
		return nil, err
	}
	path, err := codexCLIAuthFile()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		return nil, errNoCodexCredential
	}
	return func() (string, string, error) { return readCodexAuth(path) }, nil
}
```

`DZZZR_CODEX_AUTH_FILE` сохраняет старый смысл — файл формата Codex CLI (`readCodexAuth`); собственный вход dzzzr хранится только в `<sessionDir>/codex/auth.json` (перенос для тестов собственного входа — через `DZZZR_CONFIG_DIR`). Поэтому `codexAuthFile()` = `stateFilePath("", "codex", "auth.json")`. `codexLLMConfig(model)` использует `codexTokenSource()` и `codexModel(model)` вместо текущей проверки префиксов — но существующая ошибка «несовместима с Codex» для модели с `/` сохраняется, если `agentcodex_test.go` её проверяет (иначе поведение encx: `codexModel` откатывается на `defaultCodexModel`; выбрать encx-поведение и обновить старый тест). `hasCodexCredential()` = есть **только собственный** вход dzzzr (`loadCodexCredential()` без ошибки): наличие файла Codex CLI не включает автовыбор Codex, его использование требует явного `DZZZR_LLM_PROVIDER=codex` — ровно как сейчас.

- [ ] **Step 4: Run** — `go test ./cmd/dzzzr/ -count=1` → PASS; `go test -race ./cmd/dzzzr/ -run 'CodexTokenStore|ConcurrentTurns' -count=1` → PASS.

- [ ] **Step 5: Commit** — `git commit -m "feat: keep dzzzr's own ChatGPT sign-in with Codex CLI fallback"`

---

### Task 4: Веб-API настроек LLM и входа ChatGPT

**Files:**
- Create: `cmd/dzzzr/web_llm.go`, `cmd/dzzzr/web_llm_codex.go`, `cmd/dzzzr/web_llm_test.go`, `cmd/dzzzr/web_llm_codex_test.go`
- Modify: `cmd/dzzzr/web.go` (поля `webHub`, маршруты рядом с `GET /api/v1/agent/config`, закрытие менеджеров при остановке сервера)

**Interfaces:**
- Consumes: Tasks 1–3.
- Produces:
  - маршруты `/api/v1/llm/settings` (GET/PUT/DELETE), `GET /api/v1/llm/codex/status`, `POST /api/v1/llm/codex/logout`, `POST /api/v1/llm/codex/login`, `GET|DELETE /api/v1/llm/codex/login/{id}`, `POST /api/v1/llm/codex/login/{id}/code`
  - `type llmSettingsPayload` (JSON-форма как в encx `web_llm.go:105`, без gigachat-полей)
  - `func (h *webHub) llmSettingsPayload() (llmSettingsPayload, error)`
  - поля `webHub`: `codexMu sync.Mutex; codex *codexLoginManager` (как в encx `codexFlows()`)

- [ ] **Step 1: Port tests** — все тесты из `encx/web_llm_test.go` (кроме `ReportsTheFlagOverride`) и `encx/web_llm_codex_test.go`. Хелпер создания хаба заменить на существующий в `web_test.go` dzzzr (найти `newTestHub`/аналог: `grep -n "func newTest.*webHub\|&webHub{" cmd/dzzzr/*_test.go`), в начале каждого теста `isolateLLMEnv(t)`. Env-имена по таблице. Добавить:

```go
func TestWebLLMSettingsNamesTheDZZZRVariableThatWins(t *testing.T) {
	isolateLLMEnv(t)
	t.Setenv("DZZZR_LLM_MODEL", "env/model")
	h := newLLMTestHub(t)
	rec := doJSON(t, h, http.MethodPut, "/api/v1/llm/settings", `{"auth_method":"apikey","api_key":"k","model":"ui/model"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"env_var":"DZZZR_LLM_MODEL"`) || strings.Contains(body, `"k"`) {
		t.Fatalf("body = %s", body)
	}
	stored, _ := loadLLMSettings()
	if stored.Model != "ui/model" {
		t.Fatalf("stored = %+v", stored)
	}
}
```

(`newLLMTestHub`/`doJSON` — тонкие хелперы в `web_llm_test.go` поверх существующего тестового хаба dzzzr и `httptest.NewRecorder` + `h.routes().ServeHTTP` / фактическое имя метода сборки mux из `web.go:130-169`.)

- [ ] **Step 2: Run** — `go test ./cmd/dzzzr/ -run 'WebLLM|WebCodex' -count=1` → FAIL.

- [ ] **Step 3: Implement** — скопировать `encx/web_llm.go` и `encx/web_llm_codex.go`, адаптировать по таблице; `llmSettableAuthMethods = []string{authMethodAPIKey, authMethodCodex}`; `llmAgentSummary` строится из `llmConfig()`; `codexStatusForWeb()` дополнительно сообщает `source: "dzzzr"|"codex-cli"|""`, чтобы UI мог написать «используется вход Codex CLI». Тексты ошибок — по-русски. Зарегистрировать маршруты в `web.go` сразу после `GET /api/v1/agent/config`. Закрытие менеджера — туда же, где сервер завершается в `runWeb`.

- [ ] **Step 4: Run** — `go test ./cmd/dzzzr/ -count=1` и `go test -race ./cmd/dzzzr/ -run 'WebCodex' -count=1` → PASS.

- [ ] **Step 5: Commit** — `git commit -m "feat: web API for LLM settings and ChatGPT sign-in"`

---

### Task 5: Polza.ai

**Files:**
- Create: `cmd/dzzzr/web_llm_polza.go`, `cmd/dzzzr/web_llm_polza_test.go`
- Modify: `agentloop/agent.go` (`Config.ExtraBody map[string]any`, передать в `NewHTTPProviderWithMaxTokensFieldAndRequestTimeout` вместо первого `nil`), `cmd/dzzzr/agentsetup.go` (ExtraBody для Polza), `cmd/dzzzr/web.go` (маршруты, поле `polza *polzaManager`)

**Interfaces:**
- Consumes: Tasks 1–4.
- Produces: `const polzaBaseURL = "https://polza.ai/api/v1"`, `func polzaEndpoint(string) bool`, маршруты `/api/v1/llm/polza/*` (7 шт., как в encx `web.go:99-105`).

- [ ] **Step 1: Port tests** — все 8 тестов из `encx/web_llm_polza_test.go`, плюс:

```go
func TestResolveLLMConfigSendsPolzaProviderPreferences(t *testing.T) {
	isolateLLMEnv(t)
	if err := saveLLMSettings(llmSettings{AuthMethod: "apikey", BaseURL: polzaBaseURL, APIKey: "k", Model: "openai/gpt-4.1"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := resolveLLMConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ExtraBody == nil || cfg.ExtraBody["provider"] == nil {
		t.Fatalf("ExtraBody = %#v", cfg.ExtraBody)
	}
}
```

И в `agentloop` — тест, что `ExtraBody` доходит до тела запроса (httptest-сервер, проверка JSON-поля `provider`), по образцу существующих тестов `agentloop/*_test.go`.

- [ ] **Step 2: Run** — `go test ./agentloop/ ./cmd/dzzzr/ -run 'Polza|ExtraBody' -count=1` → FAIL.

- [ ] **Step 3: Implement** — скопировать `encx/web_llm_polza.go`, адаптировать по таблице (`polzaSetupAllowed` — проверка, что auth_method/base_url/api_key не перекрыты env; тексты по-русски). В `resolveLLMConfig` после сборки `out` на пути API-ключа:

```go
if polzaEndpoint(out.BaseURL) {
	out.ExtraBody = map[string]any{
		"provider": map[string]any{"sort": "throughput", "ignore": []string{"Relace", "relace/fp4"}},
	}
}
```

- [ ] **Step 4: Run** — `go test ./... -count=1` → PASS.

- [ ] **Step 5: Commit** — `git commit -m "feat: Polza.ai sign-in, models and balance"`

---

### Task 6: Онбординг (backend)

**Files:**
- Create: `cmd/dzzzr/onboarding.go`, `cmd/dzzzr/web_onboarding.go`, `cmd/dzzzr/onboarding_test.go`
- Modify: `cmd/dzzzr/web_auth.go` (вынести вход игрока в `func (h *webHub) loginPlayer(ctx context.Context, login, password string) (int, error)`), `cmd/dzzzr/web_admin.go` (вынести вход организатора в `func (h *webHub) loginOrganizer(ctx context.Context, login, password string) (int, error)`), `cmd/dzzzr/web.go` (3 маршрута)

**Interfaces:**
- Consumes: Task 1, `llmConfig()`.
- Produces:
  - `GET /api/v1/onboarding` → `{"required","completed","completed_at","skipped","steps":[{"id":"llm","title","done","detail"},{"id":"auth",...}],"error"}`
  - `POST /api/v1/onboarding/complete` body `{"role":"player"|"organizer","login","password"}`
  - `POST /api/v1/onboarding/reset`
  - `loginPlayer`/`loginOrganizer` возвращают HTTP-код и ошибку (0, nil при успехе); `httpAuthLogin`/`httpAdminLogin` становятся обёртками без изменения поведения.

- [ ] **Step 1: Write tests** — перенести 7 тестов из `encx/onboarding_test.go` (сервер движка — `dzzzr-mock`, как в существующих `web_admin_test.go`/`web_test.go`: найти хелпер `grep -n "mock" cmd/dzzzr/web_admin_test.go | head`). Добавить:

```go
func TestWebOnboardingCompleteAsOrganizer(t *testing.T) {
	isolateLLMEnv(t)
	h := newOnboardingTestHub(t) // hub wired to dzzzr-mock with known organizer creds
	rec := doJSON(t, h, http.MethodPost, "/api/v1/onboarding/complete",
		fmt.Sprintf(`{"role":"organizer","login":%q,"password":%q}`, mockAdminLogin, mockAdminPassword))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	st, err := loadOnboardingState()
	if err != nil || !st.Completed {
		t.Fatalf("state = %+v, err = %v", st, err)
	}
}

func TestWebOnboardingWrongOrganizerPasswordKeepsThePrevious(t *testing.T) {
	isolateLLMEnv(t)
	h := newOnboardingTestHub(t)
	h.cfg.adminLogin, h.cfg.adminPassword = "prev", "prevpw"
	h.client.SetAdminCredentials("prev", "prevpw")
	rec := doJSON(t, h, http.MethodPost, "/api/v1/onboarding/complete",
		`{"role":"organizer","login":"x","password":"wrong"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if h.cfg.adminLogin != "prev" {
		t.Fatalf("previous organizer lost: %q", h.cfg.adminLogin)
	}
	if st, _ := loadOnboardingState(); st.Completed {
		t.Fatal("wizard must not complete on a refused login")
	}
}

func TestWebOnboardingRejectsUnknownRole(t *testing.T) {
	isolateLLMEnv(t)
	h := newOnboardingTestHub(t)
	rec := doJSON(t, h, http.MethodPost, "/api/v1/onboarding/complete", `{"role":"judge","login":"a","password":"b"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestWebOnboardingSurvivesPlayerLogout(t *testing.T) {
	isolateLLMEnv(t)
	h := newOnboardingTestHub(t)
	if err := saveOnboardingState(onboardingState{Completed: true}); err != nil {
		t.Fatal(err)
	}
	if err := saveLLMSettings(llmSettings{APIKey: "k"}); err != nil {
		t.Fatal(err)
	}
	if rec := doJSON(t, h, http.MethodPost, "/api/v1/auth/logout", `{}`); rec.Code != http.StatusOK {
		t.Fatalf("logout %d", rec.Code)
	}
	st, _ := loadOnboardingState()
	s, _ := loadLLMSettings()
	if !st.Completed || s.APIKey != "k" {
		t.Fatalf("logout wiped state: %+v %+v", st, s)
	}
}

func TestWebOnboardingBrokenStateStillAnswers(t *testing.T) {
	dir := isolateLLMEnv(t)
	path := filepath.Join(dir, "onboarding", "state.json")
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, []byte("{"), 0o600)
	h := newOnboardingTestHub(t)
	rec := doJSON(t, h, http.MethodGet, "/api/v1/onboarding", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"required":true`) || !strings.Contains(rec.Body.String(), `"error"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}
```

(`mockAdminLogin/mockAdminPassword` — реальные значения из `cmd/dzzzr-mock/admin.go`; подставить их или существующие константы тестов.)

- [ ] **Step 2: Run** — `go test ./cmd/dzzzr/ -run 'Onboarding' -count=1` → FAIL.

- [ ] **Step 3: Implement** — `onboarding.go` = копия encx с `DZZZR_ONBOARDING_FILE`, `what` = «состояние мастера настройки». `web_onboarding.go` по образцу encx: шаг `llm` — `llmConfig()` без ошибки, `detail` = `"<codex|apikey>, модель <model>"`; шаг `auth` — done, если `h.status().HasSession || h.client.HasAdminCredentials()` (под `clientMu`), detail «игрок: <login>» / «организатор: <login>». `complete`: разобрать тело, `role` пустой или неизвестный → 400 «укажите роль: player или organizer»; вызвать `loginPlayer`/`loginOrganizer`; при ошибке `webError(w, code, "%v", err)`; иначе сохранить `onboardingState{Completed: true, CompletedAt: time.Now().UTC().Format(time.RFC3339)}` и вернуть payload. Рефакторинг `httpAuthLogin`/`httpAdminLogin`: тело до `webWriteJSON` переносится в `loginPlayer`/`loginOrganizer` дословно (с той же дисциплиной блокировок `clientMu`), обработчики вызывают их и пишут прежний ответ.

- [ ] **Step 4: Run** — `go test ./cmd/dzzzr/ -count=1` → PASS (включая существующие web_auth/web_admin тесты).

- [ ] **Step 5: Commit** — `git commit -m "feat: onboarding status, completion and reset API"`

---

### Task 7: UI — панель настроек LLM

**Files:**
- Create: `cmd/dzzzr/webui/llm.js`, `cmd/dzzzr/webui/onboarding.css`, `cmd/dzzzr/web_llm_ui_test.go`
- Modify: `cmd/dzzzr/webui/index.html` (кнопка ⚙ в `.sidebar-brand` после `#brand-model`; модалка `#llm-modal` перед `<script>`; `<link rel="stylesheet" href="onboarding.css">`; `<script src="llm.js" defer>` перед `app.js`), `cmd/dzzzr/webui/app.js` (`boot()` вызывает `bindLLMSettings()`; после сохранения — `loadAgentConfig()`)

**Interfaces:**
- Consumes: API Tasks 4–5; из `app.js`: `API`, `state`, `$`, `api(path, opts)`, `toast(msg, err)`, `loadAgentConfig()`.
- Produces (глобальные функции `llm.js`, используются Task 8): `bindLLMSettings()`, `openLLMModal()`, `closeLLMModal()`, `renderOverridesInto(slotEl, overrides)`, `selectLLMTab(prefix, tab)`, контроллеры Polza/Codex, параметризованные префиксом id (`'llm'` | `'onboarding-llm'`) — `bindPolza(prefix)`, `startCodexLogin(prefix)`, `startCodexPoll(prefix, id)`, `submitCodexCode(prefix)`, `cancelCodexLogin(prefix)`, `isModalOpen()`, `trapModalFocus(dialog, ev)`.

- [ ] **Step 1: Write failing test** — `web_llm_ui_test.go` по образцу `encx/web_onboarding_ui_test.go`: список id панели (взять все `id="llm-*"`, `btn-llm-*` из encx `index.html:57,120-230`), проверка, что каждый id есть в `webui/index.html` (через `webUIFiles.ReadFile`) и упоминается в `webui/llm.js`; проверка, что `index.html` подключает `llm.js` раньше `app.js`.

- [ ] **Step 2: Run** — `go test ./cmd/dzzzr/ -run 'LLMUI' -count=1` → FAIL.

- [ ] **Step 3: Implement** — перенести из encx `webui/index.html:120-230` разметку модалки, из `webui/app.js:1480-2338` функции (`isPolzaURL` … `bindLLMSettings`) в `llm.js`, из `webui/style.css:1473-1878` стили в `onboarding.css`. Адаптация: `api()` dzzzr принимает путь относительно `API` — проверить сигнатуру `app.js:196` и привести вызовы; удалить gigachat/local ветки; цвета — через CSS-переменные dzzzr из `style.css` (`grep -n "^\s*--" cmd/dzzzr/webui/style.css`), акцент `#e0343c`; тексты про encli → dzzzr. Контроллеры, которые в encx продублированы для `onboarding-*`, свести к одной реализации с параметром-префиксом.

- [ ] **Step 4: Run** — тест → PASS; `go build ./cmd/dzzzr && node --check cmd/dzzzr/webui/llm.js`.

- [ ] **Step 5: Commit** — `git commit -m "feat: LLM settings panel in the web UI"`

---

### Task 8: UI — мастер первого запуска

**Files:**
- Create: `cmd/dzzzr/webui/onboarding.js`, `cmd/dzzzr/web_onboarding_ui_test.go`, `cmd/dzzzr/webui_tests/onboarding.test.cjs`
- Modify: `cmd/dzzzr/webui/index.html` (кнопка ◈ `#btn-onboarding-open` рядом с ⚙; разметка `#onboarding` по encx `index.html:231-400`, шаг auth — см. ниже; `<script src="onboarding.js" defer>` после `llm.js`), `cmd/dzzzr/webui/onboarding.css` (стили мастера из encx `style.css:1879-2131`), `cmd/dzzzr/webui/app.js` (`boot()` в конце: `await initOnboarding()`), `.github/workflows/test.yml` (шаг `node --test cmd/dzzzr/webui_tests/`)

**Interfaces:**
- Consumes: Task 6 API, Task 7 глобальные функции, `state.mode`/режим редактора (определить по `document.body` классу или `state` — `grep -n "mode" cmd/dzzzr/webui/app.js | head`).
- Produces: `initOnboarding()`, `openOnboarding()`, `closeOnboarding()`, `onboardingNext()`, `onboardingBack()`, `onOnboardingAuthSubmit(ev)`, `state.onboarding = {step, status, llmAuth, role, busy}`.

Шаг «Вход» (заменяет домен encx):

```html
<section class="onboarding-pane" id="onboarding-pane-auth" tabindex="-1" hidden>
  <form id="onboarding-auth-form" class="onboarding-auth-form" novalidate>
    <div class="llm-tabs" role="radiogroup" aria-label="Роль">
      <label class="llm-tab"><input type="radio" name="role" value="player" id="onboarding-auth-role-player"> Игрок</label>
      <label class="llm-tab"><input type="radio" name="role" value="organizer" id="onboarding-auth-role-organizer"> Организатор</label>
    </div>
    <p class="llm-hint">Город: <output id="onboarding-auth-city">—</output></p>
    <label class="llm-field-label" for="onboarding-auth-login">Логин</label>
    <input id="onboarding-auth-login" name="login" class="llm-input" required autocomplete="username">
    <label class="llm-field-label" for="onboarding-auth-password">Пароль</label>
    <input id="onboarding-auth-password" name="password" type="password" class="llm-input" required autocomplete="current-password">
    <p id="onboarding-auth-status" class="onboarding-result" role="status" aria-live="polite"></p>
  </form>
</section>
```

- [ ] **Step 1: Write failing tests** — `web_onboarding_ui_test.go`: список id из encx `web_onboarding_ui_test.go` с заменой `onboarding-auth-domain` → `onboarding-auth-city`, `onboarding-auth-role-player`, `onboarding-auth-role-organizer`, плюс все `onboarding-llm-tab-polza`/`onboarding-llm-polza-*`; каждый id есть в `index.html` и упоминается в `onboarding.js` или `llm.js`. `onboarding.test.cjs` — порт encx `webui_tests/onboarding.test.cjs`: загружать в `vm` последовательно `app.js` (без `boot();`), `llm.js`, `onboarding.js`; `fields = {role: 'organizer', login: 'org', password: 'secret'}`. Добавить тесты:

```js
test('organizer role posts role to complete', async () => {
  const run = setup();
  await run('onOnboardingAuthSubmit({preventDefault() {}})');
  const body = JSON.parse(run('requests[0].options.body'));
  assert.equal(run('requests[0].url'), '/onboarding/complete'); // или '/api/v1/onboarding/complete' — по сигнатуре api() dzzzr
  assert.deepEqual(body, {role: 'organizer', login: 'org', password: 'secret'});
  assert.equal(run('closed'), true);
});

test('editor mode preselects organizer', () => {
  const run = setup();
  assert.equal(run('defaultOnboardingRole(true)'), 'organizer');
  assert.equal(run('defaultOnboardingRole(false)'), 'player');
});

test('llm step next is blocked until llm step is done', async () => {
  const run = setup();
  run(`state.onboarding.step = 'llm'; state.onboarding.status = {steps: [{id: 'llm', done: false}, {id: 'auth', done: false}]}; api = async () => state.onboarding.status;`);
  await run('onboardingNext()');
  assert.equal(run('state.onboarding.step'), 'llm');
});
```

- [ ] **Step 2: Run** — `go test ./cmd/dzzzr/ -run 'OnboardingUI' -count=1` и `node --test cmd/dzzzr/webui_tests/` → FAIL.

- [ ] **Step 3: Implement** — перенести encx `app.js:2339-2720` в `onboarding.js`; заменить LLM-контроллеры на вызовы `llm.js` с префиксом `'onboarding-llm'`; `defaultOnboardingRole(isEditor)`; `prefillOnboardingAuth` заполняет город из `/api/v1/auth/status`; `onOnboardingAuthSubmit` шлёт `{role, login, password}`; после успеха — `closeOnboarding()`, `loadAuthStatus()`, `loadAgentConfig()` и, в редакторе, обновление статуса организатора (функция из `editor.js`: `grep -n "admin/status" cmd/dzzzr/webui/editor.js`). `initOnboarding()`: `GET /onboarding`, при `required` — `openOnboarding()`. ◈ — `POST /onboarding/reset`, затем `openOnboarding()`.

- [ ] **Step 4: Run** — оба теста → PASS; `go test ./... -count=1`; `node --test scripts/test-drafts.cjs cmd/dzzzr/webui_tests/`.

- [ ] **Step 5: Commit** — `git commit -m "feat: first-run onboarding wizard in the web UI"`

---

### Task 9: Ручная проверка и документация

**Files:**
- Modify: `README.md` (раздел про настройку модели: мастер, панель ⚙, приоритет env над UI, где лежат файлы)

- [ ] **Step 1:** `go build -o /tmp/dzzzr-onb ./cmd/dzzzr` и `go build -o /tmp/dzzzr-mock ./cmd/dzzzr-mock`; запустить mock и `DZZZR_CONFIG_DIR=$(mktemp -d) DZZZR_BASE_URL=<mock> /tmp/dzzzr-onb editor` (env LLM не задан; `$HOME` не трогать).
- [ ] **Step 2:** через agent-browser: мастер открылся сам; шаг «Модель» → вкладка OpenAI-совместимый, ввести ключ `test`, base URL `http://127.0.0.1:9/v1` → «Далее» доступна; шаг «Вход» — Организатор выбран по умолчанию, неверный пароль показывает ошибку, верный закрывает мастер; `brand-model` обновился; ⚙ открывает панель с сохранёнными значениями (ключ маскирован); перезагрузка страницы — мастер не открывается; ◈ открывает снова. Скриншоты каждого шага в `/tmp/onb-*.png`.
- [ ] **Step 3:** повторить с `DZZZR_LLM_MODEL=env/model` — в панели и мастере видна плашка перекрытия.
- [ ] **Step 4:** README; `go test ./... -count=1`, `go vet ./...`, `node --test cmd/dzzzr/webui_tests/ scripts/test-drafts.cjs` → всё зелёное.
- [ ] **Step 5: Commit** — `git commit -m "docs: describe LLM onboarding and settings panel"`
