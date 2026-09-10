package main

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/charmap"
)

// The organizer half of the mock. The engine's admin area is server-rendered
// HTML in windows-1251 behind HTTP Basic, and every write is a POST answered
// by a redirect. The pages here are cut down to what the client reads: the
// list tables it scrapes, the forms it re-submits, and the hidden fields it
// carries over. They are emitted in UTF-8 because the client sniffs the
// encoding and both are handled the same way.

const (
	adminLogin    = "admin"
	adminPassword = "admin"
)

func init() {
	adminMounts = append(adminMounts, func(s *server, mux *http.ServeMux, prefix string) {
		mux.HandleFunc(prefix+"admin/", s.handleAdmin)
		mux.HandleFunc(prefix+"admin/gmAdmin.php", s.handleAdminMonitor)
		mux.HandleFunc(prefix+"admin/lookLog.php", s.handleAdminLog)
		mux.HandleFunc(prefix+"admin/gmcht.php", s.handleAdminChat)
	})
}

// requireAdmin enforces the organizer's HTTP Basic credentials.
func (s *server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	user, pass, ok := r.BasicAuth()
	if !ok || user != adminLogin || pass != adminPassword {
		w.Header().Set("WWW-Authenticate", `Basic realm="Admin area"`)
		w.WriteHeader(http.StatusUnauthorized)
		writeHTML(w, "<html><body>401 Unauthorized</body></html>")
		return false
	}
	return true
}

// writeHTML sends an administration page the way the engine does: in
// windows-1251, which is what admin/admin.php declares and what its database
// connection speaks.
func writeHTML(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/html; charset=windows-1251")
	_, _ = w.Write(encodeCP1251(body))
}

// adminNoAccessBody reproduces the engine's own reply for an organizer
// account that is valid but is not this game's assigned organizer: a 200
// whose body is just this sentence, optionally led by the account's login
// the way admin/gmcht.php prints it. Measured against classic.dzzzr.ru on
// 2026-09-09; see quirkAdminNoAccess.
func adminNoAccessBody(login string) string {
	if login == "" {
		return "Вы не имеете доступа к этому разделу"
	}
	return login + " Вы не имеете доступа к этому разделу. (0)"
}

// encodeCP1251 converts a page to windows-1251.
func encodeCP1251(s string) []byte {
	out, err := charmap.Windows1251.NewEncoder().Bytes([]byte(s))
	if err != nil {
		return []byte(s)
	}
	return out
}

// parseAdminForm reads a posted form as windows-1251. The engine's admin
// pages carry no accept-charset and its handlers store what they receive
// unchanged, so a client that posts UTF-8 there corrupts the game's texts;
// decoding here is what makes that visible in tests.
func parseAdminForm(r *http.Request) (url.Values, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	raw, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, err
	}
	decoder := charmap.Windows1251.NewDecoder()
	decode := func(s string) string {
		out, err := decoder.String(s)
		if err != nil {
			return s
		}
		return out
	}
	out := url.Values{}
	for k, vs := range raw {
		key := decode(k)
		for _, v := range vs {
			out.Add(key, decode(v))
		}
	}
	return out, nil
}

// adminRedirect answers a write the way the engine does.
func adminRedirect(w http.ResponseWriter, r *http.Request, target string) {
	http.Redirect(w, r, target, http.StatusFound)
}

// handleAdmin serves ?action=games|zadanie|teams and their POSTs.
func (s *server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		form, err := parseAdminForm(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.adminWrite(w, r, form)
		return
	}
	q := r.URL.Query()
	s.mu.Lock()
	defer s.mu.Unlock()
	switch q.Get("action") {
	case "games":
		writeHTML(w, s.gamesPage(q))
	case "zadanie":
		writeHTML(w, s.levelsPage(q))
	case "teams":
		if s.has(quirkAdminNoAccess) {
			writeHTML(w, adminNoAccessBody(""))
			return
		}
		writeHTML(w, s.teamsPage(q))
	default:
		writeHTML(w, page("<p>Выберите раздел</p><a href=?action=games&edit=1>игры</a>"))
	}
}

// adminWrite applies one form submission.
func (s *server) adminWrite(w http.ResponseWriter, r *http.Request, form url.Values) {
	target := s.applyAdminWrite(form)
	adminRedirect(w, r, target)
}

