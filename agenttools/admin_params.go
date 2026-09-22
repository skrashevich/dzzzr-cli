package agenttools

import (
	"encoding/json/v2"
	"fmt"
	"strconv"
)

// decodeAdminParams preserves explicit false, zero pointer values and empty
// slices. Reject misspelled fields rather than silently dropping an edit.
func decodeAdminParams(args arguments, target any) error {
	value, ok := args["params"].(map[string]any)
	if !ok || len(value) == 0 {
		return fmt.Errorf("argument %q must be a non-empty object", "params")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("params: %w", err)
	}
	if err := json.Unmarshal(data, target, json.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("params: %w", err)
	}
	return nil
}

func levelCodeSchema() map[string]any {
	s := schema(map[string]any{
		"code":     strProp("Код"),
		"synonyms": strProp("Синонимы кода, разделённые #"),
		"danger":   strProp("КО: 1, 1+, 2, 2+, 3, 3+, null"),
		"sector":   intProp("Номер сектора с 1; 0 — без сектора")})
	s["additionalProperties"] = false
	return s
}

func bonusCodeSchema() map[string]any {
	s := schema(map[string]any{
		"code":     strProp("Код"),
		"synonyms": strProp("Синонимы кода, разделённые #"),
		"danger":   strProp("КО: 1, 1+, 2, 2+, 3, 3+, null"),
		"minutes":  intProp("Бонусное время за найденный код, минуты")})
	s["additionalProperties"] = false
	return s
}

func fakeCodeSchema() map[string]any {
	s := schema(map[string]any{
		"code":     strProp("Код"),
		"synonyms": strProp("Синонимы кода, разделённые #"),
		"penalty":  intProp("Штраф за нахождение ложного кода, минуты")})
	s["additionalProperties"] = false
	return s
}

func spoilerParamsSchema() map[string]any {
	s := schema(map[string]any{
		"text":     strProp("Текст спойлера"),
		"code":     strProp("Код спойлера"),
		"synonyms": strProp("Синонимы к спойлеру, разделённые #"),
		"penalty":  intProp("Штраф за открытый спойлер, минуты (без минуса)")})
	s["additionalProperties"] = false
	return s
}

func gameParamsSchema() map[string]any {
	s := schema(map[string]any{
		"name":               strProp("Название игры"),
		"number":             strProp("Номер игры"),
		"date":               strProp("Дата DD.MM.YYYY"),
		"time":               strProp("Время HH:MM"),
		"author":             strProp("Автор"),
		"league":             intProp("ID лиги"),
		"other_league":       boolProp("Могут играть команды другой лиги"),
		"zone":               strProp("Зона игры: 0 — творческая, 1 — на скорость"),
		"legend":             strProp("Легенда"),
		"legend_comment":     strProp("Комментарий к легенде"),
		"anons":              strProp("Дополнительная информация по игре для игроков, например место и время брифинга"),
		"clue_min":           intProp("Время от выдачи задания до первой подсказки по умолчанию для заданий игры, минуты"),
		"clue_min2":          intProp("Время от первой до второй подсказки по умолчанию, минуты"),
		"clue_min3":          intProp("Время от второй подсказки до окончания задания по умолчанию, минуты"),
		"master_code":        strProp("Универсальный код (мастер-код): можно использовать один раз за игру вместо любого кода"),
		"master_code_shtraf": intProp("Штраф за использование универсального кода, минуты"),
		"clue_before_shtraf": intProp("Штраф за досрочное взятие подсказки: время до подсказки плюс столько минут"),
		"penalty":            intProp("Штраф за непройденный уровень по умолчанию, минуты"),
		"price":              strProp("Стоимость участия"),
		"breaf_place":        strProp("Время и место сбора перед игрой"),
		"greeting":           strProp("Приветственное сообщение, выдаётся после завершения игры"),
		"clear": map[string]any{"type": "array", "items": strProp("Имя текстового параметра"),
			"description": "Явно очистить текстовые параметры: движок сохраняет пустой текст, а пустое значение обычного поля считается «не задано». Применяется после остальных полей"},
		"duration":    intProp("Продолжительность игры, часы"),
		"bonus_after": intProp("Сколько минут принимать бонусные коды после отправки всех основных"),
		"publish":     boolProp("Опубликовать игру на сайте"),
		"finished":    boolProp("Игра завершена: к завершённым играм нельзя добавлять задания"),
		"invitation":  boolProp("Открыть приём заявок от команд")})
	s["additionalProperties"] = false
	return s
}

