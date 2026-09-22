package main

import (
	"encoding/json/jsontext"
	"net/http"
	"testing"
)

func TestWebAdminListLevels(t *testing.T) {
	_, srv := editing(t, true)

	var out struct {
		Levels []webAdminLevel `json:"levels"`
		Error  string          `json:"error"`
	}
	if code := webDo(t, srv, http.MethodGet, "/api/v1/admin/games/4242/levels", "", &out); code != http.StatusOK {
		t.Fatalf("status = %d, error = %q", code, out.Error)
	}
	if len(out.Levels) != 3 {
		t.Fatalf("уровней = %d: %+v", len(out.Levels), out.Levels)
	}
	for _, l := range out.Levels {
		if l.ID <= 0 || l.Order <= 0 {
			t.Fatalf("уровень без идентификатора или позиции: %+v", l)
		}
	}
	l0 := out.Levels[0]
	if l0.ID != 901 || l0.Order != 1 || l0.Title != "Гаражи" || l0.Kind != "основной" || !l0.Published {
		t.Errorf("levels[0] = %+v", l0)
	}
	if len(l0.Codes) != 3 || l0.CodeCount != "все" || l0.HintTimings != "30:30:30" {
		t.Errorf("levels[0] коды и счётчики = %v %q %q", l0.Codes, l0.CodeCount, l0.HintTimings)
	}
	if len(l0.BonusCodes) != 1 || l0.BonusCodes[0] != "B1 (10 мин.)" {
		t.Errorf("levels[0].BonusCodes = %v", l0.BonusCodes)
	}
	l1 := out.Levels[1]
	if l1.ID != 902 || l1.Order != 2 || l1.Title != "Пустырь" || l1.TryLimit != "4" {
		t.Errorf("levels[1] = %+v", l1)
	}
	if len(l1.FakeCodes) != 1 || l1.FakeCodes[0] != "FAKE (10 мин.)" {
		t.Errorf("levels[1].FakeCodes = %v", l1.FakeCodes)
	}
	if l2 := out.Levels[2]; l2.ID != 903 || l2.Order != 3 || l2.Kind != "бонусный" || !l2.Skvoz || l2.Published {
		t.Errorf("levels[2] = %+v", l2)
	}
}

// TestWebAdminLevelRoundTrip sends the params of a GET straight back through
// PATCH. The editor's one hard promise is exactly this: what it shows is what
// it may return, unchanged, and the engine must recognize every field of it.
func TestWebAdminLevelRoundTrip(t *testing.T) {
	engineDouble, srv := editing(t, true)

	var got struct {
		ID     int            `json:"id"`
		GameID int            `json:"game_id"`
		Order  int            `json:"order"`
		Params jsontext.Value `json:"params"`
		Error  string         `json:"error"`
	}
	if code := webDo(t, srv, http.MethodGet, "/api/v1/admin/games/4242/levels/901", "", &got); code != http.StatusOK {
		t.Fatalf("чтение уровня = %d, error = %q", code, got.Error)
	}
	if got.ID != 901 || got.GameID != 4242 || got.Order != 1 {
		t.Fatalf("идентификаторы = %+v", got)
	}
	if len(got.Params) == 0 {
		t.Fatal("уровень без params")
	}

	var saved struct {
		ID     int    `json:"id"`
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	body := `{"params":` + string(got.Params) + `}`
	if code := webDo(t, srv, http.MethodPatch, "/api/v1/admin/games/4242/levels/901", body, &saved); code != http.StatusOK {
		t.Fatalf("сохранение = %d, error = %q", code, saved.Error)
	}
	if saved.ID != 901 || saved.Status != "saved" {
		t.Fatalf("ответ на сохранение = %+v", saved)
	}
	f := engineDouble.lastPost()
	if f.Get("action") != "update_zadanie" || f.Get("id") != "901" || f.Get("categoryValue") != "4242" {
		t.Fatalf("форма = %v", f)
	}
	// The round trip must carry the level's own values, not blanks.
	checks := map[string]string{
		"title": "Гаражи", "clue1": "Подсказка 1 к уровню 1", "order_p": "1",
		"code[0]": "1", "codeS[0]": "ОДИН#ONE", "danger[0]": "1", "sector[0]": "1",
		"secName[1]": "Гаражи", "codeB[0]": "B1", "timeB[0]": "10",
		"spoiler[1]": "Скрытая подсказка", "spoilerCode[1]": "SP1",
	}
	for k, want := range checks {
		if v := f.Get(k); v != want {
			t.Errorf("форма[%q] = %q, ожидалось %q", k, v, want)
		}
	}
}

// TestWebAdminCreateLevelPostsEveryCode pins that nothing the browser sends is
// dropped on the way to the engine: a code list, sector names, bonus and fake
// codes and spoilers all reach it under the engine's own indexed names.
func TestWebAdminCreateLevelPostsEveryCode(t *testing.T) {
	engineDouble, srv := editing(t, true)

	body := `{"params":{
		"title":"Новый","question":"Вопрос","hint1":"П1","hint2":"П2",
		"codes":[{"code":"A","synonyms":"АА","danger":"1","sector":1},{"code":"B","danger":"2"}],
		"sector_names":["Первый"],
		"bonus_codes":[{"code":"BB","minutes":5}],
		"fake_codes":[{"code":"FF","penalty":7}],
		"spoilers":[{"text":"Спойлер","code":"SC","penalty":3}],
		"code_count":1,"clue_min":25}}`
	var created struct {
		ID    int    `json:"id"`
		Error string `json:"error"`
	}
	if code := webDo(t, srv, http.MethodPost, "/api/v1/admin/games/4242/levels", body, &created); code != http.StatusCreated {
		t.Fatalf("создание = %d, error = %q", code, created.Error)
	}
	if created.ID != 777 {
		t.Fatalf("id = %d", created.ID)
	}
	f := engineDouble.lastPost()
	checks := map[string]string{
		"action": "add_zadanie", "categoryValue": "4242", "category": "4242",
		"title": "Новый", "question": "Вопрос", "clue1": "П1", "clue2": "П2",
		"code[0]": "A", "codeS[0]": "АА", "danger[0]": "1", "sector[0]": "1",
		"code[1]": "B", "danger[1]": "2",
		"secName[1]": "Первый",
		"codeB[0]":   "BB", "timeB[0]": "5",
		"codeF[0]": "FF", "fakeShtraf[0]": "7",
		"spoiler[1]": "Спойлер", "spoilerCode[1]": "SC", "spoilerPenalty[1]": "3",
		"codeCount": "1", "ClueMin": "25",
	}
	for k, want := range checks {
		if v := f.Get(k); v != want {
			t.Errorf("форма[%q] = %q, ожидалось %q", k, v, want)
		}
	}
}

func TestWebAdminRejectsUnknownLevelParam(t *testing.T) {
	_, srv := editing(t, true)

	var out struct {
		Error string `json:"error"`
	}
	code := webDo(t, srv, http.MethodPatch, "/api/v1/admin/games/4242/levels/901", `{"params":{"titel":"опечатка"}}`, &out)
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, error = %q", code, out.Error)
	}
	if out.Error == "" {
		t.Fatal("отказ без объяснения")
	}
}