// applyAdminWrite performs one form submission and returns the page the
// engine would redirect to.
func (s *server) applyAdminWrite(form url.Values) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.state
	action := form.Get("action")
	gameID := parseInt(form.Get("categoryValue"))
	target := "?action=games&edit=1&Project="
	switch action {
	case "add_game":
		// The mock hosts a single game, so a create only reports an id.
		target = fmt.Sprintf("?action=games&edit=1&id=%d&ofset=0&Project=", g.GameID+1)
	case "update_game":
		if name := form.Get("name"); name != "" {
			g.GameName = name
		}
		if n := form.Get("number"); n != "" {
			g.Number = n
		}
		if v := form.Get("greeting"); v != "" {
			g.Greeting = v
		}
		if v := form.Get("masterCode"); v != "" {
			g.MasterCode = strings.ToUpper(v)
		}
		g.Finished = form.Get("finished") != ""
		target = fmt.Sprintf("?action=games&edit=1&id=%s&ofset=0&Project=", form.Get("id"))
	case "del_games":
		target = "?action=games&edit=1&ofset=0&Project="
	case "copy_game":
		target = fmt.Sprintf("?action=games&id=%d&edit=1&Project=", g.GameID+2)
	case "add_zadanie":
		order := len(g.Levels) + 1
		l := &levelDef{
			ID: nextLevelID(g), Order: order, Title: form.Get("title"), Question: form.Get("question"),
			Clue1: form.Get("clue1"), Clue2: form.Get("clue2"),
			ClueMin: 30, ClueMin2: 30, ClueMin3: 30,
			CodeCount: parseInt(form.Get("codeCount")), TryLimit: parseInt(form.Get("tryLimit")),
			LocationComment: form.Get("locationComment"),
		}
		applyIndexed(form, "code", func(i int, v string) {
			if v == "" {
				return
			}
			l.Codes = append(l.Codes, strings.ToUpper(v))
			l.Dangers = append(l.Dangers, form.Get(fmt.Sprintf("danger[%d]", i)))
			l.Sectors = append(l.Sectors, form.Get(fmt.Sprintf("sector[%d]", i)))
		})
		applyIndexed(form, "secName", func(_ int, v string) {
			if v != "" {
				l.SectorNames = append(l.SectorNames, v)
			}
		})
		applyIndexed(form, "codeB", func(i int, v string) {
			if v == "" {
				return
			}
			l.BonusCodes = append(l.BonusCodes, strings.ToUpper(v))
			l.BonusDangers = append(l.BonusDangers, form.Get(fmt.Sprintf("dangerB[%d]", i)))
			l.BonusTimes = append(l.BonusTimes, parseInt(form.Get(fmt.Sprintf("timeB[%d]", i))))
		})
		applyIndexed(form, "codeF", func(i int, v string) {
			if v == "" {
				return
			}
			l.FakeCodes = append(l.FakeCodes, strings.ToUpper(v))
			l.FakePenalties = append(l.FakePenalties, parseInt(form.Get(fmt.Sprintf("fakeShtraf[%d]", i))))
		})
		applyIndexed(form, "spoiler", func(i int, v string) {
			code := form.Get(fmt.Sprintf("spoilerCode[%d]", i))
			if v == "" && code == "" {
				return
			}
			l.Spoilers = append(l.Spoilers, spoilerDef{
				Order: i, Text: v, Code: strings.ToUpper(code),
				Penalty: parseInt(form.Get(fmt.Sprintf("spoilerPenalty[%d]", i))),
			})
		})
		g.Levels = append(g.Levels, l)
		target = fmt.Sprintf("?action=zadanie&categoryValue=%d&edit=1&id=%d&ofset=0&Project=&err=", gameID, l.ID)
	case "update_zadanie":
		if l := levelByID(g, parseInt(form.Get("id"))); l != nil {
			if v := form.Get("title"); v != "" {
				l.Title = v
			}
			if v := form.Get("question"); v != "" {
				l.Question = v
			}
			if v := form.Get("clue1"); v != "" {
				l.Clue1 = v
			}
			if v := form.Get("clue2"); v != "" {
				l.Clue2 = v
			}
			if form.Has("codeCount") {
				l.CodeCount = parseInt(form.Get("codeCount"))
			}
			if form.Has("code[0]") {
				l.Codes, l.Dangers, l.Sectors = nil, nil, nil
				applyIndexed(form, "code", func(i int, v string) {
					if v == "" {
						return
					}
					l.Codes = append(l.Codes, strings.ToUpper(v))
					l.Dangers = append(l.Dangers, form.Get(fmt.Sprintf("danger[%d]", i)))
					l.Sectors = append(l.Sectors, form.Get(fmt.Sprintf("sector[%d]", i)))
				})
			}
		}
		target = fmt.Sprintf("?action=zadanie&categoryValue=%d&edit=1&id=%s&ofset=0&Project=&err=", gameID, form.Get("id"))
	case "del_zadanie":
		id := parseInt(form.Get("id"))
		for i, l := range g.Levels {
			if l.ID == id {
				g.Levels = append(g.Levels[:i], g.Levels[i+1:]...)
				break
			}
		}
		renumber(g)
		target = fmt.Sprintf("?action=zadanie&categoryValue=%d&edit=1&ofset=0&Project=", gameID)
	case "move_up", "move_down":
		// The engine's generic row mover: it swaps a level with its
		// neighbor and keeps both ids, so only the order changes. Moving
		// past either end is accepted and changes nothing.
		id := parseInt(form.Get("id"))
		for i, l := range g.Levels {
			if l.ID != id {
				continue
			}
			j := i + 1
			if action == "move_up" {
				j = i - 1
			}
			if j >= 0 && j < len(g.Levels) {
				g.Levels[i], g.Levels[j] = g.Levels[j], g.Levels[i]
				renumber(g)
			}
			break
		}
		target = fmt.Sprintf("?action=zadanie&categoryValue=%d&edit=1&ofset=0&Project=", gameID)
	case "copy_zadanie":
		target = fmt.Sprintf("?action=zadanie&categoryValue=%d&edit=1&Project=", gameID)
	case "updateTeam":
		if v := form.Get("name"); v != "" {
			g.TeamName = v
		}
		if form.Get("newPIN") != "" {
			g.Pin = "5678"
		}
		g.Blocked = form.Get("blocked") != ""
		g.Status = parseInt(form.Get("status"))
		g.RatingPoints = parseInt(form.Get("points"))
		target = fmt.Sprintf("?action=teams&desc=&categoryValue=%d&edit=1&Project=&id=%s&ofset=0&err=", gameID, form.Get("id"))
	case "acceptAllApplications", "GeneratePinCodes":
		g.Status = 1
		g.Pin = "5678"
		target = fmt.Sprintf("?action=teams&edit=1&id=&categoryValue=%d&ofset=0&Project=", gameID)
	case "addTeamToGame", "del_application", "newTeam":
		target = fmt.Sprintf("?action=teams&categoryValue=%d&edit=1&Project=&ofset=0&id=%d&err=", gameID, g.TeamID+1)
	default:
		target = "?action=games&edit=1&Project="
	}
	return target
}

