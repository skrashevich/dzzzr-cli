package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// TestWebAdminFormCoversSchema is the reason this form exists: the editor and
// the agent must describe the same game and the same level. It fails in both
// directions — a parameter the agent gained and nobody placed in a group, and
// a group naming a parameter the agent no longer has.
func TestWebAdminFormCoversSchema(t *testing.T) {
	for _, tc := range []struct {
		kind   string
		schema map[string]any
		spec   webFormSpec
	}{
		{"game", agenttools.GameParamsSchema(), webGameFormSpec()},
		{"level", agenttools.LevelParamsSchema(), webLevelFormSpec()},
	} {
		props, ok := tc.schema["properties"].(map[string]any)
		if !ok || len(props) == 0 {
			t.Fatalf("%s: в схеме нет properties", tc.kind)
		}
		placed := map[string]string{}
		for _, group := range tc.spec.Groups {
			if group.Title == "" {
				t.Errorf("%s: группа без заголовка", tc.kind)
			}
			if len(group.Fields) == 0 {
				t.Errorf("%s: пустая группа %q", tc.kind, group.Title)
			}
			for _, field := range group.Fields {
				if where, dup := placed[field.Name]; dup {
					t.Errorf("%s: поле %q встречается и в группе %q, и в группе %q", tc.kind, field.Name, where, group.Title)
					continue
				}
				placed[field.Name] = group.Title

				prop, ok := props[field.Name].(map[string]any)
				if !ok {
					t.Errorf("%s: группа %q называет несуществующий параметр %q", tc.kind, group.Title, field.Name)
					continue
				}
				if field.Label == "" {
					t.Errorf("%s: поле %q без подписи", tc.kind, field.Name)
				}
				// The help text must be the schema's own words, so the author
				// and the agent read one description of the field.
				if want, _ := prop["description"].(string); field.Hint != want {
					t.Errorf("%s: подсказка поля %q = %q, в схеме %q", tc.kind, field.Name, field.Hint, want)
				}
				if field.Type != webFieldType(field.Name, prop) {
					t.Errorf("%s: тип поля %q = %q", tc.kind, field.Name, field.Type)
				}
			}
		}
		for name := range props {
			if _, ok := placed[name]; !ok {
				t.Errorf("%s: параметр %q не размещён ни в одной группе формы", tc.kind, name)
			}
		}
	}
}

// TestWebAdminFormFieldTypes pins the mapping the browser relies on to pick a
// control.
func TestWebAdminFormFieldTypes(t *testing.T) {
	want := map[string]string{
		"title": "text",
		// The engine renders задание and подсказки to the players as HTML, so
		// the editor puts a formatting toolbar over them; комментарий
		// организатора is read by the organizer alone and stays a plain box.
		"question":     "html",
		"hint1":        "html",
		"hint2":        "html",
		"comment":      "textarea",
		"clue_min2":    "int",
		"publish":      "bool",
		"codes":        "codes",
		"bonus_codes":  "bonus_codes",
		"fake_codes":   "fake_codes",
		"spoilers":     "spoilers",
		"sector_names": "strings",
		"clear":        "strings",
	}
	got := map[string]string{}
	for _, group := range webLevelFormSpec().Groups {
		for _, field := range group.Fields {
			got[field.Name] = field.Type
		}
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("тип поля %q = %q, ожидался %q", name, got[name], kind)
		}
	}
}

// adminSchemaHalf is one half of the answer of GET /api/v1/admin/schema.
type adminSchemaHalf struct {
	Form      webFormSpec    `json:"form"`
	Schema    map[string]any `json:"schema"`
	Clearable []string       `json:"clearable"`
}

func TestWebAdminSchemaEndpoint(t *testing.T) {
	// The empty state is drawn before anyone signs in, so the description must
	// be served without organizer credentials.
	_, srv := editing(t, false)

	var out struct {
		Game  adminSchemaHalf `json:"game"`
		Level adminSchemaHalf `json:"level"`
		Error string          `json:"error"`
	}
	if code := webDo(t, srv, http.MethodGet, "/api/v1/admin/schema", "", &out); code != http.StatusOK {
		t.Fatalf("status = %d, error = %q", code, out.Error)
	}
	for name, part := range map[string]struct {
		half      adminSchemaHalf
		textParam func(string) bool
	}{
		"game":  {out.Game, dzzzr.GameTextParam},
		"level": {out.Level, dzzzr.LevelTextParam},
	} {
		if len(part.half.Form.Groups) == 0 {
			t.Fatalf("%s: форма без групп", name)
		}
		for _, group := range part.half.Form.Groups {
			if len(group.Fields) == 0 {
				t.Errorf("%s: группа %q пуста", name, group.Title)
			}
		}
		props, ok := part.half.Schema["properties"].(map[string]any)
		if !ok || len(props) == 0 {
			t.Fatalf("%s: в ответе нет схемы параметров", name)
		}

		// Both directions again: the browser turns an emptied text box into an
		// entry of params.clear, so the list must name every text parameter of
		// the schema and nothing that is not one.
		listed := map[string]bool{}
		for _, param := range part.half.Clearable {
			if _, ok := props[param]; !ok {
				t.Errorf("%s: в clearable параметр %q, которого нет в схеме", name, param)
			}
			if !part.textParam(param) {
				t.Errorf("%s: в clearable параметр %q, который движок не хранит как текст", name, param)
			}
			listed[param] = true
		}
		for param := range props {
			if part.textParam(param) && !listed[param] {
				t.Errorf("%s: текстовый параметр %q не попал в clearable", name, param)
			}
		}
		if len(listed) == 0 {
			t.Errorf("%s: clearable пуст", name)
		}
	}
}

