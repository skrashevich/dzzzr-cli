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
		"danger":   strProp("Сложность: 1, 1+, 2, 2+, 3, 3+, null"),
		"sector":   intProp("Номер сектора с 1; 0 — без сектора")})
	s["additionalProperties"] = false
	return s
}

func bonusCodeSchema() map[string]any {
	s := schema(map[string]any{
		"code":     strProp("Код"),
		"synonyms": strProp("Синонимы кода, разделённые #"),
		"danger":   strProp("Сложность: 1, 1+, 2, 2+, 3, 3+, null"),
		"minutes":  intProp("Бонусные минуты")})
	s["additionalProperties"] = false
	return s
}

func fakeCodeSchema() map[string]any {
	s := schema(map[string]any{
		"code":     strProp("Код"),
		"synonyms": strProp("Синонимы кода, разделённые #"),
		"penalty":  intProp("Штраф в минутах")})
	s["additionalProperties"] = false
	return s
}

func spoilerParamsSchema() map[string]any {
	s := schema(map[string]any{
		"text":     strProp("Текст спойлера"),
		"code":     strProp("Код"),
		"synonyms": strProp("Синонимы кода, разделённые #"),
		"penalty":  intProp("Штраф в минутах")})
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
		"other_league":       boolProp("Игра другой лиги"),
		"zone":               strProp("Зона игры"),
		"legend":             strProp("Легенда"),
		"legend_comment":     strProp("Комментарий к легенде"),
		"anons":              strProp("Анонс"),
		"clue_min":           intProp("Интервал первой подсказки, минуты"),
		"clue_min2":          intProp("Интервал второй подсказки, минуты"),
		"clue_min3":          intProp("Интервал третьей подсказки, минуты"),
		"master_code":        strProp("Мастер-код"),
		"master_code_shtraf": intProp("Штраф за мастер-код, минуты"),
		"clue_before_shtraf": intProp("Штраф за досрочную подсказку, минуты"),
		"penalty":            intProp("Штраф в минутах"),
		"price":              strProp("Стоимость участия"),
		"breaf_place":        strProp("Место брифинга"),
		"greeting":           strProp("Приветствие"),
		"clear": map[string]any{"type": "array", "items": strProp("Имя текстового параметра"),
			"description": "Явно очистить текстовые параметры: движок сохраняет пустой текст, а пустое значение обычного поля считается «не задано». Применяется после остальных полей"},
		"duration":    intProp("Продолжительность"),
		"bonus_after": intProp("Бонус после"),
		"publish":     boolProp("Опубликовать"),
		"finished":    boolProp("Игра завершена"),
		"invitation":  boolProp("Приём заявок")})
	s["additionalProperties"] = false
	return s
}

func levelParamsSchema() map[string]any {
	s := schema(map[string]any{
		"title":                 strProp("Название уровня"),
		"subtitle":              strProp("Подзаголовок"),
		"question":              strProp("Текст задания"),
		"hint1":                 strProp("Первая подсказка"),
		"hint2":                 strProp("Вторая подсказка"),
		"location":              strProp("Место"),
		"location_comment":      strProp("Комментарий к месту"),
		"comment":               strProp("Комментарий организатора и фото кодов; пустая строка очищает"),
		"time_add_bonus_all":    intProp("Дополнительный бонус за все бонусные коды, минуты; 0 сбрасывает"),
		"codes":                 map[string]any{"type": "array", "items": levelCodeSchema(), "description": "Основные коды"},
		"sector_names":          map[string]any{"type": "array", "items": strProp("Название сектора"), "description": "Названия секторов"},
		"bonus_codes":           map[string]any{"type": "array", "items": bonusCodeSchema(), "description": "Бонусные коды"},
		"fake_codes":            map[string]any{"type": "array", "items": fakeCodeSchema(), "description": "Ложные коды"},
		"spoilers":              map[string]any{"type": "array", "items": spoilerParamsSchema(), "description": "Спойлеры"},
		"clue_min":              intProp("Интервал первой подсказки, минуты"),
		"clue_min2":             intProp("Интервал второй подсказки, минуты"),
		"clue_min3":             intProp("Интервал третьей подсказки, минуты"),
		"hint1_on_request":      boolProp("Первая подсказка по запросу"),
		"hint1_interval":        intProp("Интервал первой подсказки"),
		"hint1_penalty":         intProp("Штраф первой подсказки"),
		"hint2_on_request":      boolProp("Вторая подсказка по запросу"),
		"hint2_interval":        intProp("Интервал второй подсказки"),
		"hint2_penalty":         intProp("Штраф второй подсказки"),
		"code_count":            intProp("Число кодов для прохождения; 0 сохраняет текущее значение при обновлении"),
		"try_limit":             intProp("Лимит попыток"),
		"penalty":               intProp("Штраф в минутах"),
		"no_master_code":        boolProp("Запретить мастер-код"),
		"is_sabotage":           boolProp("Уровень-диверсия"),
		"bonus":                 boolProp("Бонусный уровень"),
		"bonus_time":            intProp("Бонусное время"),
		"skvoz":                 boolProp("Сквозной бонусный уровень"),
		"skvoz_min":             intProp("Время сквозного уровня, минуты"),
		"skvoz_start":           strProp("Начало сквозного уровня"),
		"lat":                   strProp("Широта"),
		"lon":                   strProp("Долгота"),
		"radius":                strProp("Радиус"),
		"show_coord_with_hint2": boolProp("Показать координаты со второй подсказкой"),
		"no_break":              boolProp("Запретить перерыв"),
		"clue_before":           boolProp("Разрешить досрочные подсказки"),
		"reserve":               boolProp("Запасной уровень"),
		"publish":               boolProp("Опубликовать"),
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