// applyIndexed calls fn for every "name[i]" field, in index order.
func applyIndexed(form url.Values, name string, fn func(i int, v string)) {
	prefix := name + "["
	idx := make([]int, 0, len(form))
	values := map[int]string{}
	for k, vs := range form {
		if !strings.HasPrefix(k, prefix) || !strings.HasSuffix(k, "]") {
			continue
		}
		n, err := strconv.Atoi(k[len(prefix) : len(k)-1])
		if err != nil {
			continue
		}
		idx = append(idx, n)
		if len(vs) > 0 {
			values[n] = vs[0]
		}
	}
	sort.Ints(idx)
	for _, i := range idx {
		fn(i, values[i])
	}
}

func parseInt(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// levelIDOf gives a level a stable database-style id, distinct from its
// order, so a client that confuses the two fails here as it would on the
// real engine.
func levelIDOf(order int) int { return 900 + order }

func levelByID(g *gameState, id int) *levelDef {
	for _, l := range g.Levels {
		if l.ID == id {
			return l
		}
	}
	return nil
}

// nextLevelID allocates an id above every existing one, so a level added
// after a reorder cannot collide with a level that kept an older id.
func nextLevelID(g *gameState) int {
	next := 900
	for _, l := range g.Levels {
		if l.ID > next {
			next = l.ID
		}
	}
	return next + 1
}

// renumber restores consecutive order values after a move.
func renumber(g *gameState) {
	for i, l := range g.Levels {
		l.Order = i + 1
	}
}

func esc(s string) string { return html.EscapeString(s) }

func page(body string) string {
	return `<html><head><meta http-equiv="Content-Type" content="text/html; charset=windows-1251"><title>DozoR</title></head><body>` + body + `</body></html>`
}

// gamesPage renders the games list and, when a game is selected, its form.
func (s *server) gamesPage(q url.Values) string {
	g := s.state
	var b strings.Builder
	b.WriteString(`<table cellpadding=0 cellspacing=0 border=0 width=100%>`)
	b.WriteString(`<tr align=center class=news style="color:white" bgcolor=a1a1a1><td height=35 width=10></td><td><b>Дата</b></td><td width=10></td><td><strong>N</strong></td><td width=70%><b>Название</b></td><td width=10></td><td colspan=2 width=70%></td><td colspan=2></td><td></td></tr>`)
	status := "анонс"
	if g.Finished {
		status = "завершена"
	}
	_, _ = fmt.Fprintf(&b, `<tr valign=top bgcolor=FFFEDE><td></td><td class=news nowrap><b>%s</b></td><td width=10></td><td nowrap class=news>%s</td><td><table cellpadding=0 cellspacing=0><tr><td valign=top width=20></td><td width=10></td><td class=news width=400><b>%s</b></td></tr></table></td><td width=10></td><td class=news nowrap>%s</td><td class=news nowrap>сезон %d 2026</td><td width=20 align=right></td><td width=70 class=news align=left></td><td width=10></td></tr>`,
		g.Start.Format("02.01.2006 15:04"), esc(g.Number), esc(g.GameName), status, g.Season)
	b.WriteString(`</table>`)

	b.WriteString(`<form method=post name=store>`)
	_, _ = fmt.Fprintf(&b, `<input type=hidden name=id value=%d>`, g.GameID)
	b.WriteString(`<input type=hidden name=Project value=><input type=hidden name=ofset value=0><input type=hidden name=categoryValue value=>`)
	b.WriteString(`<input type=hidden name=action value=update_game>`)
	_, _ = fmt.Fprintf(&b, `<input type=text name=name value='%s'>`, esc(g.GameName))
	_, _ = fmt.Fprintf(&b, `<input type=text name=number value="%s">`, esc(g.Number))
	_, _ = fmt.Fprintf(&b, `<input type=text name=date value="%s"><input type=text name=time value="%s">`,
		g.Start.Format("02.01.2006"), g.Start.Format("15:04"))
	_, _ = fmt.Fprintf(&b, `<input type=text name=author value='%s'>`, esc(g.Authors))
	b.WriteString(`<select name=league><option value=1 selected>Первая<option value=2>Вторая</select>`)
	b.WriteString(`<input type=checkbox name=otherLeague> другая лига`)
	_, _ = fmt.Fprintf(&b, `<textarea name=legend>%s</textarea>`, esc(g.Legend))
	b.WriteString(`<textarea name=legendComment></textarea><textarea name=anons></textarea>`)
	b.WriteString(`<input type=text name=clueMin value="30"><input type=text name=clueMin2 value="30"><input type=text name=clueMin3 value="30">`)
	_, _ = fmt.Fprintf(&b, `<input type=text name=masterCode value="%s"><input type=text name=masterCodeShtraf value="%d">`, esc(g.MasterCode), g.MasterCodePenalty)
	_, _ = fmt.Fprintf(&b, `<input type=text name=clueBeforeShtraf value="%d">`, g.ClueBeforePenalty)
	b.WriteString(`<input type=text name=penalty value="30"><input type=text name=price value="1500">`)
	b.WriteString(`<textarea name=breafPlace>Парковка у ТЦ</textarea>`)
	_, _ = fmt.Fprintf(&b, `<input type=text name=greeting value="%s">`, esc(g.Greeting))
	_, _ = fmt.Fprintf(&b, `<input type=text name=bonusAfter value="%d">`, g.BonusAfter)
	b.WriteString(`<input type=checkbox name=publish checked> опубликовать`)
	if g.Finished {
		b.WriteString(`<input type=checkbox name=finished checked> завершена`)
	} else {
		b.WriteString(`<input type=checkbox name=finished> завершена`)
	}
	b.WriteString(`<input type=submit value="Сохранить"></form>`)

	b.WriteString(`<form method=post><input type=hidden name=Project value=><input type=hidden name=action value=copy_game>`)
	_, _ = fmt.Fprintf(&b, `<select name=game><option value=%d>%s</select>`, g.GameID, esc(g.GameName))
	b.WriteString(`<input type=checkbox name=zadan> копировать с заданиями<input type=submit value="Копировать"></form>`)
	return page(b.String())
}

// levelsPage renders the level list and the selected level's form.
func (s *server) levelsPage(q url.Values) string {
	g := s.state
	selected := parseInt(q.Get("id"))
	if selected == 0 && len(g.Levels) > 0 {
		selected = g.Levels[0].ID
	}
	var b strings.Builder
	b.WriteString(`<table cellpadding=0 cellspacing=0 border=0 width=100%>`)
	b.WriteString(`<tr align=center class=news style="color:white" bgcolor=a1a1a1><td height=35 width=10></td><td><b>Номер</b></td><td width=10></td><td width=40%><b>Название уровня</b></td><td width=40><b>Интервалы</b></td><td width=40><b>Лимит<br>команд</b></td><td width=10><strong>Коды</strong></td><td width=10><strong>Нужно<br>найти</strong></td><td>&nbsp;<strong>Попытки<br>ввода</strong></td><td><strong>Спойлер</strong></td><td colspan=4></td><td></td></tr>`)
	for _, l := range g.Levels {
		id := l.ID
		kind := "<strong>основной</strong>"
		if l.Bonus {
			kind = "бонусный"
			if l.Skvoz {
				kind += " <br>сквозной"
			}
			if l.BonusTime > 0 {
				kind += fmt.Sprintf(" -%d мин.", l.BonusTime)
			}
		}
		title := esc(l.Title)
		nameCell := fmt.Sprintf(`<a href=?action=zadanie&edit=1&id=%d&categoryValue=%d&ofset=0&Project= class=news>%s</a>`, id, g.GameID, title)
		rowAttr := ""
		if id == selected {
			rowAttr = " bgcolor=FFFEDE"
			nameCell = "<b>" + title + "</b>"
		}
		var codes strings.Builder
		codes.WriteString(`<select class=news>`)
		for _, c := range l.Codes {
			_, _ = fmt.Fprintf(&codes, "<option>%s", esc(c))
		}
		if len(l.BonusCodes) > 0 {
			codes.WriteString("<option>---бонусные")
			for i, c := range l.BonusCodes {
				minutes := 0
				if i < len(l.BonusTimes) {
					minutes = l.BonusTimes[i]
				}
				_, _ = fmt.Fprintf(&codes, "<option>%s (%d мин.)", esc(c), minutes)
			}
		}
		if len(l.FakeCodes) > 0 {
			codes.WriteString("<option>---ложные")
			for i, c := range l.FakeCodes {
				penalty := 0
				if i < len(l.FakePenalties) {
					penalty = l.FakePenalties[i]
				}
				_, _ = fmt.Fprintf(&codes, "<option>%s (%d мин.)", esc(c), penalty)
			}
		}
		codes.WriteString(`</select>`)
		needed := "все"
		if l.CodeCount > 0 {
			needed = strconv.Itoa(l.CodeCount)
		}
		tryLimit := ""
		if l.TryLimit > 0 {
			tryLimit = strconv.Itoa(l.TryLimit)
		}
		spoiler := ""
		if len(l.Spoilers) > 0 {
			spoiler = "+"
		}
		printed := l.Order
		if l.ID == selected && s.has(quirkSelectedLevelZero) {
			printed = 0
		}
		_, _ = fmt.Fprintf(&b, `<tr valign=top%s><td></td><td class=news><table cellpadding=0 cellspacing=0><tr><td></td><td class=news><span>%d</span>.</td></tr></table></td><td width=10></td><td><table cellpadding=0 cellspacing=0><tr><td valign=top width=20></td><td width=10></td><td class=news width=400>%s</td></tr></table></td><td nowrap class=news align=center>%d:%d:%d</td><td nowrap class=news align=center>&nbsp;</td><td nowrap class=news>%s</td><td class=news align=center>%s</td><td class=news align=center>%s</td><td align=center>%s</td><td nowrap class=news>%s</td><td width=10></td><td width=20 align=right></td><td width=70 class=news align=left></td><td width=10></td></tr>`,
			rowAttr, printed, nameCell, l.ClueMin, l.ClueMin2, l.ClueMin3, codes.String(), needed, tryLimit, spoiler, kind)
	}
	b.WriteString(`</table>`)

	if l := levelByID(g, selected); l != nil {
		b.WriteString(levelForm(g, l))
	}
	return page(b.String())
}

// levelForm renders the edit form of one level, with the indexed fields the
// client reads and re-submits.
func levelForm(g *gameState, l *levelDef) string {
	var b strings.Builder
	b.WriteString(`<form method=post name=store>`)
	_, _ = fmt.Fprintf(&b, `<input type=hidden name=id value=%d>`, l.ID)
	b.WriteString(`<input type=hidden name=Project value=><input type=hidden name=ofset value=0>`)
	_, _ = fmt.Fprintf(&b, `<input type=hidden name=categoryValue value=%d><input type=hidden name=category value=%d>`, g.GameID, g.GameID)
	b.WriteString(`<input type=hidden name=action value=update_zadanie>`)
	_, _ = fmt.Fprintf(&b, `<input type=hidden name=order_p value=%d>`, l.Order)
	_, _ = fmt.Fprintf(&b, `<input type=text name=title value='%s'>`, esc(l.Title))
	b.WriteString(`<input type=text name=subtitle value=''>`)
	_, _ = fmt.Fprintf(&b, `<textarea name=question>%s</textarea>`, esc(l.Question))
	_, _ = fmt.Fprintf(&b, `<textarea name=clue1>%s</textarea>`, esc(l.Clue1))
	_, _ = fmt.Fprintf(&b, `<textarea name=clue2>%s</textarea>`, esc(l.Clue2))
	_, _ = fmt.Fprintf(&b, `<input type=text name=ClueMin value="%d"><input type=text name=ClueMin2 value="%d"><input type=text name=ClueMin3 value="%d">`, l.ClueMin, l.ClueMin2, l.ClueMin3)
	b.WriteString(`<input type=checkbox name=zapros1><input type=text name=interval1 value=""><input type=text name=shtraf1 value="">`)
	b.WriteString(`<input type=checkbox name=zapros2><input type=text name=interval2 value=""><input type=text name=shtraf2 value="">`)
	_, _ = fmt.Fprintf(&b, `<textarea name=location></textarea><textarea name=locationComment>%s</textarea>`, esc(l.LocationComment))
	for i, c := range l.Codes {
		danger, sector := "", ""
		if i < len(l.Dangers) {
			danger = l.Dangers[i]
		}
		if i < len(l.Sectors) {
			sector = l.Sectors[i]
		}
		_, _ = fmt.Fprintf(&b, `<input type=text name=code[%d] value="%s"><input type=text name=codeS[%d] value="">`, i, esc(c), i)
		_, _ = fmt.Fprintf(&b, `<select name=danger[%d]><option value="">-<option value="%s" selected>%s</select>`, i, esc(danger), esc(danger))
		if sector == "" {
			_, _ = fmt.Fprintf(&b, `<select name=sector[%d]><option value="" selected>-<option value=1>1</select>`, i)
		} else {
			_, _ = fmt.Fprintf(&b, `<select name=sector[%d]><option value="">-<option value=%s selected>%s</select>`, i, esc(sector), esc(sector))
		}
	}
	for i, n := range l.SectorNames {
		_, _ = fmt.Fprintf(&b, `<input type=text name=secName[%d] value='%s'>`, i+1, esc(n))
	}
	for i, c := range l.BonusCodes {
		danger, minutes := "1", 0
		if i < len(l.BonusDangers) {
			danger = l.BonusDangers[i]
		}
		if i < len(l.BonusTimes) {
			minutes = l.BonusTimes[i]
		}
		_, _ = fmt.Fprintf(&b, `<input type=text name=codeB[%d] value="%s"><input type=text name=codeBS[%d] value="">`, i, esc(c), i)
		_, _ = fmt.Fprintf(&b, `<select name=dangerB[%d]><option value="">-<option value="%s" selected>%s</select>`, i, esc(danger), esc(danger))
		_, _ = fmt.Fprintf(&b, `<input type=text name=timeB[%d] value="%d">`, i, minutes)
	}
	for i, c := range l.FakeCodes {
		penalty := 0
		if i < len(l.FakePenalties) {
			penalty = l.FakePenalties[i]
		}
		_, _ = fmt.Fprintf(&b, `<input type=text name=codeF[%d] value="%s"><input type=text name=codeFS[%d] value=""><input type=text name=fakeShtraf[%d] value="%d">`, i, esc(c), i, i, penalty)
	}
	for _, sp := range l.Spoilers {
		_, _ = fmt.Fprintf(&b, `<textarea name=spoiler[%d]>%s</textarea>`, sp.Order, esc(sp.Text))
		_, _ = fmt.Fprintf(&b, `<input type=text name=spoilerCode[%d] value="%s"><input type=text name=spoilerSynonyms[%d] value="">`, sp.Order, esc(sp.Code), sp.Order)
		_, _ = fmt.Fprintf(&b, `<input type=text name=spoilerPenalty[%d] value="%d">`, sp.Order, sp.Penalty)
	}
	needed := ""
	if l.CodeCount > 0 {
		needed = strconv.Itoa(l.CodeCount)
	}
	tryLimit := ""
	if l.TryLimit > 0 {
		tryLimit = strconv.Itoa(l.TryLimit)
	}
	_, _ = fmt.Fprintf(&b, `<input type=text name=codeCount value="%s"><input type=text name=tryLimit value="%s">`, needed, tryLimit)
	b.WriteString(`<input type=text name=penalty value="" id=penalty><input type=text name=greeting value="">`)
	_, _ = fmt.Fprintf(&b, `<input type=text name=lat value="%s"><input type=text name=lon value="%s"><input type=text name=radius value="%s">`, esc(l.Lat), esc(l.Lon), esc(l.Radius))
	b.WriteString(checkbox("noMasterCode", l.NoMasterCode))
	b.WriteString(checkbox("isSabotage", false))
	b.WriteString(checkbox("bonus", l.Bonus))
	_, _ = fmt.Fprintf(&b, `<input type=text name=bonusTime value="%d">`, l.BonusTime)
	b.WriteString(checkbox("skvoz", l.Skvoz))
	_, _ = fmt.Fprintf(&b, `<input type=text name=skvozMin value="%d"><input type=text name=skvozStart value="">`, l.SkvozMin)
	b.WriteString(checkbox("nobreak", l.NoBreak))
	b.WriteString(checkbox("clueBefore", true))
	b.WriteString(checkbox("zapas", false))
	b.WriteString(checkbox("publish", true))
	b.WriteString(`<input type=submit value="Сохранить"></form>`)
	return b.String()
}

func checkbox(name string, checked bool) string {
	if checked {
		return fmt.Sprintf(`<input type=checkbox name=%s checked> `, name)
	}
	return fmt.Sprintf(`<input type=checkbox name=%s> `, name)
}

// teamsPage renders the team list and the selected team's form.
func (s *server) teamsPage(q url.Values) string {
	g := s.state
	var b strings.Builder
	b.WriteString(`<table cellpadding=0 cellspacing=0 border=0 width=100%>`)
	b.WriteString(`<tr align=center class=news style="color:white" bgcolor=a1a1a1><td height=47 width=10></td><td><b>Капитан</b></td><td width=10></td><td align=left><b>Команда</b></td><td width=10></td><td><b>Статус заявки</b></td><td width=10></td><td></td><td></td><td><strong>Сыграно игр</strong></td><td></td><td nowrap><b>Скидка</b></td><td width=10></td><td nowrap><b>PIN</b></td><td width=10></td><td nowrap><b>рейтинговые очки</b></td><td width=10></td><td nowrap><b>лига</b></td><td colspan=3></td></tr>`)
	statuses := map[int]string{0: "не рассмотрена", 1: "принята", 2: "отклонена", 3: "вне зачета", 4: "автор", 5: "дисквалифицирована"}
	blocked := ""
	if g.Blocked {
		blocked = " bgcolor=F0F0F0"
	}
	_, _ = fmt.Fprintf(&b, `<tr valign=top bgcolor=FFFEDE%s><td></td><td class=news><a href=?action=users&edit=1&login=%s&categoryValue=Site&Project= class=news>%s</a></td><td width=10></td><td><table cellpadding=0 cellspacing=0><tr><td valign=top width=20></td><td width=10></td><td class=news width=400><b>%s</b></td></tr></table></td><td width=10></td><td class=news>%s</td><td width=10></td><td></td><td></td><td align=center class=news>7</td><td></td><td class=news align=right nowrap></td><td width=10></td><td class=news>%s</td><td width=10></td><td class=news align=right>%d</td><td width=10></td><td class=news nowrap>Первая</td><td width=20 align=right></td><td width=70 class=news align=left></td><td width=10></td></tr>`,
		blocked, esc(g.Captain), esc(g.Captain), esc(g.TeamName), statuses[g.Status], esc(g.Pin), g.RatingPoints)
	b.WriteString(`</table>`)

	// The engine's "add a team to this game" picker lists every team of the
	// city, which is the only place that directory is readable.
	b.WriteString(`<form method=post name=add action=admin.php><input type=hidden name=action value=addTeamToGame>`)
	_, _ = fmt.Fprintf(&b, `<input type=hidden name=categoryValue value=%d><select name=team><option value=0>`, g.GameID)
	for _, t := range g.CityTeams {
		_, _ = fmt.Fprintf(&b, `<option value=%d>%s`, t.ID, esc(t.Name))
	}
	b.WriteString(`</select><input type=submit value="Добавить"></form>`)

	// The engine prints a create-team form above the card and never closes
	// it, which is what makes the card unreachable through the tree.
	if s.has(quirkUnclosedForm) {
		b.WriteString(`<form method=post name=store action=admin.php><input type=hidden name=action value=newTeam><input type=text name=name value=''>`)
	}
	b.WriteString(`<form method=post name=frm>`)
	_, _ = fmt.Fprintf(&b, `<input type=hidden name=id value=%d>`, g.TeamID)
	b.WriteString(`<input type=hidden name=desc value=><input type=hidden name=Project value=>`)
	_, _ = fmt.Fprintf(&b, `<input type=hidden name=categoryValue value=%d>`, g.GameID)
	_, _ = fmt.Fprintf(&b, `<input type=hidden name=captain value=%s>`, esc(g.Captain))
	b.WriteString(`<input type=hidden name=ofset value=0><input type=hidden name=action value=updateTeam>`)
	_, _ = fmt.Fprintf(&b, `<input type=text name=name value='%s'>`, esc(g.TeamName))
	b.WriteString(`<select name=league><option value=1 selected>Первая</select>`)
	_, _ = fmt.Fprintf(&b, `<select name=newCap><option value='%s' selected>%s</select>`, esc(g.Captain), esc(g.Captain))
	b.WriteString(`<select name=shtab><option value='' selected>не назначен</option></select>`)
	b.WriteString(`<select name=captainHelper><option value='' selected>не назначен</option></select>`)
	b.WriteString(`<textarea name=history></textarea><input type=text name=deviz value='Мы тут просто мимо'><input type=text name=website value=''>`)
	// "Полный состав команды": one checkbox per member, ticked for the ones
	// the engine counts as being on the team, plus the nameless box it puts
	// at the head of the block for adding a player.
	b.WriteString(`<b class=news>Полный состав команды</b><br><input type=checkbox name=pl[]>`)
	for _, p := range g.Roster {
		_, _ = fmt.Fprintf(&b, `<input type=checkbox name=pl[%s]%s><a href=?action=users&edit=1&login=%s>%s</a><br />`,
			esc(p.Login), map[bool]string{true: " checked", false: ""}[p.OnTeam], esc(p.Login), esc(p.Login))
	}
	b.WriteString(checkbox("blocked", g.Blocked))
	b.WriteString(checkbox("autoApplication", true))
	b.WriteString(`<textarea name=changeLog></textarea>`)
	b.WriteString(`<select name=status>`)
	for _, code := range []int{0, 1, 2, 3, 4, 5} {
		sel := ""
		if code == g.Status {
			sel = " selected"
		}
		_, _ = fmt.Fprintf(&b, `<option value=%d%s>%s`, code, sel, statuses[code])
	}
	b.WriteString(`</select>`)
	_, _ = fmt.Fprintf(&b, `<input type=text name=pinlogin disabled value='%s'>`, esc(g.Pin))
	b.WriteString(`<input type=checkbox id=nPn name=newPIN> Сгенерировать новый`)
	b.WriteString(`<textarea name=comment></textarea>`)
	_, _ = fmt.Fprintf(&b, `<input type=text name=points value='%d'>`, g.RatingPoints)
	b.WriteString(checkbox("novice", false))
	b.WriteString(`<input type=submit value="Сохранить"></form>`)
	return page(b.String())
}

// handleAdminMonitor serves the live game screen and its interventions.
func (s *server) handleAdminMonitor(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		form, err := parseAdminForm(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.monitorWrite(form)
		adminRedirect(w, r, fmt.Sprintf("gmAdmin.php?finished=%s&Project=", form.Get("finished")))
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.state
	var b strings.Builder
	b.WriteString(`<form name=fnsh method=get><select name=finished onChange="document.fnsh.submit()">`)
	_, _ = fmt.Fprintf(&b, `<option value=%d selected>%s - %s`, g.GameID, g.Start.Format("2006-01-02"), esc(g.GameName))
	b.WriteString(`</select><select name=refresh><option value=15 selected>15 сек.</select></form>`)
	b.WriteString(`<form method=post name=gmform>`)
	_, _ = fmt.Fprintf(&b, `<input type=hidden name=finished value=%d>`, g.GameID)
	_, _ = fmt.Fprintf(&b, `<select name=team><option value=0>-- команда --<option value=%d>%s</select>`, g.TeamID, esc(g.TeamName))
	b.WriteString(`<select name=level><option value=0>-- уровень --`)
	for _, l := range g.Levels {
		_, _ = fmt.Fprintf(&b, `<option value=%d>%d. %s`, l.Order, l.Order, esc(l.Title))
	}
	b.WriteString(`</select><input type=text name=time value=""><input type=submit value="выдать"></form>`)
	engineButton := "остановить движок"
	if g.EngineStopped {
		engineButton = "включить движок"
	}
	_, _ = fmt.Fprintf(&b, `<form method=post><input type=hidden name=action value=switchRobot><input type=hidden name=finished value=%d><input type=submit value="%s"></form>`, g.GameID, engineButton)
	b.WriteString(`<table id=suxx>`)
	_, _ = fmt.Fprintf(&b, `<tr id=team%d><td>%s</td>`, g.TeamID, esc(g.TeamName))
	for _, l := range g.Levels {
		cell := ""
		if p := g.Progress[l.Order]; p != nil {
			cell = p.IssuedAt.Format("15:04:05")
		}
		_, _ = fmt.Fprintf(&b, `<td id=cell_%d_%d>%s</td>`, g.TeamID, l.Order, cell)
	}
	b.WriteString(`</tr></table>`)
	_, _ = fmt.Fprintf(&b, `<iframe name=LogFrame src=lookLog.php?gmid=%d&Project=&refresh=15></iframe>`, g.GameID)
	writeHTML(w, page(b.String()))
}

// monitorWrite applies an organizer intervention.
func (s *server) monitorWrite(form url.Values) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.state
	now := time.Now()
	teamID := parseInt(form.Get("team"))
	level := parseInt(form.Get("level"))
	switch form.Get("action") {
	case "newlevel":
		if l := g.level(level); l != nil {
			g.issueLevel(level, now)
			g.addLog(now, fmt.Sprintf("выдан уровень %d", level), strconv.Itoa(level), "admin")
		}
	case "acceptLevel", "newLevelAndAccept":
		if l := g.level(level); l != nil {
			if form.Get("action") == "newLevelAndAccept" {
				g.issueLevel(level, now)
			}
			if p := g.Progress[level]; p != nil {
				for _, code := range l.Codes {
					p.Codes[code] = true
				}
				g.addLog(now, "приняты все коды", strconv.Itoa(level), "admin")
			}
		}
	case "code":
		if p := g.Progress[level]; p != nil {
			code := strings.ToUpper(strings.TrimSpace(form.Get("code")))
			p.Codes[code] = true
			g.addLog(now, "принят код", code, "admin")
		}
	case "delLevel":
		if p := g.Progress[level]; p != nil {
			p.Codes = map[string]bool{}
		}
	case "removeLevel":
		delete(g.Progress, level)
		for i, o := range g.Issued {
			if o == level {
				g.Issued = append(g.Issued[:i], g.Issued[i+1:]...)
				break
			}
		}
		if g.Current == level {
			g.Current = 0
		}
	case "delstat":
		g.Progress = map[int]*levelProgress{}
		g.Issued = nil
		g.Current = 0
	case "bonus":
		minutes := parseInt(form.Get("mins"))
		if minutes < 0 {
			minutes = -minutes
		}
		if parseInt(form.Get("type")) > 0 {
			g.BonusMin += minutes
		} else {
			g.PenaltyMin += minutes
		}
		g.addLog(now, "начисление времени", form.Get("comment"), "admin")
	case "delBonus":
		if parseInt(form.Get("type")) > 0 {
			g.BonusMin = 0
		} else {
			g.PenaltyMin = 0
		}
	case "stop":
		g.Blocked = true
	case "start":
		g.Blocked = false
	case "switchRobot":
		// gmPost1.php reads no parameter here: the option is deleted when it
		// is set and inserted otherwise, so the button is a plain toggle.
		g.EngineStopped = !g.EngineStopped
	case "finishGame":
		g.Finished = true
	case "chtime":
		if p := g.Progress[level]; p != nil {
			if t, err := time.ParseInLocation("2006-01-02 15:04:05", form.Get("time"), time.Local); err == nil {
				p.IssuedAt = t
			}
		}
	}
	_ = teamID // the mock hosts a single team; the field is accepted for shape
}

// handleAdminLog answers the poll of the organizer's live log, packing the
// events into the meta-refresh URL the engine uses.
func (s *server) handleAdminLog(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if s.has(quirkAdminNoAccess) {
		writeHTML(w, adminNoAccessBody(""))
		return
	}
	s.mu.Lock()
	g := s.state
	since := r.URL.Query().Get("lastTime")
	var cutoff time.Time
	if since != "" {
		cutoff, _ = time.ParseInLocation("2006-01-02 15:04:05", since, time.Local)
	}
	var records []string
	last := cutoff
	for _, e := range g.Log {
		if !cutoff.IsZero() && !e.Time.After(cutoff) {
			continue
		}
		event := logEventCode(e.Event)
		records = append(records, fmt.Sprintf("%d-%d-%d-%s-%s", g.TeamID, e.Level, event, e.Data, e.Time.Format("15:04:05")))
		if e.Time.After(last) {
			last = e.Time
		}
	}
	if last.IsZero() {
		last = time.Now()
	}
	gmid := r.URL.Query().Get("gmid")
	refresh := r.URL.Query().Get("refresh")
	if refresh == "" {
		refresh = "15"
	}
	s.mu.Unlock()

	target := fmt.Sprintf("http://%s/%s/admin/lookLog.php?lastTime=%s&gmid=%s&refresh=%s&Project=&opt=%s",
		r.Host, s.city, url.QueryEscape(last.Format("2006-01-02 15:04:05")), gmid, refresh, url.QueryEscape(strings.Join(records, "|")))
	// The engine writes this URL raw, ampersands and all, so the client sees
	// the same thing it must parse in production.
	writeHTML(w, fmt.Sprintf("<head>\n<meta http-equiv=\"Refresh\" content=\"%s;URL=%s\">\n</head>\n<body bgcolor=\"f0f0f0\" >\n</body>", refresh, target))
}

// logEventCode maps the mock's log wording back to the engine's event codes.
func logEventCode(event string) int {
	switch {
	case strings.HasPrefix(event, "выдан уровень"):
		return 1
	case strings.HasPrefix(event, "принят код"), strings.HasPrefix(event, "приняты все коды"):
		return 2
	case strings.HasPrefix(event, "выдано сквозное"):
		return 3
	case strings.Contains(event, "первой подсказки"):
		return 4
	case strings.Contains(event, "второй подсказки"):
		return 5
	case strings.Contains(event, "неверный код"):
		return 6
	case strings.Contains(event, "спойлер"):
		return 11
	}
	return 0
}

// handleAdminChat serves the organizer chat and its writes.
func (s *server) handleAdminChat(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if s.has(quirkAdminNoAccess) {
		user, _, _ := r.BasicAuth()
		writeHTML(w, adminNoAccessBody(user))
		return
	}
	if r.Method == http.MethodPost {
		form, err := parseAdminForm(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.chatWrite(form)
		adminRedirect(w, r, fmt.Sprintf("gmcht.php?Project=&gm=%s", form.Get("gm")))
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.state
	var b strings.Builder
	b.WriteString(`<form method=post><input type=hidden name=action value=addMessage><input type=hidden name=Project value=>`)
	_, _ = fmt.Fprintf(&b, `<input type=hidden name=gm value=%d>`, g.GameID)
	b.WriteString(`<textarea name=content></textarea>`)
	_, _ = fmt.Fprintf(&b, `<select name=team><option value=0>-- ВСЕМ --<option value=%d>%s</select>`, g.TeamID, esc(g.TeamName))
	b.WriteString(`<input type=checkbox name=important> ВАЖНОЕ!<input type=text name=showafter value=""><input type=submit value="Отправить сообщение"></form>`)
	b.WriteString(`<h3>Сообщения</h3><table cellpadding=3 cellspacing=0 border=1>`)
	for i := len(g.OrgMessages) - 1; i >= 0; i-- {
		m := g.OrgMessages[i]
		bg := "#ffcccc"
		addressee := "всем"
		if m.Team != 0 {
			addressee = esc(g.TeamName)
		}
		if m.Team < 0 {
			// A negative team is what the team sent to the organizer.
			bg = "#99cccc"
		}
		ts := m.Time.Format("2006-01-02 15:04:05")
		_, _ = fmt.Fprintf(&b, `<tr style='background-color:%s'><td>%s</td><td><strong>%s</strong></td><td>%s</td><td>%s</td><form method=post><td><input type=hidden name=Project value=><input type=hidden name=gm value=%d><input type=hidden name=action value=delMessage><input type=hidden name=tm value='%s'><input type=submit value='del'></td></form></tr>`,
			bg, m.Time.Format("15:04:05"), addressee, esc(m.Text), showAfterCell(m), g.GameID, ts)
	}
	b.WriteString(`</table>`)
	b.WriteString(`<form method=post><input type=hidden name=Project value=><input type=hidden name=action value=delAllMessages>`)
	_, _ = fmt.Fprintf(&b, `<input type=hidden name=gm value=%d><input type=submit value="Удалить все сообщения"></form>`, g.GameID)
	writeHTML(w, page(b.String()))
}

// showAfterCell renders the fourth column of the chat: the moment a delayed
// message becomes visible. The engine shows that here, not the important
// flag, which it stores but never prints.
func showAfterCell(m orgMessage) string {
	if m.ShowAfter.IsZero() {
		return ""
	}
	return m.ShowAfter.Format("2006-01-02 15:04:05")
}

// chatWrite applies an organizer chat write.
func (s *server) chatWrite(form url.Values) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.state
	switch form.Get("action") {
	case "addMessage":
		now := time.Now()
		msg := orgMessage{
			Time: now, Team: parseInt(form.Get("team")), Text: form.Get("content"),
			User: "организатор", Important: form.Get("important") != "",
		}
		if v := strings.TrimSpace(form.Get("showafter")); v != "" {
			if t, err := time.ParseInLocation("2006-01-02 15:04:05", v, time.Local); err == nil {
				msg.Time = t
				msg.ShowAfter = t
			}
		}
		g.OrgMessages = append(g.OrgMessages, msg)
	case "delMessage":
		ts := form.Get("tm")
		for i, m := range g.OrgMessages {
			if m.Time.Format("2006-01-02 15:04:05") == ts {
				g.OrgMessages = append(g.OrgMessages[:i], g.OrgMessages[i+1:]...)
				break
			}
		}
	case "delAllMessages":
		g.OrgMessages = nil
	}
}
