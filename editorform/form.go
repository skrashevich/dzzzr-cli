// Package editorform shares editor layouts and validation between web and WASM.
package editorform

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
	"golang.org/x/text/encoding/charmap"
)

// The editor draws its forms from agenttools.GameParamsSchema and
// agenttools.LevelParamsSchema — the same objects the agent's
// admin_create_game / admin_create_level tools accept. This file only says how
// to lay those fields out for a human: which group a field belongs to and what
// to call it. The help text and the control type are read out of the schema
// itself, never retyped, so the browser and the agent describe one field with
// one sentence.
//
// The layout below must name every schema property exactly once, and name
// nothing else: TestWebAdminFormCoversSchema checks both directions, so adding
// a field to the agent's schema fails the test until someone places it in a
// group here.

// Field is one control of the editor's form.
type Field struct {
	Name  string `json:"name"`  // schema property name, e.g. "clue_min2"
	Label string `json:"label"` // short Russian label for the control
	Hint  string `json:"hint"`  // the schema's own description
	Type  string `json:"type"`  // "text" | "textarea" | "html" | "int" | "bool" | "codes" | "bonus_codes" | "fake_codes" | "spoilers" | "strings"
}

// Group is one titled block of controls.
type Group struct {
	Title  string  `json:"title"`
	Fields []Field `json:"fields"`
}

// Spec is the whole form.
type Spec struct {
	Groups []Group `json:"groups"`
}

// layout is the hand-written half: the order of the groups, and the
// property names and labels inside them.
type layout struct {
	title  string
	fields [][2]string // {property name, Russian label}
}

// webLongFormFields are the parameters an author writes paragraphs into, and
// which therefore deserve a text area rather than a single-line input. Every
// other string is a short value — a name, a date, a coordinate.
var webLongFormFields = map[string]bool{
	"question":       true,
	"legend":         true,
	"anons":          true,
	"legend_comment": true,
	"hint1":          true,
	"hint2":          true,
	"comment":        true,
	"greeting":       true,
}

// webHTMLFields are the long fields the engine renders as HTML to the players,
// so the editor offers a formatting toolbar over them instead of a bare box.
// The rest of the long fields stay plain: an organizer's note is read by the
// organizer, and markup there would only get in the way.
//
// The markup itself is never rewritten on its own — the control keeps a source
// view and the engine stores what it is given — so a field listed here only
// gains a toolbar, it does not change what reaches the engine.
var webHTMLFields = map[string]bool{
	"question": true,
	"hint1":    true,
	"hint2":    true,
	"legend":   true,
	"anons":    true,
	"greeting": true,
}

// webArrayFieldTypes name the arrays the editor has a dedicated row editor
// for; any other array is a plain list of strings.
var webArrayFieldTypes = map[string]string{
	"codes":       "codes",
	"bonus_codes": "bonus_codes",
	"fake_codes":  "fake_codes",
	"spoilers":    "spoilers",
}

// FieldType maps a schema property onto the control the browser draws.
func FieldType(name string, prop map[string]any) string {
	switch prop["type"] {
	case "integer":
		return "int"
	case "boolean":
		return "bool"
	case "array":
		if t, ok := webArrayFieldTypes[name]; ok {
			return t
		}
		return "strings"
	}
	if webHTMLFields[name] {
		return "html"
	}
	if webLongFormFields[name] {
		return "textarea"
	}
	return "text"
}

// buildWebFormSpec fills the layout from the schema. A property the layout
// names but the schema does not have keeps an empty hint rather than being
// dropped, so the completeness test can point at it by name.
func buildWebFormSpec(schema map[string]any, layout []layout) Spec {
	props, _ := schema["properties"].(map[string]any)
	spec := Spec{Groups: make([]Group, 0, len(layout))}
	for _, group := range layout {
		out := Group{Title: group.title, Fields: make([]Field, 0, len(group.fields))}
		for _, f := range group.fields {
			field := Field{Name: f[0], Label: f[1], Type: "text"}
			if prop, ok := props[f[0]].(map[string]any); ok {
				field.Hint, _ = prop["description"].(string)
				field.Type = FieldType(f[0], prop)
			}
			out.Fields = append(out.Fields, field)
		}
		spec.Groups = append(spec.Groups, out)
	}
	return spec
}

