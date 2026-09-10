# dzzzr-cli

Go-библиотека и консольная утилита для движка городской игры **«Дозор Классик»**
(`classic.dzzzr.ru`). Позволяет играть и администрировать игру без браузера, а также
встраивать клиент движка в свой код.

В комплекте: библиотека, CLI, mock-сервер движка для разработки, инструменты для
LLM-агентов с MCP-сервером и gomobile-биндинги для мобильного приложения игрока.

Проект повторяет по структуре и функциям
[`encx-cli`](https://github.com/skrashevich/encx-cli) — такой же клиент для движка
Encounter.

## Что внутри

| Каталог | Назначение |
| --- | --- |
| `dzzzr/` | клиент движка: игрок и организатор |
| `cmd/dzzzr/` | консольная утилита |
| `cmd/dzzzr-mock/` | mock-сервер движка, см. [README](cmd/dzzzr-mock/README.md) |
| `agenttools/` | каталог инструментов движка для LLM-агентов с политиками доступа |
| `agentmcp/` | MCP-сервер над этим каталогом |
| `agentloop/` | цикл LLM-агента над каталогом, на базе [PicoClaw](https://github.com/sipeed/picoclaw) |
| `agentfiles/` | чтение локальных файлов и сохранение новых JSON-документов агентом |
| `gamesource/`, `pdfsource/` | импорт сценария игры из JSON и PDF |
| `mobile/dzzrmobile/` | gomobile-обёртка для iOS и Android |
| `e2e/` | сквозные тесты: собранная утилита против mock-сервера |

## Установка

```sh
go install github.com/skrashevich/dzzzr-cli/cmd/dzzzr@latest
go install github.com/skrashevich/dzzzr-cli/cmd/dzzzr-mock@latest
```

Библиотека:

```sh
go get github.com/skrashevich/dzzzr-cli/dzzzr
```

Docker:

```sh
docker run --rm ghcr.io/skrashevich/dzzzr-cli version
docker run --rm -p 18090:18090 -e DZZR_MOCK_ADDR=0.0.0.0:18090 ghcr.io/skrashevich/dzzzr-mock
```

## Быстрый старт

```sh
# вход: спросит недостающие логин и пароль
dzzzr login -city moscow

# что происходит в игре
dzzzr status

# текст задания и подсказки
dzzzr level
dzzzr hints

# отправить код
dzzzr send-code "(112)D45R92"
```

Всё это можно попробовать против mock-сервера, ничего не задев на боевом движке:

```sh
dzzzr-mock &                       # слушает 0.0.0.0:18090
export DZZR_BASE_URL=http://127.0.0.1:18090/moscow/
dzzzr login -login demo -password demo -captain demo -pin 1234
dzzzr status
dzzzr send-code 1
```

Отправка кода завершается кодом выхода `0`, если движок код принял, и `2`, если
отклонил, — удобно в скриптах:

```sh
if dzzzr send-code "$code"; then echo принят; else echo мимо; fi
```

Полный список команд игрока и организатора, флагов и переменных окружения —
в [docs/cli.md](docs/cli.md).

## Авторизация

1. **Аккаунт сайта** — логин и пароль. `API/login.php` меняет их на токен сессии.
   Этого достаточно, чтобы играть.
2. **Заголовок HTTP Basic** — движок требует, чтобы он просто был. Утилита
   подставляет `{город}_{логин}` с пустым паролем; логин капитана и PIN
   (`-captain`, `-pin`) нужны только там, где организатор их выдал.
3. **Учётная запись организатора** — отдельные логин и пароль от админки, с игровыми
   никак не связаны.

Утилита хранит всё это в `~/.config/dzzzr/<город>.json` с правами `600`. Файл содержит
секреты — не кладите его в общий каталог.

## ИИ-агент

Движок можно отдать LLM-агенту как каталог инструментов — по MCP или встроенным
циклом прямо из утилиты:

```sh
dzzzr mcp                                    # MCP-сервер, только чтение
export DZZR_LLM_API_KEY=sk-…
dzzzr agent "на каком мы уровне и сколько кодов осталось"
dzzzr chat                                   # полноэкранный диалог
```

Политика доступа (`readonly` / `approve` / `full`) задаётся флагом `-security`.
Агент умеет импортировать сценарий игры из PDF. Подробно — в
[docs/agent.md](docs/agent.md).

## Мобильные биндинги

`mobile/dzzrmobile` — gomobile-обёртка: методы возвращают JSON-строки, все целые
`int64`, контекста нет.

```sh
./mobile/bind-ios.sh        # → mobile/build/dzzzr.xcframework
./mobile/bind-android.sh    # → mobile/build/dzzzr.aar (нужны ANDROID_HOME и NDK)
```

## Разработка

```sh
go build ./...
go vet ./...
go test ./... -count=1
go test -race ./... -count=1
go test -tags e2e ./e2e -count=1     # собранная утилита против mock-сервера
golangci-lint run ./...              # нужен golangci-lint v2
```

Живые тесты против боевого движка включаются переменными `DZZR_INTEGRATION=1`,
`DZZR_E2E_CITY`, `DZZR_E2E_LOGIN`, `DZZR_E2E_PASSWORD`, `DZZR_E2E_CAPTAIN`,
`DZZR_E2E_PIN`. Без них они пропускаются.

## Документация

| Файл | О чём |
| --- | --- |
| [docs/cli.md](docs/cli.md) | все команды CLI, флаги и переменные окружения |
| [docs/library.md](docs/library.md) | использование пакета `dzzzr` как библиотеки |
| [docs/agent.md](docs/agent.md) | инструменты для ИИ-агентов, MCP, `dzzzr chat`, импорт сценария из PDF |
| [docs/engine-notes.md](docs/engine-notes.md) | как на самом деле ведёт себя движок «Дозор Классик» |
| [docs/design.md](docs/design.md) | дизайн проекта |
| [docs/source-helper-parity.md](docs/source-helper-parity.md) | сверка возможностей заливки сценария с DzrSourceHelper |
| [cmd/dzzzr-mock/README.md](cmd/dzzzr-mock/README.md) | mock-сервер движка и его `-quirks` |

## Лицензия

Apache 2.0, см. [LICENSE](LICENSE).
