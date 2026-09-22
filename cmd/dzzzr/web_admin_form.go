package main

import (
	"encoding/json/jsontext"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
	"github.com/skrashevich/dzzzr-cli/gamesource"
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

// webFormField is one control of the editor's form.
type webFormField struct {
	Name  string `json:"name"`  // schema property name, e.g. "clue_min2"
	Label string `json:"label"` // short Russian label for the control
	Hint  string `json:"hint"`  // the schema's own description
	Type  string `json:"type"`  // "text" | "textarea" | "html" | "int" | "bool" | "codes" | "bonus_codes" | "fake_codes" | "spoilers" | "strings"
}

// webFormGroup is one titled block of controls.
type webFormGroup struct {
	Title  string         `json:"title"`
	Fields []webFormField `json:"fields"`
}

// webFormSpec is the whole form.
type webFormSpec struct {
	Groups []webFormGroup `json:"groups"`
}

// webFormLayout is the hand-written half: the order of the groups, and the
// property names and labels inside them.
type webFormLayout struct {
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

// webFieldType maps a schema property onto the control the browser draws.
func webFieldType(name string, prop map[string]any) string {
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
func buildWebFormSpec(schema map[string]any, layout []webFormLayout) webFormSpec {
	props, _ := schema["properties"].(map[string]any)
	spec := webFormSpec{Groups: make([]webFormGroup, 0, len(layout))}
	for _, group := range layout {
		out := webFormGroup{Title: group.title, Fields: make([]webFormField, 0, len(group.fields))}
		for _, f := range group.fields {
			field := webFormField{Name: f[0], Label: f[1], Type: "text"}
			if prop, ok := props[f[0]].(map[string]any); ok {
				field.Hint, _ = prop["description"].(string)
				field.Type = webFieldType(f[0], prop)
			}
			out.Fields = append(out.Fields, field)
		}
		spec.Groups = append(spec.Groups, out)
	}
	return spec
}

// webGameFormSpec lays the game parameters out the way an organizer fills them
// in: first what the players see in the announcement, then the texts, then the
// timings, and last the switches that put the game on the site.
func webGameFormSpec() webFormSpec {
	return buildWebFormSpec(agenttools.GameParamsSchema(), []webFormLayout{
		{title: "Игра", fields: [][2]string{
			{"name", "Название"},
			{"number", "Номер"},
			{"date", "Дата"},
			{"time", "Время"},
			{"author", "Автор"},
			{"league", "Лига"},
			{"other_league", "Другая лига"},
			{"zone", "Зона"},
			{"price", "Стоимость"},
			{"breaf_place", "Место брифинга"},
		}},
		{title: "Тексты", fields: [][2]string{
			{"legend", "Легенда"},
			{"legend_comment", "Комментарий к легенде"},
			{"anons", "Анонс"},
			{"greeting", "Приветствие"},
		}},
		{title: "Подсказки и штрафы", fields: [][2]string{
			{"clue_min", "Первая подсказка"},
			{"clue_min2", "Вторая подсказка"},
			{"clue_min3", "Третья подсказка"},
			{"clue_before_shtraf", "Штраф за досрочную подсказку"},
			{"master_code", "Мастер-код"},
			{"master_code_shtraf", "Штраф за мастер-код"},
			{"penalty", "Штраф"},
		}},
		{title: "Расписание", fields: [][2]string{
			{"duration", "Продолжительность"},
			{"bonus_after", "Бонус после"},
		}},
		{title: "Публикация", fields: [][2]string{
			{"publish", "Опубликовать"},
			{"invitation", "Приём заявок"},
			{"finished", "Игра завершена"},
		}},
		{title: "Служебное", fields: [][2]string{
			{"clear", "Очистить поля"},
		}},
	})
}

// webLevelFormSpec lays the level parameters out the way an author writes a
// level: the task, then the hints, then the codes, then the extras, then the
// switches that decide what kind of level it is.
func webLevelFormSpec() webFormSpec {
	return buildWebFormSpec(agenttools.LevelParamsSchema(), []webFormLayout{
		{title: "Задание", fields: [][2]string{
			{"title", "Название"},
			{"subtitle", "Подзаголовок"},
			{"question", "Задание"},
			{"location", "Место"},
			{"location_comment", "Комментарий к месту"},
			{"comment", "Комментарий организатора"},
		}},
		{title: "Подсказки", fields: [][2]string{
			{"hint1", "Первая подсказка"},
			{"hint2", "Вторая подсказка"},
			{"clue_min", "Интервал первой подсказки"},
			{"clue_min2", "Интервал второй подсказки"},
			{"clue_min3", "Интервал третьей подсказки"},
			{"hint1_on_request", "Первая по запросу"},
			{"hint1_interval", "Интервал первой по запросу"},
			{"hint1_penalty", "Штраф первой подсказки"},
			{"hint2_on_request", "Вторая по запросу"},
			{"hint2_interval", "Интервал второй по запросу"},
			{"hint2_penalty", "Штраф второй подсказки"},
			{"clue_before", "Досрочные подсказки"},
		}},
		{title: "Коды", fields: [][2]string{
			{"codes", "Основные коды"},
			{"sector_names", "Названия секторов"},
			{"code_count", "Кодов для прохождения"},
			{"try_limit", "Лимит попыток"},
			{"no_master_code", "Запретить мастер-код"},
		}},
		{title: "Бонусы и штрафы", fields: [][2]string{
			{"bonus_codes", "Бонусные коды"},
			{"fake_codes", "Ложные коды"},
			{"time_add_bonus_all", "Бонус за все бонусные коды"},
			{"penalty", "Штраф"},
			{"spoilers", "Спойлеры"},
		}},
		{title: "Режим уровня", fields: [][2]string{
			{"bonus", "Бонусный уровень"},
			{"bonus_time", "Бонусное время"},
			{"skvoz", "Сквозной уровень"},
			{"skvoz_min", "Время сквозного уровня"},
			{"skvoz_start", "Начало сквозного уровня"},
			{"is_sabotage", "Уровень-диверсия"},
			{"no_break", "Запретить перерыв"},
			{"reserve", "Запасной уровень"},
			{"publish", "Опубликовать"},
		}},
		{title: "Координаты", fields: [][2]string{
			{"lat", "Широта"},
			{"lon", "Долгота"},
			{"radius", "Радиус"},
			{"show_coord_with_hint2", "Координаты со второй подсказкой"},
		}},
		{title: "Служебное", fields: [][2]string{
			{"clear", "Очистить поля"},
		}},
	})
}

// webClearableParams names the parameters the engine stores as explicitly
// blank when they are listed in params.clear. The browser needs the list
// because an emptied text box is not an absent field: the engine reads an
// absent one as «не трогать», so clearing a text has to travel as a name in
// clear rather than as an empty value.
//
// The list is filtered out of the schema's own property names through the
// dzzzr predicate, so it can neither name a parameter the schema lost nor miss
// one the schema gained.
func webClearableParams(schema map[string]any, textParam func(string) bool) []string {
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

// httpAdminSchema serves the form layout together with the raw schema. It asks
// for no organizer credentials on purpose: this is a static description of the
// parameters, and the editor needs it to draw its empty state before anyone
// has signed in.
func (h *webHub) httpAdminSchema(w http.ResponseWriter, r *http.Request) {
	game, level := agenttools.GameParamsSchema(), agenttools.LevelParamsSchema()
	webWriteJSON(w, http.StatusOK, map[string]any{
		"game": map[string]any{
			"form":      webGameFormSpec(),
			"schema":    game,
			"clearable": webClearableParams(game, dzzzr.GameTextParam),
		},
		"level": map[string]any{
			"form":      webLevelFormSpec(),
			"schema":    level,
			"clearable": webClearableParams(level, dzzzr.LevelTextParam),
		},
	})
}

// adminValidateBody is the body of POST /api/v1/admin/validate.
type adminValidateBody struct {
	Kind   string         `json:"kind"`
	Params jsontext.Value `json:"params"`
}

// httpAdminValidate checks parameters without touching the engine, so the
// editor can tell an author about a duplicate code while they are still typing
// instead of after a refused write.
func (h *webHub) httpAdminValidate(w http.ResponseWriter, r *http.Request) {
	var body adminValidateBody
	if !webReadJSON(w, r, &body) {
		return
	}
	switch body.Kind {
	case "level":
		var params dzzzr.LevelParams
		if !decodeParams(w, body.Params, &params) {
			return
		}
		// A one-level create plan is the exported way into gamesource's level
		// checks, and it runs exactly what «dzzzr admin-validate-source» runs.
		plan := gamesource.Plan{GameID: 1, Mode: "create", Levels: []gamesource.Level{{Params: params}}}
		webWriteValidation(w, webValidationProblems(plan.Validate()))
	case "game":
		var params dzzzr.GameParams
		if !decodeParams(w, body.Params, &params) {
			return
		}
		webWriteValidation(w, webValidateGameParams(params))
	default:
		webError(w, http.StatusBadRequest, "неизвестный вид проверки %q: ожидается game или level", body.Kind)
	}
}

// webWriteValidation answers with the verdict and every problem separately: an
// author fixing three things wants to see three lines.
func webWriteValidation(w http.ResponseWriter, problems []string) {
	webWriteJSON(w, http.StatusOK, map[string]any{"ok": len(problems) == 0, "errors": problems})
}

// webValidationProblems splits what gamesource reports. Plan.Validate joins its
// findings with errors.Join, which renders one error per line, so the newline
// is the joiner to split on. The texts themselves are passed through word for
// word: they are what the command-line validator prints, and a translation
// table here would be a second source of truth that drifts silently.
func webValidationProblems(err error) []string {
	problems := []string{}
	if err == nil {
		return problems
	}
	for _, line := range strings.Split(err.Error(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// The plan always holds exactly one level, so its index is noise.
		problems = append(problems, strings.TrimPrefix(line, "levels[0]: "))
	}
	return problems
}

// webValidateGameParams runs the checks a game can pass without the engine:
// the same windows-1251 encodability gamesource requires of a scenario, and
// the date and time formats the administration area expects. Everything else
// about a game — the league, the zone, the numbering — only the engine knows.
func webValidateGameParams(p dzzzr.GameParams) []string {
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
