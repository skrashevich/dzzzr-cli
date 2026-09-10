package dzzzr_test

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func TestAdminListLevels(t *testing.T) {
	s := newAdminServer(t, map[string]string{"zadanie": "admin_levels_list.html"})
	levels, err := s.client.AdminListLevels(context.Background(), 4242)
	if err != nil {
		t.Fatal(err)
	}
	if q := s.gets[0]; q.Get("action") != "zadanie" || q.Get("categoryValue") != "4242" {
		t.Errorf("query = %v", q)
	}
	if len(levels) != 3 {
		t.Fatalf("levels = %+v", levels)
	}
	l0 := levels[0]
	if l0.ID != 901 || l0.Order != 1 || l0.Title != "Гаражи" || l0.Kind != "основной" || l0.Skvoz || !l0.Published || !l0.Selected {
		t.Errorf("levels[0] = %+v", l0)
	}
	if len(l0.Codes) != 3 || l0.Codes[0] != "1" || l0.Codes[2] != "3" {
		t.Errorf("levels[0].Codes = %v", l0.Codes)
	}
	if len(l0.BonusCodes) != 1 || l0.BonusCodes[0] != "B1 (10 мин.)" {
		t.Errorf("levels[0].BonusCodes = %v", l0.BonusCodes)
	}
	if l0.CodeCount != "все" || l0.HintTimings != "30:30:30" {
		t.Errorf("levels[0] counters = %q %q", l0.CodeCount, l0.HintTimings)
	}
	l1 := levels[1]
	if l1.ID != 902 || l1.Order != 2 || l1.Title != "Пустырь" || l1.CodeCount != "1" || l1.TryLimit != "4" {
		t.Errorf("levels[1] = %+v", l1)
	}
	if len(l1.FakeCodes) != 1 || l1.FakeCodes[0] != "FAKE (10 мин.)" {
		t.Errorf("levels[1].FakeCodes = %v", l1.FakeCodes)
	}
	l2 := levels[2]
	if l2.ID != 903 || l2.Order != 3 || l2.Kind != "бонусный" || !l2.Skvoz || l2.Published {
		t.Errorf("levels[2] = %+v", l2)
	}
}

// TestAdminListLevelsKeepsTheSelectedLevel covers a level the list used to
// lose outright. The engine prints "0." instead of a position for the level
// currently open in its editor, and a row without a number was treated as not
// being a level row at all — so admin-levels silently returned 50 of a game's
// 51 levels, and anything that looked the level up reported it missing.
// Measured against classic.dzzzr.ru on 2026-09-09 with level 23563 selected.
//
// The position comes from the editor's own order_p, which is exact even when
// the printed numbers have gaps; the neighbours are only the fallback.
func TestAdminListLevelsKeepsTheSelectedLevel(t *testing.T) {
	for _, tc := range []struct {
		name      string
		fixture   string
		wantOrder int
	}{
		// order_p wins: the neighbours would say 1.
		{"order_p", "admin_levels_selected_order_p.html", 42},
		// Without it, the position is derived from the next level.
		{"neighbours", "admin_levels_selected_no_order.html", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newAdminServer(t, map[string]string{"zadanie": tc.fixture})
			levels, err := s.client.AdminListLevels(context.Background(), 4242)
			if err != nil {
				t.Fatal(err)
			}
			if len(levels) != 3 {
				t.Fatalf("уровней = %d, ожидалось 3: %+v", len(levels), levels)
			}
			if l := levels[0]; l.ID != 901 || !l.Selected {
				t.Fatalf("levels[0] = %+v, ожидался выбранный 901", l)
			}
			if levels[0].Order != tc.wantOrder {
				t.Errorf("позиция выбранного = %d, ожидалась %d", levels[0].Order, tc.wantOrder)
			}
			if levels[1].Order != 2 {
				t.Errorf("соседний уровень сместился: %d", levels[1].Order)
			}
		})
	}
}

