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
| `mobile/dzzzrmobile/` | gomobile-обёртка для iOS и Android |
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
docker run --rm -p 18090:18090 -e DZZZR_MOCK_ADDR=0.0.0.0:18090 ghcr.io/skrashevich/dzzzr-mock
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
export DZZZR_BASE_URL=http://127.0.0.1:18090/moscow/
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

![dzzzr quickstart](https://skrashevich.github.io/dzzzr-cli/gifs/dzzzr-quickstart.gif)

Ещё записи — прохождение уровня, mock-сервер, организаторская часть — на
[GitHub Pages](https://skrashevich.github.io/dzzzr-cli/). Тапки VHS лежат в
[`docs/vhs/`](docs/vhs), собрать локально: `./docs/vhs/render.sh` (нужны `vhs`,
`ttyd`, `ffmpeg`).

## Авторизация

1. **Аккаунт сайта** — логин и пароль. `API/login.php` меняет их на токен сессии.
   Этого достаточно, чтобы играть.
2. **Заголовок HTTP Basic** — движок требует, чтобы он просто был. Утилита
   подставляет `{город}_{логин}` с пустым паролем; логин капитана и PIN
   (`-captain`, `-pin`) нужны только там, где организатор их выдал.
3. **Учётная запись организатора** — отдельные логин и пароль от админки, с игровыми
   никак не связаны.

Утилита хранит всё это в `~/.config/dzzzr/<город>.json` с правами `600`
(`$XDG_CONFIG_HOME/dzzzr`, если переменная задана; на Windows — `%AppData%\dzzzr`,
где права файла заменяет наследуемый доступ к профилю). Каталог целиком
переопределяется переменной `DZZZR_CONFIG_DIR`. Файл содержит секреты — не кладите
его в общий каталог.

## ИИ-агент

Движок можно отдать LLM-агенту как каталог инструментов — по MCP или встроенным
циклом прямо из утилиты:

```sh
dzzzr mcp                                    # MCP-сервер, только чтение
export DZZZR_LLM_API_KEY=sk-…
dzzzr agent "на каком мы уровне и сколько кодов осталось"
dzzzr chat                                   # полноэкранный диалог в терминале
dzzzr web                                    # тот же агент в браузере
```

Политика доступа (`readonly` / `approve` / `full`) задаётся флагом `-security`.
Агент умеет импортировать сценарий игры из PDF и читать публичные веб-страницы
инструментом `fetch_url` — HTML приводится к тексту, кодировка распознаётся,
непубличные адреса (localhost, локальная сеть, служебные диапазоны) не читаются.
Содержимое страницы — данные, а не инструкции. Подробно — в
[docs/agent.md](docs/agent.md).

### Веб-интерфейс

`dzzzr web` поднимает локальный сервер (по умолчанию `127.0.0.1:8788`) с тем же
агентом, что и `dzzzr chat`: переписка нескольких чатов, потоковый ответ,
согласование действий, вход на сайт города и файлы, которые агент сохранил за
время чата. Наружу ничего не публикуется, слушает только петлю.

На Windows запуск `dzzzr.exe` без команды и без терминала открывает этот же
браузерный чат: иначе справка мелькнула бы в консольном окне, которое
закрывается вместе с процессом. Если команда всё же не запустилась, окно
удерживается до нажатия Enter, чтобы ошибку можно было прочитать.

Признак «терминала нет» — ровно один процесс на консоли. Это двойной клик и
ярлык, но так же выглядит и запуск без оболочки из планировщика задач, CI или
`wt.exe`. Запуск из cmd.exe, PowerShell, Git Bash и MSYS2 ведёт себя как
раньше. Любое непустое значение `DZZZR_NO_AUTO_WEB` выключает автозапуск —
в том числе `DZZZR_NO_AUTO_WEB=0`.

| | |
| --- | --- |
| [![Диалог с агентом](https://skrashevich.github.io/dzzzr-cli/screenshots/overview.png)](https://skrashevich.github.io/dzzzr-cli/screenshots/overview.png) | [![Тёмная тема](https://skrashevich.github.io/dzzzr-cli/screenshots/overview-dark.png)](https://skrashevich.github.io/dzzzr-cli/screenshots/overview-dark.png) |
| Переписка, вызовы инструментов, выбор прав агента | Тёмная тема |
| [![Ход агента](https://skrashevich.github.io/dzzzr-cli/screenshots/agent-run.png)](https://skrashevich.github.io/dzzzr-cli/screenshots/agent-run.png) | [![Согласование действия](https://skrashevich.github.io/dzzzr-cli/screenshots/approval.png)](https://skrashevich.github.io/dzzzr-cli/screenshots/approval.png) |
| Статус хода, инструменты, потоковый ответ | Согласование действия под политикой `approve` |
| [![Файлы сессии](https://skrashevich.github.io/dzzzr-cli/screenshots/session-files.png)](https://skrashevich.github.io/dzzzr-cli/screenshots/session-files.png) | [![Вход на сайт](https://skrashevich.github.io/dzzzr-cli/screenshots/login.png)](https://skrashevich.github.io/dzzzr-cli/screenshots/login.png) |
| Файлы, сохранённые агентом за чат | Вход на сайт одного города |

Скриншоты и GIF ниже пересобираются в CI
([`.github/workflows/media.yml`](.github/workflows/media.yml)) из
[`docs/shots/`](docs/shots) и [`docs/vhs/`](docs/vhs) и публикуются на
[GitHub Pages](https://skrashevich.github.io/dzzzr-cli/).

## Мобильные биндинги

`mobile/dzzzrmobile` — gomobile-обёртка: методы возвращают JSON-строки, все целые
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

Живые тесты против боевого движка включаются переменными `DZZZR_INTEGRATION=1`,
`DZZZR_E2E_CITY`, `DZZZR_E2E_LOGIN`, `DZZZR_E2E_PASSWORD`, `DZZZR_E2E_CAPTAIN`,
`DZZZR_E2E_PIN`. Без них они пропускаются.

Демо-медиа для README собираются локально теми же скриптами, что и в CI:

```sh
./docs/vhs/render.sh      # GIF CLI → docs/gifs/   (нужны vhs, ttyd, ffmpeg)
./docs/shots/render.sh    # скриншоты web-ui → docs/screenshots/   (нужен Node)
```

Оба каталога в `.gitignore`: workflow
[`media.yml`](.github/workflows/media.yml) пересобирает их при изменениях в
`docs/vhs/`, `docs/shots/`, `cmd/dzzzr/` или `cmd/dzzzr-mock/` и публикует на
GitHub Pages. Скриншоты снимаются с настоящего `dzzzr web` поверх `dzzzr-mock`;
переписка в чатах — фикстуры из [`docs/shots/fixtures/`](docs/shots/fixtures).

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