func TestWebAdminValidateLevel(t *testing.T) {
	_, srv := editing(t, false)

	for _, tc := range []struct {
		name string
		body string
		ok   bool
		want string
	}{
		{
			name: "два одинаковых основных кода",
			body: `{"kind":"level","params":{"title":"Уровень","question":"Текст","codes":[{"code":"КОД","danger":"1"},{"code":"код","danger":"1"}]}}`,
			want: `duplicate code or synonym "код" in codes[0] and codes[1]`,
		},
		{
			name: "публикация без задания",
			body: `{"kind":"level","params":{"title":"Уровень","hint1":"Раз","hint2":"Два","publish":true,"codes":[{"code":"КОД","danger":"1"}]}}`,
			want: "publish requires title, question, both hints and a main code",
		},
		{
			name: "годный уровень",
			body: `{"kind":"level","params":{"title":"Уровень","question":"Текст","hint1":"Раз","hint2":"Два","publish":true,"codes":[{"code":"КОД","danger":"1"}]}}`,
			ok:   true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out struct {
				OK     bool     `json:"ok"`
				Errors []string `json:"errors"`
				Error  string   `json:"error"`
			}
			if code := webDo(t, srv, http.MethodPost, "/api/v1/admin/validate", tc.body, &out); code != http.StatusOK {
				t.Fatalf("status = %d, error = %q", code, out.Error)
			}
			if out.OK != tc.ok {
				t.Fatalf("ok = %v, ошибки = %q", out.OK, out.Errors)
			}
			if tc.ok {
				if len(out.Errors) != 0 {
					t.Fatalf("годный уровень с ошибками: %q", out.Errors)
				}
				return
			}
			found := false
			for _, e := range out.Errors {
				if strings.Contains(e, tc.want) {
					found = true
				}
				if strings.HasPrefix(e, "levels[") {
					t.Errorf("в сообщении остался номер уровня: %q", e)
				}
			}
			if !found {
				t.Fatalf("среди ошибок %q нет %q", out.Errors, tc.want)
			}
		})
	}
}

func TestWebAdminValidateGame(t *testing.T) {
	_, srv := editing(t, false)

	var out struct {
		OK     bool     `json:"ok"`
		Errors []string `json:"errors"`
		Error  string   `json:"error"`
	}
	bad := `{"kind":"game","params":{"name":"Игра","date":"2026-09-22","time":"вечером"}}`
	if code := webDo(t, srv, http.MethodPost, "/api/v1/admin/validate", bad, &out); code != http.StatusOK {
		t.Fatalf("status = %d, error = %q", code, out.Error)
	}
	if out.OK {
		t.Fatal("игра с неверными датой и временем принята")
	}
	joined := strings.Join(out.Errors, "\n")
	for _, want := range []string{"DD.MM.YYYY", "HH:MM"} {
		if !strings.Contains(joined, want) {
			t.Errorf("среди ошибок %q нет упоминания %q", out.Errors, want)
		}
	}

	good := `{"kind":"game","params":{"name":"Игра","date":"22.09.2026","time":"21:00"}}`
	if code := webDo(t, srv, http.MethodPost, "/api/v1/admin/validate", good, &out); code != http.StatusOK {
		t.Fatalf("status = %d, error = %q", code, out.Error)
	}
	if !out.OK {
		t.Fatalf("годная игра отклонена: %q", out.Errors)
	}
}

func TestWebAdminValidateRejectsBadRequests(t *testing.T) {
	_, srv := editing(t, false)

	for _, tc := range []struct {
		name string
		body string
	}{
		{"неизвестный вид", `{"kind":"scenario","params":{"title":"x"}}`},
		{"вид не указан", `{"params":{"title":"x"}}`},
		{"опечатка в параметре уровня", `{"kind":"level","params":{"titel":"x"}}`},
		{"опечатка в параметре игры", `{"kind":"game","params":{"naem":"x"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out struct {
				Error string `json:"error"`
			}
			if code := webDo(t, srv, http.MethodPost, "/api/v1/admin/validate", tc.body, &out); code != http.StatusBadRequest {
				t.Fatalf("status = %d, error = %q", code, out.Error)
			}
			if out.Error == "" {
				t.Fatal("отказ без объяснения")
			}
		})
	}
}
