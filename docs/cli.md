# Команды CLI

Полный справочник команд игрока и организатора, флагов и переменных окружения.
Краткий обзор — в [README](../README.md).

## Команды игрока

| Команда | Что делает |
| --- | --- |
| `login` | Входит на сайт и сохраняет сессию вместе с логином капитана и PIN |
| `logout` | Удаляет сохранённую сессию города |
| `games [-new] [-archive]` | Список игр города |
| `status` | Состояние игры: уровень, коды, таймер, сообщения |
| `level` | Текст текущего задания без разметки |
| `hints` | Подсказки текущего уровня и время до следующей |
| `bonus-levels` | Сквозные бонусные задания |
| `stat` | Статистика команды по уровням |
| `log` | Журнал действий команды |
| `game-stat ИГРА [-levels]` | Итоги завершённой игры по всем командам: время на заданиях, штрафы, бонусы, места |
| `game-desc ИГРА` | Описание (сценарий) игры: легенда, задания, коды, подсказки, спойлеры, комментарии авторов |
| `game-log ИГРА` | Полный лог игры по всем командам |

| `messages [-after «…»]` | Переписка с организатором |
| `send-message ТЕКСТ…` | Сообщение организатору |
| `send-code КОД` | Код текущего уровня |
| `send-bonus УРОВЕНЬ КОД` | Код сквозного бонусного задания |
| `spoiler УРОВЕНЬ КОД` | Код спойлера |
| `hint УРОВЕНЬ НОМЕР` | Запросить подсказку 1 или 2 там, где они по запросу |
| `hint-early [НОМЕР] [УРОВЕНЬ]` | Взять подсказку досрочно, со штрафом (по умолчанию — ещё не выданную на текущем уровне) |
| `abandon` | Отказаться от задания (дважды за игру, только капитан или штаб) |
| `break` | Взять перерыв 15 минут после текущего уровня или отменить запланированный |
| `break-stop` | Завершить перерыв досрочно |
| `next-level [УРОВЕНЬ]` | Перейти дальше, оставив бонусные коды |
| `select-level [СЛЕДУЮЩИЙ]` | Выбрать уровень в играх со свободным порядком; без аргумента — список доступных |
| `agent ЗАПРОС…` | Запрос к модели на естественном языке, см. [Встроенный агент](#встроенный-агент) |
| `chat` | Полноэкранный диалог с агентом |
| `web [-web-addr АДРЕС]` | Тот же агент в браузере плюс ручной редактор игры; по умолчанию `127.0.0.1:8788`, см. [docs/webui.md](webui.md) |
| `editor [-web-addr АДРЕС]` | То же, что `web`, но браузер открывается сразу в редакторе |
| `version` | Версия |

Статистика игры публична. Сценарий и лог используют сохранённую сессию;
доступность заданий и лога определяет движок. При отказе `game-desc`
показывает шапку и объяснение сайта, а `game-log` сообщает ошибку.

Отправка кода завершается кодом выхода `0`, если движок код принял, и `2`, если отклонил, — удобно в скриптах:

```sh
if dzzzr send-code "$code"; then echo принят; else echo мимо; fi
```

С `-json` результат действия несёт `outcome`: `accepted`, `rejected` или
`unknown`. Третье значение существует потому, что управляющие действия
(`abandon`, `break`, `break-stop`, `next-level`) движок отклоняет ровно так
же, как выполняет: полным состоянием с `errNo` 0 и без формулировки. Поле
`accepted` означает «движок подтвердил», а не «скорее всего получилось»;
при `unknown` код выхода остаётся `0`, потому что отказом это тоже назвать
нельзя — проверяйте состояние через `dzzzr status`.

## Команды организатора

Требуют `-admin-login` и `-admin-password` (или `DZZZR_ADMIN_LOGIN` / `DZZZR_ADMIN_PASSWORD`).

```sh
# игры
dzzzr admin-games
dzzzr admin-game-info 4242
dzzzr admin-create-game name="Ночной дозор" date=10.09.2026 time=21:00 league=1
dzzzr admin-update-game 4242 clue_min=25 publish=true
dzzzr admin-copy-game 4242 -with-levels
dzzzr admin-delete-game 4243

# уровни
dzzzr admin-levels 4242
dzzzr admin-level 4242 901
dzzzr admin-create-level 4242 title="Гаражи" question="Найдите три кода" \
  hint1="Смотрите выше" hint2="За углом" \
  codes="(112)D45R92:1:1,(112)D46R93:2:2" sector_names="Гаражи,Пустырь" \
  bonus_codes="(112)B1:1:10" spoilers="SP1:5:Скрытая часть задания"
dzzzr admin-update-level 4242 901 code_count=2 try_limit=4
dzzzr admin-delete-level 4242 903
dzzzr admin-move-level 4242 901 down   # на одну позицию; up|down
dzzzr admin-copy-levels 4300 4242

# команды и заявки
dzzzr admin-teams 4242
dzzzr admin-city-teams 4242 "стальные"   # найти id команды по названию
dzzzr admin-set-application 4242 5150 status=1 new-pin=true
dzzzr admin-accept-all 4242
dzzzr admin-generate-pins 4242

# ход игры
dzzzr admin-monitor 4242
dzzzr admin-log 4242
dzzzr admin-give-level 4242 5150 2
dzzzr admin-accept-code 4242 5150 1 "(112)D45R92"
dzzzr admin-add-correction 4242 5150 bonus 10 "за красивое решение"
dzzzr admin-block-team 4242 5150
dzzzr admin-engine 4242 off        # читает состояние и жмёт переключатель, если нужно
dzzzr admin-finish-game 4242

# сообщения командам
dzzzr admin-messages 4242
dzzzr admin-send-message 4242 "Через 10 минут закрываем движок" -important
```

Значения `key=value` разбираются так: `codes="КОД:сложность:сектор"` через запятую (синонимы кода после `#`), `bonus_codes="КОД:сложность:минуты"`, `fake_codes="КОД:штраф"`, `spoilers="КОД:штраф:текст"`, `sector_names="Первый,Второй"`. Логические значения принимают `true`/`false`, `1`/`0`, `да`/`нет`.

## Флаги и переменные окружения

| Флаг | Переменная | Назначение |
| --- | --- | --- |
| `-city` | `DZZZR_CITY` | Город, сегмент пути движка (по умолчанию `moscow`) |
| `-login`, `-password` | `DZZZR_LOGIN`, `DZZZR_PASSWORD` | Аккаунт сайта |
| `-captain`, `-pin` | `DZZZR_CAPTAIN`, `DZZZR_PIN` | Логин капитана и PIN, если организатор их выдал |
| `-admin-login`, `-admin-password` | `DZZZR_ADMIN_LOGIN`, `DZZZR_ADMIN_PASSWORD` | Доступ к админке |
| `-base-url` | `DZZZR_BASE_URL` | Другой адрес движка, например mock |
| `-http` | — | HTTP вместо HTTPS для стандартного хоста |
| `-insecure` | `DZZZR_INSECURE` | Не проверять TLS-сертификат |
| `-json` | — | Машинночитаемый вывод |
| `-debug` | `DZZZR_DEBUG` | Печатать запросы в stderr (токен и PIN скрыты) |
| `-har`, `-har-out` | `DZZZR_HAR`, `DZZZR_HAR_OUT` | Записать трафик в HAR-файл |
| `-timeout` | — | Таймаут HTTP в секундах |
| `-security` | — | Права агента: `readonly`, `approve` или `full` (`agent`, `chat`, `web`, `mcp`) |
| `-levels` | — | Показывать колонки заданий, а не только итоги (`game-stat`) |