func TestAdminGetLevel(t *testing.T) {
	s := newAdminServer(t, map[string]string{"zadanie": "admin_levels_list.html"})
	info, err := s.client.AdminGetLevel(context.Background(), 4242, 901)
	if err != nil {
		t.Fatal(err)
	}
	if q := s.gets[0]; q.Get("id") != "901" || q.Get("categoryValue") != "4242" {
		t.Errorf("query = %v", q)
	}
	if info.ID != 901 || info.GameID != 4242 || info.Order != 1 {
		t.Errorf("ids = %+v", info)
	}
	p := info.Params
	if p.Title != "Гаражи" || p.Question != "Найдите <b>три</b> кода во дворе" || p.Hint1 != "Подсказка 1 к уровню 1" || p.Hint2 != "Подсказка 2 к уровню 1" {
		t.Errorf("texts = %+v", p)
	}
	if p.ClueMin != 30 || p.ClueMin2 != 30 || p.ClueMin3 != 30 || p.CodeCount != 0 || p.TryLimit != 0 {
		t.Errorf("numbers = %+v", p)
	}
	if len(p.Codes) != 3 {
		t.Fatalf("codes = %+v", p.Codes)
	}
	if p.Codes[0] != (dzzzr.LevelCode{Code: "1", Synonyms: "ОДИН#ONE", Danger: "1", Sector: 1}) {
		t.Errorf("codes[0] = %+v", p.Codes[0])
	}
	if p.Codes[2] != (dzzzr.LevelCode{Code: "3", Danger: "2", Sector: 2}) {
		t.Errorf("codes[2] = %+v", p.Codes[2])
	}
	if len(p.SectorNames) != 2 || p.SectorNames[0] != "Гаражи" || p.SectorNames[1] != "Пустырь" {
		t.Errorf("sector names = %v", p.SectorNames)
	}
	if len(p.BonusCodes) != 1 || p.BonusCodes[0] != (dzzzr.BonusCode{Code: "B1", Danger: "1", Minutes: 10}) {
		t.Errorf("bonus codes = %+v", p.BonusCodes)
	}
	if len(p.FakeCodes) != 0 {
		t.Errorf("fake codes = %+v", p.FakeCodes)
	}
	if len(p.Spoilers) != 1 || p.Spoilers[0] != (dzzzr.SpoilerParams{Text: "Скрытая подсказка", Code: "SP1", Synonyms: "СП1", Penalty: 5}) {
		t.Errorf("spoilers = %+v", p.Spoilers)
	}
	if p.Lat != "55.75" || p.Lon != "37.61" || p.Location != "Двор за гаражами" || p.LocationComment != "Тихий район" {
		t.Errorf("location = %+v", p)
	}
	if p.Publish == nil || !*p.Publish || p.Bonus == nil || *p.Bonus || p.ClueBefore == nil || !*p.ClueBefore || p.Penalty != nil {
		t.Errorf("flags = %+v", p)
	}
}

