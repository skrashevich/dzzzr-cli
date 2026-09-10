package dzzzr

import "testing"

// TestFormSelectSubmitsWhatABrowserWould pins the two select rules the admin
// area depends on. The engine writes the level editor's difficulty selects
// with the empty placeholder AND the real value both marked selected, so
// taking the first one read every difficulty as empty; a browser submits the
// last. A multi-select submits every selected option, and an unselected one
// submits nothing at all.
func TestFormSelectSubmitsWhatABrowserWould(t *testing.T) {
	const page = `<form>
<select name=danger><option value="" selected>-<option value="1" selected>1<option value="2">2</select>
<select name=plain><option value="a">a<option value="b">b</select>
<select name=many multiple><option value="x" selected>x<option value="y">y<option value="z" selected>z</select>
<select name=none multiple><option value="q">q</select>
</form>`
	doc, err := parseHTML(page)
	if err != nil {
		t.Fatal(err)
	}
	forms := doc.forms()
	if len(forms) != 1 {
		t.Fatalf("форм = %d", len(forms))
	}
	f := forms[0]
	if got := f.Fields.Get("danger"); got != "1" {
		t.Errorf("danger = %q, ожидалось последнее выбранное 1", got)
	}
	// With nothing selected a browser submits the first option.
	if got := f.Fields.Get("plain"); got != "a" {
		t.Errorf("plain = %q, ожидалось a", got)
	}
	if got := f.Fields["many"]; len(got) != 2 || got[0] != "x" || got[1] != "z" {
		t.Errorf("many = %v, ожидались все выбранные x и z", got)
	}
	if got, ok := f.Fields["none"]; ok {
		t.Errorf("невыбранный multiple отправлен: %v", got)
	}
}

// TestRawFormWithActionSkipsNestedForms covers the recovery of a form the
// parser dropped. The engine leaves a <form> unclosed, so every later one is
// ignored by the tree, and it also writes small <form>…</form> pairs inside
// table rows. Stopping at the first </form> after the action marker would cut
// the recovered form short — and because AdminSetApplication reposts what it
// read, a short read silently drops the fields it never saw.
func TestRawFormWithActionSkipsNestedForms(t *testing.T) {
	const page = `<html><body>
<form method=post name=store>
<input type=hidden name=action value=newTeam>
<input type=text name=name value=''>
<table><tr><td>
<form method=post name=frm>
<input type=hidden name=action value=updateTeam>
<input type=text name=id value=31>
<table><tr><td><form name=row><input type=hidden name=action value=delRow></form></td></tr></table>
<input type=checkbox name=pl[after] checked>
</form>
</td></tr></table>
</body></html>`
	doc, err := parseHTML(page)
	if err != nil {
		t.Fatal(err)
	}
	// The tree cannot offer it: the unclosed first form swallows the rest.
	for _, f := range doc.forms() {
		if f.Fields.Get("action") == "updateTeam" {
			t.Fatal("дерево неожиданно сохранило форму — тест перестал проверять восстановление")
		}
	}
	f := doc.formWithAction("updateTeam")
	if f == nil {
		t.Fatal("форма не восстановлена из исходного текста")
	}
	if got := f.Fields.Get("id"); got != "31" {
		t.Errorf("id = %q, ожидалось 31", got)
	}
	// The field after the nested form must survive: this is what a slice
	// ending at the first </form> would lose.
	if on, ok := f.Checkboxes["pl[after]"]; !ok || !on {
		t.Errorf("поле после вложенной формы потеряно: %v %v", on, ok)
	}
	if doc.formWithAction("noSuchAction") != nil {
		t.Error("несуществующее действие не должно ничего возвращать")
	}
}
