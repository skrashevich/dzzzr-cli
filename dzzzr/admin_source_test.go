package dzzzr_test

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func TestAdminCreateTechnicalLevel(t *testing.T) {
	s := newAdminServer(t, nil)
	id, err := s.client.AdminCreateTechnicalLevel(t.Context(), 42)
	if err != nil || id != 77 {
		t.Fatalf("id=%d error=%v", id, err)
	}
	form := s.lastPost()
	question := `<p>Это техническая заглушка</p>
<script src="../../uploaded/moscow/Night/jquery-1.11.2.min.txt"></script>
<script src="../../uploaded/moscow/Night/jquery.cookie.txt"></script>
<script type="text/javascript">// <![CDATA[
$(document).ready(function() {
  $.ajax({ url: "https://alt.where.games/initn.js?v=1", dataType: "script", cache: true, });
});
// ]]></script>`
	for key, want := range map[string]string{"action": "add_zadanie", "category": "42", "categoryValue": "42", "title": "Технический уровень", "question": question, "clue1": "-", "clue2": "-", "comment": "-", "code[0]": "456457643867837433636", "danger[0]": "null", "bonus": "on", "skvoz": "on"} {
		if got := form.Get(key); got != want {
			t.Errorf("%s=%q want %q", key, got, want)
		}
	}
	if len(s.posts) != 1 || len(s.gets) != 0 {
		t.Fatalf("unexpected requests: %d POST %d GET", len(s.posts), len(s.gets))
	}
}

func sourceLevelForm() url.Values {
	return url.Values{"action": {"update_zadanie"}, "id": {"7"}, "categoryValue": {"42"}, "category": {"42"}, "order_p": {"9"}, "engineOnly": {"keep", "second"}, "title": {"Old"}, "question": {"Old question"}, "clue1": {"Old hint"}, "comment": {"Фото кодов <img src=\"x.png\">"}, "timeAddBonusAll": {"15"}, "ClueMin": {"30"}, "penalty": {"20"}, "code[2]": {"old"}, "codeS[2]": {"alias"}, "danger[2]": {"2"}, "sector[2]": {"3"}, "secName[3]": {"sector"}, "codeB[1]": {"old bonus"}, "codeBS[1]": {"alias"}, "dangerB[1]": {"1"}, "timeB[1]": {"3"}, "codeF[0]": {"fake"}, "codeFS[0]": {"alias"}, "fakeShtraf[0]": {"4"}, "spoiler[1]": {"hidden"}, "spoilerCode[1]": {"open"}, "spoilerSynonyms[1]": {"alias"}, "spoilerPenalty[1]": {"8"}}
}

func sourceLevelClient(t *testing.T, fields url.Values) (*dzzzr.Client, <-chan url.Values, *atomic.Int32) {
	t.Helper()
	var page strings.Builder
	page.WriteString(`<form method="post">`)
	for key, values := range fields {
		for _, value := range values {
			_, _ = fmt.Fprintf(&page, `<input name="%s" value="%s">`, html.EscapeString(key), html.EscapeString(value))
		}
	}
	page.WriteString(`<input type="checkbox" name="bonus" checked><input type="checkbox" name="publish" checked></form>`)
	posts := make(chan url.Values, 2)
	requests := new(atomic.Int32)
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		wantBasic(t, r, "org", "secret")
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, page.String())
			return
		}
		form, err := readCP1251Form(r)
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		posts <- form
		http.Redirect(w, r, "/moscow/admin/?action=zadanie&id=7", http.StatusFound)
	}), dzzzr.WithAdminCredentials("org", "secret"))
	return client, posts, requests
}

func TestAdminSourceNewFieldsRoundTrip(t *testing.T) {
	fields := sourceLevelForm()
	client, _, _ := sourceLevelClient(t, fields)
	info, err := client.AdminGetLevel(t.Context(), 42, 7)
	if err != nil {
		t.Fatal(err)
	}
	if info.Params.Comment == nil || *info.Params.Comment != fields.Get("comment") || info.Params.TimeAddBonusAll == nil || *info.Params.TimeAddBonusAll != 15 {
		t.Fatalf("fields not parsed: %+v", info.Params)
	}
	fields.Del("comment")
	fields.Del("timeAddBonusAll")
	client, _, _ = sourceLevelClient(t, fields)
	info, err = client.AdminGetLevel(t.Context(), 42, 7)
	if err != nil {
		t.Fatal(err)
	}
	if info.Params.Comment != nil || info.Params.TimeAddBonusAll != nil {
		t.Fatal("absent fields must remain nil")
	}
}

