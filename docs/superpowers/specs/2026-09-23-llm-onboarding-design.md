# Онбординг и настройка LLM в веб-интерфейсе — дизайн

Дата: 2026-09-23. Источник: encx-cli (`../en-app-research/cmd/encli`), коммиты
e648e94, e3669c2, 0a4e531, 9437455, 3b6464c, 71eb393.

## Цель

Новый пользователь `dzzzr web` / `dzzzr editor` (в том числе двойной клик по exe
на Windows) настраивает модель прямо в браузере — без переменных окружения — и
входит в Дозор. Сейчас LLM настраивается только через env `DZZZR_LLM_*`, а
Codex — только через готовый `~/.codex/auth.json` от Codex CLI.

## Решения

- **Полный паритет с encx-cli** по провайдерам: Polza.ai (OAuth, выбор модели,
  баланс, ключ), подписка ChatGPT (собственный OAuth-вход), OpenAI-совместимый
  провайдер (base URL / ключ / модель, по умолчанию OpenRouter).
- **Подход: портирование** слоя из encx с адаптацией под идиомы dzzzr
  (`agentloop.Config`, `webWriteJSON`/`webError`, русские сообщения, env
  `DZZZR_*`). Общий модуль между репозиториями не выделяем.
- **Мастер только в веб-интерфейсе.** Сохранённые настройки при этом читает
  `resolveLLMConfig()`, поэтому они действуют и в `dzzzr chat`, и в MCP.
- **Шаг «Вход»:** переключатель Игрок / Организатор, достаточно одного
  успешного входа. В режиме редактора по умолчанию — Организатор. Город
  показывается только для чтения (один запуск обслуживает один город).

## Backend (`cmd/dzzzr/`)

### `state_file.go`
Общие `stateFilePath(envVar, subdir, name)`, `loadJSONState[T]`,
`saveJSONState`, `deleteJSONState`. Файлы — 0600, каталоги — 0700, запись через
`writeSecretFile`. Корень — `sessionDir()` (уважает `DZZZR_CONFIG_DIR`).
Состояние лежит в подкаталогах `llm/`, `onboarding/`, `codex/`, чтобы
`httpAuthLogout` (удаляет файл сессии города) их не задевал. Отсутствующий файл —
нулевое значение, не ошибка.

### `llm_settings.go`
`llmSettings{AuthMethod, BaseURL, APIKey, Model}` в `llm/settings.json`
(перенос: `DZZZR_LLM_SETTINGS_FILE`). `resolveLLMField` с приоритетом
**флаг → env → файл → значение по умолчанию**; env перекрывает файл, и API
сообщает, какая переменная победила. Списки env:

| поле | переменные |
|---|---|
| auth_method | `DZZZR_LLM_PROVIDER` |
| api_key | `DZZZR_LLM_API_KEY`, `LLM_API_KEY`, `OPENROUTER_API_KEY` |
| base_url | `DZZZR_LLM_BASE_URL`, `LLM_BASE_URL`, `OPENROUTER_BASE_URL` |
| model | `DZZZR_LLM_MODEL`, `LLM_MODEL`, `OPENROUTER_MODEL` |

Значения auth_method: `""`/`openai`/`openrouter`/`apikey` (API-ключ), `codex`
(синоним `chatgpt`), `polza`. `validateLLMBaseURL`, `maskAPIKey` — как в encx.
Нечитаемый файл настроек — ошибка резолвинга (не молча другой провайдер).

`resolveLLMConfig()` переписывается на `resolveLLMField`. Без явного
auth_method, ключа и base URL при наличии входа ChatGPT выбирается Codex.
Существующие env-сценарии и сообщения об ошибках сохраняют поведение
(существующие тесты `agentcodex_test.go`, `agentcontext_test.go` проходят).

### Codex (`llm_auth_codex.go`, правка `agentcodex.go`)
Порт OAuth PKCE-входа из encx (`llm_auth_codex.go`): свой токен в
`<sessionDir>/codex/auth.json`, обновление refresh-токена. Порядок источника
учётных данных: `DZZZR_CODEX_AUTH_FILE` → собственный вход dzzzr →
`$CODEX_HOME/auth.json` / `~/.codex/auth.json` (обратная совместимость).
Проверка совместимости модели с Codex сохраняется.