func levelParamsSchema() map[string]any {
	s := schema(map[string]any{
		"title":                 strProp("Название уровня: видно в архиве игры и в панели ведения"),
		"subtitle":              strProp("Название для выбора уровня командой самостоятельно, если линейка это позволяет"),
		"question":              strProp("Текст задания"),
		"hint1":                 strProp("Подсказка 1"),
		"hint2":                 strProp("Подсказка 2"),
		"location":              strProp("Расположение локации и кодов; видит только организатор в панели ведения"),
		"location_comment":      strProp("Примечания к заданию для игроков: «не шумите на локации», «осторожно, собаки»"),
		"comment":               strProp("Комментарии: логика решения и фото кодов, открываются на сайте после игры; пустая строка очищает"),
		"time_add_bonus_all":    intProp("Бонусное время за нахождение всех бонусных кодов, минуты; 0 сбрасывает"),
		"codes":                 map[string]any{"type": "array", "items": levelCodeSchema(), "description": "Основные коды"},
		"sector_names":          map[string]any{"type": "array", "items": strProp("Название сектора"), "description": "Имена секторов"},
		"bonus_codes":           map[string]any{"type": "array", "items": bonusCodeSchema(), "description": "Бонусные коды"},
		"fake_codes":            map[string]any{"type": "array", "items": fakeCodeSchema(), "description": "Ложные коды"},
		"spoilers":              map[string]any{"type": "array", "items": spoilerParamsSchema(), "description": "Спойлеры"},
		"clue_min":              intProp("Интервал от выдачи задания до первой подсказки, минуты; 0 — стандартный из игры"),
		"clue_min2":             intProp("Интервал от первой до второй подсказки, минуты; 0 — стандартный из игры"),
		"clue_min3":             intProp("Интервал от второй подсказки до окончания уровня, минуты; 0 — стандартный из игры"),
		"hint1_on_request":      boolProp("Подсказку 1 выдавать по запросу"),
		"hint1_interval":        intProp("Подсказку 1 по запросу выдавать не ранее чем через столько минут после получения задания"),
		"hint1_penalty":         intProp("Штраф за использование подсказки 1 по запросу, бонусные минуты"),
		"hint2_on_request":      boolProp("Подсказку 2 выдавать по запросу"),
		"hint2_interval":        intProp("Подсказку 2 по запросу выдавать не ранее чем через столько минут после получения задания"),
		"hint2_penalty":         intProp("Штраф за использование подсказки 2 по запросу, бонусные минуты"),
		"code_count":            intProp("Сколько основных кодов достаточно найти для выполнения задания; 0 сохраняет текущее значение при обновлении"),
		"try_limit":             intProp("Количество попыток ввода кода; в игре может быть только одно такое задание"),
		"penalty":               intProp("Штраф за непройденный уровень, минуты; пусто — настройка игры"),
		"no_master_code":        boolProp("Запретить в задании универсальный код игры"),
		"is_sabotage":           boolProp("Задание типа «Саботаж»: команды ищут бонусные коды, каждый код может ввести только одна команда"),
		"bonus":                 boolProp("Бонусный уровень"),
		"bonus_time":            intProp("Время бонусного уровня, минуты"),
		"skvoz":                 boolProp("Сквозной бонусный уровень"),
		"skvoz_min":             intProp("Ограничить время выполнения сквозного задания, минуты с момента выдачи"),
		"skvoz_start":           strProp("Выдать сквозное задание в: ГГГГ-ММ-ДД ЧЧ:ММ:СС или минуты от начала игры"),
		"lat":                   strProp("Широта"),
		"lon":                   strProp("Долгота"),
		"radius":                strProp("Радиус локации"),
		"show_coord_with_hint2": boolProp("Показывать координаты локации вместе со второй подсказкой"),
		"no_break":              boolProp("Запретить брать перерыв после этого задания"),
		"clue_before":           boolProp("Разрешить брать подсказки досрочно"),
		"reserve":               boolProp("Запасной уровень: нельзя сделать активным"),
		"publish":               boolProp("Задание активно"),
		"clear": map[string]any{"type": "array", "items": strProp("Имя текстового параметра"),
			"description": "Явно очистить текстовые параметры уровня (title, question, hint1, hint2, location, location_comment, comment, lat, lon, radius, subtitle, skvoz_start)"}})
	s["additionalProperties"] = false
	return s
}

func applicationParamsSchema() map[string]any {
	s := schema(map[string]any{
		"status":  map[string]any{"type": "integer", "enum": []int{0, 1, 2, 3, 4, 5}, "description": "Статус заявки: 0 ожидает, 1 принята, 2 отклонена, 3 вне зачёта, 4 автор, 5 дисквалификация"},
		"new_pin": boolProp("Выдать новый PIN"),
		"points":  intProp("Рейтинговые очки"),
		"novice":  boolProp("Команда-новичок"),
		"comment": strProp("Комментарий капитану по электронной почте")})
	s["additionalProperties"] = false
	return s
}

// requireAdminInt accepts integer JSON numbers and decimal integer strings,
// without coercing booleans or truncating fractional identifiers.
func requireAdminInt(args arguments, key string) (int, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return 0, fmt.Errorf("argument %q must be an integer", key)
	}
	if text, ok := raw.(string); ok {
		n, err := strconv.Atoi(text)
		if err != nil {
			return 0, fmt.Errorf("argument %q must be an integer", key)
		}
		return n, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return 0, fmt.Errorf("argument %q: %w", key, err)
	}
	var n int
	if err := json.Unmarshal(data, &n); err != nil {
		return 0, fmt.Errorf("argument %q must be an integer", key)
	}
	return n, nil
}