func TestAdminUpdateSourceFields(t *testing.T) {
	for _, tc := range []struct {
		name           string
		params         dzzzr.LevelParams
		comment, bonus string
	}{
		{"preserve", dzzzr.LevelParams{Title: "New"}, "Фото кодов <img src=\"x.png\">", "15"},
		{"clear", dzzzr.LevelParams{Comment: new(""), TimeAddBonusAll: new(0)}, "", "0"},
		{"set", dzzzr.LevelParams{Comment: new("Новые фото"), TimeAddBonusAll: new(25)}, "Новые фото", "25"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, posts, _ := sourceLevelClient(t, sourceLevelForm())
			if err := client.AdminUpdateLevel(t.Context(), 42, 7, tc.params); err != nil {
				t.Fatal(err)
			}
			form := <-posts
			if form.Get("comment") != tc.comment || form.Get("timeAddBonusAll") != tc.bonus {
				t.Errorf("new fields: %v", form)
			}
			if form.Get("code[2]") != "old" || form.Get("order_p") != "9" {
				t.Error("patch changed unrelated fields")
			}
		})
	}
}

func TestAdminReplaceLevelSnapshot(t *testing.T) {
	for _, params := range []dzzzr.LevelParams{{}, {Title: "New", Codes: []dzzzr.LevelCode{{Code: "FIRST", Danger: "1"}, {Code: "SECOND", Danger: "2"}}, Comment: new(""), TimeAddBonusAll: new(0)}} {
		fields := sourceLevelForm()
		client, posts, _ := sourceLevelClient(t, fields)
		if err := client.AdminReplaceLevel(t.Context(), 42, 7, params); err != nil {
			t.Fatal(err)
		}
		form := <-posts
		for key, want := range map[string]string{"action": "update_zadanie", "id": "7", "category": "42", "categoryValue": "42", "order_p": "9", "title": params.Title, "question": "", "clue1": "", "comment": "", "timeAddBonusAll": "0", "ClueMin": "0", "penalty": ""} {
			if !form.Has(key) || form.Get(key) != want {
				t.Errorf("%s=%q want %q", key, form.Get(key), want)
			}
		}
		if !reflect.DeepEqual(form["engineOnly"], fields["engineOnly"]) {
			t.Error("lost unmodeled multivalue field")
		}
		for _, key := range []string{"bonus", "publish", "code[2]", "codeS[2]", "danger[2]", "sector[2]", "secName[3]", "codeB[1]", "codeBS[1]", "dangerB[1]", "timeB[1]", "codeF[0]", "codeFS[0]", "fakeShtraf[0]", "spoiler[1]", "spoilerCode[1]", "spoilerSynonyms[1]", "spoilerPenalty[1]"} {
			if form.Has(key) {
				t.Errorf("retained stale %s", key)
			}
		}
		if len(params.Codes) > 0 && (form.Get("code[0]") != "FIRST" || form.Get("code[1]") != "SECOND") {
			t.Error("lost source code order")
		}
	}
}

func TestAdminSourceInvalidIDs(t *testing.T) {
	client, _, requests := sourceLevelClient(t, sourceLevelForm())
	for _, ids := range [][2]int{{0, 7}, {-1, 7}, {42, 0}, {42, -1}} {
		if err := client.AdminReplaceLevel(t.Context(), ids[0], ids[1], dzzzr.LevelParams{}); err == nil {
			t.Errorf("accepted IDs %v", ids)
		}
	}
	for _, id := range []int{0, -1} {
		if _, err := client.AdminCreateTechnicalLevel(t.Context(), id); err == nil {
			t.Errorf("accepted game %d", id)
		}
	}
	if requests.Load() != 0 {
		t.Errorf("invalid IDs made %d requests", requests.Load())
	}
}

func TestAdminReplaceLevelRejectsMismatchedIdentity(t *testing.T) {
	for _, key := range []string{"id", "categoryValue"} {
		t.Run(key, func(t *testing.T) {
			fields := sourceLevelForm()
			fields.Set(key, "999")
			client, posts, requests := sourceLevelClient(t, fields)
			if err := client.AdminReplaceLevel(t.Context(), 42, 7, dzzzr.LevelParams{Title: "New"}); err == nil {
				t.Error("accepted wrong form identity")
			}
			if len(posts) != 0 || requests.Load() != 1 {
				t.Fatal("POST after mismatched identity")
			}
		})
	}
}