// Game lays the game parameters out the way an organizer fills them
// in: first what the players see in the announcement, then the texts, then the
// timings, and last the switches that put the game on the site.
func Game() Spec {
	return buildWebFormSpec(agenttools.GameParamsSchema(), []layout{
		{title: "Игра", fields: [][2]string{
			{"name", "Название"},
			{"number", "Номер"},
			{"date", "Дата"},
			{"time", "Время"},
			{"author", "Автор"},
			{"league", "Лига"},
			{"other_league", "Могут играть команды другой лиги"},
			{"zone", "Зона: творческая или на скорость"},
			{"price", "Стоимость участия"},
			{"breaf_place", "Время и место сбора перед игрой"},
		}},
		{title: "Тексты", fields: [][2]string{
			{"legend", "Легенда"},
			{"legend_comment", "Комментарий к легенде"},
			{"anons", "Дополнительная информация по игре"},
			{"greeting", "Приветственное сообщение"},
		}},
		{title: "Подсказки и штрафы", fields: [][2]string{
			{"clue_min", "Время до подсказки"},
			{"clue_min2", "Время до второй подсказки"},
			{"clue_min3", "Время до окончания задания"},
			{"clue_before_shtraf", "Штраф за досрочное взятие подсказки"},
			{"master_code", "Универсальный код"},
			{"master_code_shtraf", "Штраф за универсальный код"},
			{"penalty", "Штраф за непройденный уровень"},
		}},
		{title: "Расписание", fields: [][2]string{
			{"duration", "Продолжительность игры, ч"},
			{"bonus_after", "Бонусные коды после всех основных, мин"},
		}},
		{title: "Публикация", fields: [][2]string{
			{"publish", "Опубликовать на сайте"},
			{"invitation", "Открыть приём заявок"},
			{"finished", "Игра завершена"},
		}},
		{title: "Служебное", fields: [][2]string{
			{"clear", "Очистить поля"},
		}},
	})
}

// Level lays the level parameters out the way an author writes a
// level: the task, then the hints, then the codes, then the extras, then the
// switches that decide what kind of level it is.
func Level() Spec {
	return buildWebFormSpec(agenttools.LevelParamsSchema(), []layout{
		{title: "Задание", fields: [][2]string{
			{"title", "Название"},
			{"subtitle", "Название для выбора уровня командой"},
			{"question", "Текст задания"},
			{"location", "Расположение локации и кодов"},
			{"location_comment", "Примечания к заданию"},
			{"comment", "Комментарии (после игры)"},
		}},
		{title: "Подсказки", fields: [][2]string{
			{"hint1", "Подсказка 1"},
			{"hint2", "Подсказка 2"},
			{"clue_min", "Интервал до первой подсказки"},
			{"clue_min2", "Интервал до второй подсказки"},
			{"clue_min3", "Интервал до окончания уровня"},
			{"hint1_on_request", "Подсказку 1 выдавать по запросу"},
			{"hint1_interval", "Подсказку 1 не ранее чем через, мин"},
			{"hint1_penalty", "Штраф за подсказку 1"},
			{"hint2_on_request", "Подсказку 2 выдавать по запросу"},
			{"hint2_interval", "Подсказку 2 не ранее чем через, мин"},
			{"hint2_penalty", "Штраф за подсказку 2"},
			{"clue_before", "Разрешить брать подсказки досрочно"},
		}},
		{title: "Коды", fields: [][2]string{
			{"codes", "Основные коды"},
			{"sector_names", "Имена секторов"},
			{"code_count", "Кодов для выполнения задания"},
			{"try_limit", "Количество попыток ввода кода"},
			{"no_master_code", "Запретить универсальный код игры"},
		}},
		{title: "Бонусы и штрафы", fields: [][2]string{
			{"bonus_codes", "Бонусные коды"},
			{"fake_codes", "Ложные коды"},
			{"time_add_bonus_all", "Бонусное время за все бонусные коды"},
			{"penalty", "Штраф за непройденный уровень"},
			{"spoilers", "Спойлеры"},
		}},
		{title: "Режим уровня", fields: [][2]string{
			{"bonus", "Бонусный уровень"},
			{"bonus_time", "Время бонусного уровня, мин"},
			{"skvoz", "Сквозной бонусный уровень"},
			{"skvoz_min", "Ограничить время выполнения, мин"},
			{"skvoz_start", "Выдать задание в"},
			{"is_sabotage", "Задание типа «Саботаж»"},
			{"no_break", "Запретить перерыв после задания"},
			{"reserve", "Запасной уровень"},
			{"publish", "Активен"},
		}},
		{title: "Координаты локации", fields: [][2]string{
			{"lat", "Широта"},
			{"lon", "Долгота"},
			{"radius", "Радиус локации"},
			{"show_coord_with_hint2", "Показывать координаты со второй подсказкой"},
		}},
		{title: "Служебное", fields: [][2]string{
			{"clear", "Очистить поля"},
		}},
	})
}