// moveAnswer is what the move endpoint reports, since the engine's own
// redirect says nothing about whether anything moved.
type moveAnswer struct {
	OrderBefore int    `json:"order_before"`
	OrderAfter  int    `json:"order_after"`
	Moved       bool   `json:"moved"`
	Error       string `json:"error"`
}

// TestWebAdminMoveLevel covers the one thing the engine will not say: whether
// the move happened. The double reorders and renumbers the list it serves, so
// the handler sees the level at its new position the way it would live.
func TestWebAdminMoveLevel(t *testing.T) {
	engineDouble, srv := editing(t, true)

	var out moveAnswer
	code := webDo(t, srv, http.MethodPost, "/api/v1/admin/games/4242/levels/902/move", `{"up":true}`, &out)
	if code != http.StatusOK {
		t.Fatalf("перемещение = %d, error = %q", code, out.Error)
	}
	if out.OrderBefore != 2 || out.OrderAfter != 1 || !out.Moved {
		t.Fatalf("ответ = %+v, ожидалось 2 -> 1", out)
	}
	if f := engineDouble.lastPost(); f.Get("action") != "move_up" || f.Get("id") != "902" {
		t.Fatalf("форма = %v", f)
	}
}

// TestWebAdminMoveLevelAtEdge is why the handler reads the list twice: the
// engine answers a refused move with the same redirect as an accepted one, so
// only the position tells the two apart.
func TestWebAdminMoveLevelAtEdge(t *testing.T) {
	_, srv := editing(t, true)

	var out moveAnswer
	code := webDo(t, srv, http.MethodPost, "/api/v1/admin/games/4242/levels/901/move", `{"up":true}`, &out)
	if code != http.StatusOK {
		t.Fatalf("перемещение = %d, error = %q", code, out.Error)
	}
	if out.Moved || out.OrderBefore != 1 || out.OrderAfter != 1 {
		t.Fatalf("ответ = %+v, первый уровень не может подняться", out)
	}
}

func TestWebAdminMoveUnknownLevel(t *testing.T) {
	_, srv := editing(t, true)

	var out struct {
		Error string `json:"error"`
	}
	code := webDo(t, srv, http.MethodPost, "/api/v1/admin/games/4242/levels/909/move", `{"up":false}`, &out)
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, error = %q", code, out.Error)
	}
	if out.Error == "" {
		t.Fatal("отказ без объяснения")
	}
}

func TestWebAdminDeleteLevel(t *testing.T) {
	engineDouble, srv := editing(t, true)

	var out struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if code := webDo(t, srv, http.MethodDelete, "/api/v1/admin/games/4242/levels/903", "", &out); code != http.StatusOK {
		t.Fatalf("удаление = %d, error = %q", code, out.Error)
	}
	if out.Status != "deleted" {
		t.Fatalf("ответ = %+v", out)
	}
	if f := engineDouble.lastPost(); f.Get("action") != "del_zadanie" || f.Get("id") != "903" || f.Get("categoryValue") != "4242" {
		t.Fatalf("форма = %v", f)
	}
}

func TestWebAdminRejectsBadLevelID(t *testing.T) {
	_, srv := editing(t, true)

	var out struct {
		Error string `json:"error"`
	}
	for _, path := range []string{
		"/api/v1/admin/games/4242/levels/0",
		"/api/v1/admin/games/4242/levels/abc",
		"/api/v1/admin/games/4242/levels/-1",
	} {
		if code := webDo(t, srv, http.MethodGet, path, "", &out); code != http.StatusBadRequest {
			t.Errorf("GET %s: status = %d, ожидался 400", path, code)
		}
	}
}