### Polza.ai (`llm_polza.go`)
OpenAI-совместимый транспорт с base URL Polza; OAuth-вход, список моделей с
поддержкой инструментов, баланс, подключение по ключу — порт `web_llm_polza.go`.

### Web API (`web_llm.go`, `web_llm_codex.go`, `web_llm_polza.go`)
```
GET/PUT/DELETE /api/v1/llm/settings
POST   /api/v1/llm/polza/login        GET/DELETE /api/v1/llm/polza/login/{id}
POST   /api/v1/llm/polza/login/{id}/code
GET    /api/v1/llm/polza/models       POST /api/v1/llm/polza/connect
POST   /api/v1/llm/polza/check
GET    /api/v1/llm/codex/status       POST /api/v1/llm/codex/logout
POST   /api/v1/llm/codex/login        GET/DELETE /api/v1/llm/codex/login/{id}
POST   /api/v1/llm/codex/login/{id}/code
```
Ключи в ответах только маскированные. `GET /api/v1/agent/config` без изменений
по форме.

### Онбординг (`onboarding.go`, `web_onboarding.go`)
Состояние `{completed, completed_at, skipped}` в `onboarding/state.json`
(перенос: `DZZZR_ONBOARDING_FILE`).
- `GET /api/v1/onboarding` → `{required, completed, completed_at, skipped,
  steps:[{id:"llm"},{id:"auth"}], error?}`. Шаг `llm` готов, если
  `llmConfig()` без ошибки; `auth` — если есть сессия игрока **или**
  организаторские учётные данные. Битый файл состояния — `error` в ответе,
  `required: true`, не 500.
- `POST /api/v1/onboarding/complete` `{role: "player"|"organizer", login,
  password}` — выполняет тот же вход, что `httpAuthLogin` / `httpAdminLogin`
  (логика выносится в общие функции), при успехе сохраняет состояние.
  Ошибки входа — те же коды, что у исходных обработчиков (401/502/400).
- `POST /api/v1/onboarding/reset` — удаляет состояние.

Смена настроек применяется со следующего хода агента: `llmConfig()` вызывается
на каждый ход (`web_agent.go`), перезапуск не нужен.

## UI (`cmd/dzzzr/webui/`)

Новый код — отдельными файлами, `app.js` не раздувается:
- `llm.js` — контроллер вкладок провайдеров, переиспользуемый панелью и
  мастером (префиксы id `llm-*` и `onboarding-llm-*`): вкладки Polza.ai /
  Подписка ChatGPT / OpenAI-совместимый провайдер, OAuth-опрос с отменой, ручной
  ввод redirect URL, выбор модели Polza и баланс, плашки «перекрыто env …».
- `onboarding.js` — мастер Начало → Модель → Вход с индикатором шагов;
  «Далее» на шаге модели доступна только при `steps.llm.done`; шаг входа с
  переключателем Игрок/Организатор (в редакторе по умолчанию Организатор),
  город только для чтения.
- `onboarding.css` — стили модалок и вкладок из encx в палитре dzzzr
  (`#e0343c`, Geologica).
- `index.html` — кнопки ⚙ «Настройки LLM» и ◈ «Мастер настройки» у
  `brand-model` в сайдбаре; разметка двух модалок; подключение скриптов.
- Автооткрытие мастера при загрузке, если `required`, в обоих режимах (чат и
  редактор). ◈ вызывает `reset` и открывает мастер. После сохранения
  обновляется `brand-model`.

## Тестирование

Изоляция: `DZZZR_CONFIG_DIR=t.TempDir()` и/или `DZZZR_*_FILE`; тесты никогда не
подменяют и не трогают `$HOME`.
- Go: приоритеты `resolveLLMField`; маскирование; save/load/reset и битый файл;
  `resolveLLMConfig` с файлом настроек и без (регрессия существующих
  env-сценариев); Codex — источник собственного входа и фолбэк на `~/.codex`
  через `CODEX_HOME`; OAuth Codex и Polza на `httptest`; `onboarding/complete`
  для игрока и организатора против `dzzzr-mock`; маршруты UI-разметки
  (порт `web_onboarding_ui_test.go`).
- JS: `webui_tests/onboarding.test.cjs` (порт из encx).
- Ручная проверка: `dzzzr editor` с чистым `DZZZR_CONFIG_DIR`, прохождение
  мастера в браузере, скриншоты.

## Вне рамок

GigaChat и локальный инференс из encx; выбор города в мастере; мастер в TUI.