// Clearable names the parameters the engine stores as explicitly
// blank when they are listed in params.clear. The browser needs the list
// because an emptied text box is not an absent field: the engine reads an
// absent one as «не трогать», so clearing a text has to travel as a name in
// clear rather than as an empty value.
//
// The list is filtered out of the schema's own property names through the
// dzzzr predicate, so it can neither name a parameter the schema lost nor miss
// one the schema gained.
func Clearable(schema map[string]any, textParam func(string) bool) []string {
	props, _ := schema["properties"].(map[string]any)
	out := make([]string, 0, len(props))
	for name := range props {
		if textParam(name) {
			out = append(out, name)
		}
	}
	slices.Sort(out) // Map iteration is random; the browser gets a stable order.
	return out
}

// ValidateGame runs the checks a game can pass without the engine:
// the same windows-1251 encodability gamesource requires of a scenario, and
// the date and time formats the administration area expects. Everything else
// about a game — the league, the zone, the numbering — only the engine knows.
func ValidateGame(p dzzzr.GameParams) []string {
	problems := []string{}
	encodable := func(s string) bool {
		_, err := charmap.Windows1251.NewEncoder().String(s)
		return err == nil
	}
	v := reflect.ValueOf(p)
	for i := range v.NumField() {
		name, _, _ := strings.Cut(v.Type().Field(i).Tag.Get("json"), ",")
		field := v.Field(i)
		switch field.Kind() {
		case reflect.String:
			if !encodable(field.String()) {
				problems = append(problems, fmt.Sprintf("%s не кодируется в windows-1251: движок хранит только эту кодировку", name))
			}
		case reflect.Slice:
			for j := range field.Len() {
				if item := field.Index(j); item.Kind() == reflect.String && !encodable(item.String()) {
					problems = append(problems, fmt.Sprintf("%s[%d] не кодируется в windows-1251: движок хранит только эту кодировку", name, j))
				}
			}
		}
	}
	// gamesource lets a scenario carry the engine's own placeholder date.
	if p.Date != "" && p.Date != "00.00.0000" {
		if _, err := time.Parse("02.01.2006", p.Date); err != nil {
			problems = append(problems, "date: дата игры должна быть в формате DD.MM.YYYY")
		}
	}
	if p.Time != "" {
		if _, err := time.Parse("15:04", p.Time); err != nil {
			problems = append(problems, "time: время игры должно быть в формате HH:MM")
		}
	}
	return problems
}