func TestAdminCreateLevel(t *testing.T) {
	s := newAdminServer(t, map[string]string{"zadanie": "admin_levels_list.html"})
	s.redirect = func(form url.Values) string {
		return "/moscow/admin/?action=zadanie&categoryValue=4242&edit=1&id=904&ofset=0&Project=&err="
	}
	id, err := s.client.AdminCreateLevel(context.Background(), 4242, dzzzr.LevelParams{
		Title:    "Новый",
		Question: "Вопрос",
		Hint1:    "П1", Hint2: "П2",
		Codes:       []dzzzr.LevelCode{{Code: "A", Danger: "1", Sector: 1}, {Code: "B", Synonyms: "БЭ", Danger: "2"}},
		SectorNames: []string{"Первый"},
		BonusCodes:  []dzzzr.BonusCode{{Code: "BB", Minutes: 5}},
		FakeCodes:   []dzzzr.FakeCode{{Code: "FF", Penalty: 7}},
		Spoilers:    []dzzzr.SpoilerParams{{Text: "Спойлер", Code: "SC", Penalty: 3}},
		CodeCount:   1, ClueMin: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != 904 {
		t.Errorf("id = %d", id)
	}
	f := s.lastPost()
	checks := map[string]string{
		"action": "add_zadanie", "categoryValue": "4242", "category": "4242",
		"title": "Новый", "question": "Вопрос", "clue1": "П1", "clue2": "П2",
		"code[0]": "A", "danger[0]": "1", "sector[0]": "1",
		"code[1]": "B", "codeS[1]": "БЭ", "danger[1]": "2", "sector[1]": "",
		"secName[1]": "Первый", "codeB[0]": "BB", "dangerB[0]": "1", "timeB[0]": "5",
		"codeF[0]": "FF", "fakeShtraf[0]": "7",
		"spoiler[1]": "Спойлер", "spoilerCode[1]": "SC", "spoilerPenalty[1]": "3",
		"codeCount": "1", "ClueMin": "25", "publish": "on",
	}
	for k, want := range checks {
		if got := f.Get(k); got != want {
			t.Errorf("form[%q] = %q, want %q", k, got, want)
		}
	}
	if _, err := s.client.AdminCreateLevel(context.Background(), 4242, dzzzr.LevelParams{Title: "x", Codes: []dzzzr.LevelCode{{Code: "A"}}}); err == nil {
		t.Error("code without danger must fail")
	}
	if _, err := s.client.AdminCreateLevel(context.Background(), 4242, dzzzr.LevelParams{}); err == nil {
		t.Error("empty level must fail")
	}
}

func TestAdminUpdateLevelCarriesFields(t *testing.T) {
	s := newAdminServer(t, map[string]string{"zadanie": "admin_levels_list.html"})
	pub := false
	err := s.client.AdminUpdateLevel(context.Background(), 4242, 901, dzzzr.LevelParams{
		Title:   "Гаражи-2",
		Codes:   []dzzzr.LevelCode{{Code: "9", Danger: "3"}},
		Publish: &pub,
	})
	if err != nil {
		t.Fatal(err)
	}
	f := s.lastPost()
	if f.Get("action") != "update_zadanie" || f.Get("id") != "901" || f.Get("title") != "Гаражи-2" {
		t.Errorf("form = %v", f)
	}
	if f.Get("clue1") != "Подсказка 1 к уровню 1" || f.Get("order_p") != "1" || f.Get("spoilerCode[1]") != "SP1" {
		t.Errorf("carried fields missing: %v", f)
	}
	if f.Get("code[0]") != "9" || f.Get("danger[0]") != "3" || f.Has("code[1]") || f.Has("code[2]") {
		t.Errorf("codes not replaced: %v", f)
	}
	if f.Has("publish") {
		t.Errorf("publish must be cleared: %v", f["publish"])
	}
}

// TestAdminMoveLevel pins the fields the live arrow forms submit, measured
// against classic.dzzzr.ru on 2026-09-09: the engine drives reordering
// through its generic row mover, so table and parentName name the relation
// and the level id alone identifies the row — no game id is sent.
func TestAdminMoveLevel(t *testing.T) {
	s := newAdminServer(t, map[string]string{"zadanie": "admin_levels_list.html"})
	if err := s.client.AdminMoveLevel(context.Background(), 902, true); err != nil {
		t.Fatal(err)
	}
	f := s.lastPost()
	want := map[string]string{
		"action": "move_up", "id": "902",
		"table": "zadanie", "parentName": "game", "ofset": "0",
	}
	for k, v := range want {
		if got := f.Get(k); got != v {
			t.Errorf("form[%q] = %q, want %q", k, got, v)
		}
	}
	if err := s.client.AdminMoveLevel(context.Background(), 902, false); err != nil {
		t.Fatal(err)
	}
	if got := s.lastPost().Get("action"); got != "move_down" {
		t.Errorf("action = %q, want move_down", got)
	}
}

// TestAdminUpdateLevelClearsTextField mirrors the game-level clear: an
// explicitly empty text parameter blanks the field instead of being read as
// "not given".
func TestAdminUpdateLevelClearsTextField(t *testing.T) {
	s := newAdminServer(t, map[string]string{"zadanie": "admin_levels_list.html"})
	err := s.client.AdminUpdateLevel(context.Background(), 4242, 901, dzzzr.LevelParams{
		Clear: []string{"location_comment", "subtitle"},
	})
	if err != nil {
		t.Fatal(err)
	}
	f := s.lastPost()
	for _, k := range []string{"locationComment", "subtitle"} {
		if !f.Has(k) || f.Get(k) != "" {
			t.Errorf("поле %s не очищено: %q (есть: %v)", k, f.Get(k), f.Has(k))
		}
	}
	// greeting is deliberately not clearable through a level: the live level
	// form carries no such field at all (measured 2026-09-09), even though
	// this fixture, written from DzrSourceHelper rather than from the wire,
	// does have one.
	if dzzzr.LevelTextParam("greeting") {
		t.Error("greeting must not be listed as a clearable level text field")
	}
}

func TestAdminDeleteAndCopyLevels(t *testing.T) {
	s := newAdminServer(t, map[string]string{"zadanie": "admin_levels_list.html"})
	if err := s.client.AdminDeleteLevel(context.Background(), 4242, 903); err != nil {
		t.Fatal(err)
	}
	if f := s.lastPost(); f.Get("action") != "del_zadanie" || f.Get("id") != "903" || f.Get("categoryValue") != "4242" {
		t.Errorf("form = %v", f)
	}
	if err := s.client.AdminCopyLevels(context.Background(), 4300, 4242); err != nil {
		t.Fatal(err)
	}
	if f := s.lastPost(); f.Get("action") != "copy_zadanie" || f.Get("categoryValue") != "4300" || f.Get("game") != "4242" {
		t.Errorf("form = %v", f)
	}
}

// TestAdminLevelStrayQuoteInFieldName reproduces the level editor as
// classic.dzzzr.ru actually renders it, measured 2026-09-09: the difficulty
// and sector selects are written name=danger[0]" — an unquoted attribute with
// a stray closing quote — and the current value is marked selected on top of
// an already-selected empty placeholder.
//
// Read with the raw names, every difficulty and sector came back empty, and
// because AdminUpdateLevel re-posts the form it read, the broken names went
// back while danger[N] never did. Live, a title-only edit moved КОДДВА's
// difficulty onto КОДОДИН, dropped the other, and unpublished the level.
func TestAdminLevelStrayQuoteInFieldName(t *testing.T) {
	s := newAdminServer(t, map[string]string{"zadanie": "admin_level_stray_quote.html"})
	info, err := s.client.AdminGetLevel(context.Background(), 1383, 31049)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Params.Codes) != 2 {
		t.Fatalf("codes = %+v", info.Params.Codes)
	}
	// The last selected option wins, as a browser submits it.
	if c := info.Params.Codes[0]; c.Code != "КОДОДИН" || c.Danger != "1" || c.Sector != 1 {
		t.Errorf("codes[0] = %+v, want КОДОДИН difficulty 1 sector 1", c)
	}
	if c := info.Params.Codes[1]; c.Code != "КОДДВА" || c.Danger != "2" {
		t.Errorf("codes[1] = %+v, want КОДДВА difficulty 2", c)
	}
	// The repaired name must reach the engine, and the broken one must not.
	if err := s.client.AdminUpdateLevel(context.Background(), 1383, 31049, dzzzr.LevelParams{Title: "новое имя"}); err != nil {
		t.Fatal(err)
	}
	f := s.lastPost()
	if f.Get("danger[0]") != "1" || f.Get("danger[1]") != "2" {
		t.Errorf("difficulties not carried through the edit: danger[0]=%q danger[1]=%q", f.Get("danger[0]"), f.Get("danger[1]"))
	}
	for k := range f {
		if strings.Contains(k, `"`) {
			t.Errorf("posted a field name with the engine's stray quote: %q", k)
		}
	}
	if !f.Has("publish") {
		t.Error("a title-only edit must not unpublish the level")
	}
}

// The engine numbers sector names from one. A page with secName[0] is not
// something it emits, but a parser of remote HTML must not be able to panic
// on one.
func TestAdminGetLevelWithZeroSectorIndex(t *testing.T) {
	s := newAdminServer(t, map[string]string{"zadanie": "admin_level_zero_sector.html"})
	info, err := s.client.AdminGetLevel(context.Background(), 4242, 901)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Params.SectorNames) != 1 || info.Params.SectorNames[0] != "Первый" {
		t.Errorf("sector names = %v", info.Params.SectorNames)
	}
}
