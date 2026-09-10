package dzzzr_test

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func TestAdminReplaceGamePostsExplicitZeroAndEmpty(t *testing.T) {
	fields := url.Values{"action": {"update_game"}, "id": {"42"}, "name": {"Old"}, "price": {"500"}, "clueMin": {"30"}, "penalty": {"10"}, "league": {"3"}, "engineOnly": {"keep"}}
	var page strings.Builder
	page.WriteString("<form>")
	for name, values := range fields {
		for _, value := range values {
			_, _ = fmt.Fprintf(&page, `<input name="%s" value="%s">`, html.EscapeString(name), html.EscapeString(value))
		}
	}
	page.WriteString(`<input type="checkbox" name="publish" checked><input type="checkbox" name="invitation" checked></form>`)
	posts := make(chan url.Values, 1)
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		http.Redirect(w, r, "/moscow/admin/?action=games&id=42", http.StatusFound)
	}), dzzzr.WithAdminCredentials("org", "secret"))
	if err := c.AdminReplaceGame(t.Context(), 42, dzzzr.GameParams{Name: "New", Publish: new(false)}); err != nil {
		t.Fatal(err)
	}
	form := <-posts
	for k, want := range map[string]string{"name": "New", "price": "", "clueMin": "0", "penalty": "0", "league": "0", "engineOnly": "keep", "action": "update_game", "id": "42"} {
		if !form.Has(k) || form.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, form.Get(k), want)
		}
	}
	if form.Has("publish") || form.Has("invitation") {
		t.Fatal("false checkboxes were posted")
	}
}

func TestAdminReplaceLevelKeepsEmptyBonusDifficulty(t *testing.T) {
	c, posts, _ := sourceLevelClient(t, sourceLevelForm())
	if err := c.AdminReplaceLevel(t.Context(), 42, 7, dzzzr.LevelParams{BonusCodes: []dzzzr.BonusCode{{Code: "BONUS", Danger: ""}}}); err != nil {
		t.Fatal(err)
	}
	form := <-posts
	if !form.Has("dangerB[0]") || form.Get("dangerB[0]") != "" {
		t.Fatalf("difficulty changed: %q", form.Get("dangerB[0]"))
	}
	if !form.Has("penalty") || form.Get("penalty") != "" {
		t.Fatal("inheritance not restored")
	}
}
