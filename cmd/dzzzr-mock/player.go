package main

import (
	"bytes"
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// Auth-error envelopes. APIconst.php answers with these, and with HTTP 200
// for a bad session token, so a client cannot tell them apart by status.
const (
	errNoCaptain = `{"error" : "Ошибка авторизации. Не введено имя капитана"}`
	errNoSession = `{"error" : "Ошибка авторизации. Неверный идентификатор сессии"}`
)

// registerPlayer mounts the endpoints a team uses.
func (s *server) registerPlayer(mux *http.ServeMux, prefix string) {
	mux.HandleFunc(prefix+"API/login.php", s.handleLogin)
	mux.HandleFunc(prefix+"API/game.php", s.handleGameAPI)
	mux.HandleFunc(prefix+"API/messages.php", s.handleMessages)
	mux.HandleFunc(prefix+"API/postmessage.php", s.handlePostMessage)
	mux.HandleFunc(prefix+"API/gamesList.php", s.handleGamesList)
	mux.HandleFunc(prefix+"go/", s.handleGo)
}

// writeJSON sends a value as the engine does: JSON, UTF-8, no HTML escaping
// (the engine embeds raw markup in its strings).
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(buf.Bytes())
}

// writeRaw sends a body the mock assembled itself.
func writeRaw(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

// jsonString quotes a string for hand-assembled JSON, leaving markup alone.
func jsonString(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return `""`
	}
	return strings.TrimRight(buf.String(), "\n")
}

// basicCredentials returns the game PIN from HTTP Basic. The engine expects
// "{city}_{captain}" as the user name and the PIN as the password.
func (s *server) basicCredentials(r *http.Request) (user, pass string, ok bool) {
	if user, pass, ok = r.BasicAuth(); ok && user != "" {
		return user, pass, true
	}
	// APIconst.php also accepts the pair in a "pin={user}:{password}" query
	// parameter, for clients that cannot set the header.
	if v := r.URL.Query().Get("pin"); v != "" {
		user, pass, _ = strings.Cut(v, ":")
		if user != "" {
			return user, pass, true
		}
	}
	return "", "", false
}

// requireBasic enforces the HTTP Basic wall in front of the API.
//
// The engine only checks that a header is present: APIconst.php looks the
// team's application up by the password and then accepts the request anyway
// ("|| true"). Classic teams have no PIN at all, so validating one here would
// make the mock stricter than the engine and let a client pass tests it would
// fail in production. A missing header is 401 with the "no captain name"
// envelope, as on the live server.
func (s *server) requireBasic(w http.ResponseWriter, r *http.Request) bool {
	if _, _, ok := s.basicCredentials(r); !ok {
		writeUnauthorized(w, errNoCaptain)
		return false
	}
	return true
}

// writeUnauthorized answers with the 401 the engine's Basic wall sends.
func writeUnauthorized(w http.ResponseWriter, body string) {
	w.Header().Set("WWW-Authenticate", `Basic realm="DozoR API"`)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(body))
}

// requireSession resolves the s= token. An unknown token answers 200 with the
// auth envelope, which is what APIconst.php does.
func (s *server) requireSession(w http.ResponseWriter, r *http.Request) (*session, bool) {
	token := r.URL.Query().Get("s")
	s.mu.Lock()
	sess := s.sessions[token]
	s.mu.Unlock()
	if token == "" || sess == nil {
		writeRaw(w, errNoSession)
		return nil, false
	}
	return sess, true
}

// networkDown answers a request that falls inside a simulated outage. The
// engine has no such state; it imitates the PZDC code teams use to test how
// their software survives a dead connection.
func (s *server) networkDown(w http.ResponseWriter, r *http.Request, sess *session) bool {
	s.mu.Lock()
	until := sess.NetDown
	s.mu.Unlock()
	now := time.Now()
	if until.IsZero() || !now.Before(until) {
		return false
	}
	wait := networkDownSleep
	if rest := until.Sub(now); rest < wait {
		wait = rest
	}
	sleepCtx(r, wait)
	http.Error(w, "mock: network is down", http.StatusBadGateway)
	return true
}

// sleepCtx waits for d unless the client hangs up first.
func sleepCtx(r *http.Request, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-r.Context().Done():
	}
}

// beginPlayer runs the checks every team endpoint shares: session token,
// simulated outage, HTTP Basic.
func (s *server) beginPlayer(w http.ResponseWriter, r *http.Request) (*session, bool) {
	sess, ok := s.requireSession(w, r)
	if !ok {
		return nil, false
	}
	if s.networkDown(w, r, sess) {
		return nil, false
	}
	if !s.requireBasic(w, r) {
		return nil, false
	}
	return sess, true
}

// newToken builds the 32 upper-case letters API/login.php hands out.
func newToken() string {
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte('A' + rand.IntN(26))
	}
	return string(b)
}

// handleLogin implements API/login.php: it signs a site account in and
// returns the session token every other endpoint needs.
func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.requireBasic(w, r) {
		return
	}
	q := r.URL.Query()
	login, password := q.Get("login"), q.Get("password")
	if login == "" || password == "" {
		writeRaw(w, `{"error" : "Неверные параметры", "code" : "6"}`)
		return
	}
	if login == "fail" && password == "fail" {
		writeRaw(w, `{"error" : "Неверный пользователь или пароль. Проверьте личные данные.", "code" : "4"}`)
		return
	}
	token := newToken()
	s.mu.Lock()
	s.sessions[token] = &session{Token: token, Login: login, CreatedAt: time.Now()}
	s.mu.Unlock()
	writeRaw(w, `{"userName": `+jsonString(login)+`, "error" : "Вы успешно авторизовались", "userToken" : "`+token+`", "code" : "2"}`)
}

// handleGo implements go/: the game page as JSON on GET, the engine actions
// on POST.
func (s *server) handleGo(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.beginPlayer(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		s.handleGoPost(w, r, sess)
		return
	}
	if s.has(quirkNoGame) {
		// templates/JSON/go_nogame.tpl: a numeric error and the wording
		// beside it, with HTTP 200.
		writeRaw(w, "{\n    \"error\": 2,\n    \"errorText\": \"Мы рады, что вы заглянули. В настоящее время вы не заявлены ни в одной из ближайших игр.\"\n}")
		return
	}
	errNo, _ := strconv.Atoi(r.URL.Query().Get("err"))
	now := time.Now()
	s.mu.Lock()
	s.state.tick(now)
	body := s.renderGoState(s.state, sess, errNo, now)
	s.mu.Unlock()
	if s.has(quirkNoise) {
		// The demo town prints a leftover var_dump ahead of the payload.
		body = "<!--string(11) \"dzzzr_ndemo\"\n-->" + body
	}
	writeRaw(w, body)
}

// handleGoPost runs one engine action and answers with the redirect whose
// query carries the result code, exactly as go2.php does.
func (s *server) handleGoPost(w http.ResponseWriter, r *http.Request, sess *session) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	api := r.URL.Query().Has("api")
	// go2.php only acts on POSTs that come from the site itself or carry
	// api=true; anything else is bounced back to the game page untouched.
	if !api && !strings.Contains(r.Header.Get("Referer"), "dzzzr.ru") {
		s.redirect(w, "", false, 0)
		return
	}
	action := r.PostForm.Get("action")
	now := time.Now()

	s.mu.Lock()
	s.state.tick(now)
	res := s.doAction(sess, action, r.PostForm, now)
	s.mu.Unlock()

	if res.netDown {
		sleepCtx(r, networkDownDuration)
		http.Error(w, "mock: network is down", http.StatusBadGateway)
		return
	}
	code := ""
	if res.err != 0 {
		code = strconv.Itoa(res.err)
	}
	s.redirect(w, code, api && action == "entcod", res.penalty)
}

// redirect answers with the game-page redirect the engine sends after every
// action. Only entcod carries api=true through to the target.
func (s *server) redirect(w http.ResponseWriter, errCode string, api bool, penalty int) {
	loc := "/" + s.city + "/go/?nostat=&notext=&notags=&log=&legend=&err=" + errCode + "&bonus=&kladMap="
	// go/errors.php interpolates $_GET[bcs] into the text of code 42, so the
	// engine reports an early hint's penalty only in the redirect.
	if penalty > 0 {
		loc += "&bcs=" + strconv.Itoa(penalty)
	}
	if api {
		loc += "&api=true"
	}
	w.Header().Set("Location", loc)
	w.WriteHeader(http.StatusFound)
}

// actionOutcome is what one engine action produced: a result code, or the
// signal that the connection has to be held for a simulated outage.
type actionOutcome struct {
	err     int
	penalty int // minutes, reported as bcs in the redirect
	netDown bool
}

// doAction dispatches one POST action. The caller holds the mutex.
func (s *server) doAction(sess *session, action string, form url.Values, now time.Time) actionOutcome {
	switch action {
	case "entcod":
		return s.entcod(sess, form, now)
	case "abandon":
		return s.abandon(sess, now)
	case "break":
		return s.takeBreak(sess, now)
	case "breakStop":
		return s.stopBreak(sess, now)
	case "takeClue":
		return s.takeClue(sess, form, now)
	case "beforeClue":
		return s.beforeClue(sess, form, now)
	case "nextLevel":
		return s.nextLevel(sess, form, now)
	case "selectlevel":
		return s.selectLevel(sess, form, now)
	case "spoilerCode":
		return s.spoilerCode(sess, form, now)
	case "send_message":
		return s.sendMessage(sess, form, now)
	case "auth":
		if form.Get("login") == "fail" {
			return actionOutcome{err: dzzzr.ErrWrongPassword}
		}
		return actionOutcome{err: dzzzr.ErrLoginOK}
	case "saveOptions":
		return actionOutcome{}
	default:
		return actionOutcome{}
	}
}

// isCaptain reports whether the signed-in account may drive the game.
func (g *gameState) isCaptain(sess *session) bool {
	return sess != nil && sess.Login == g.Captain
}

// entcod is the code-entry action, the heart of the engine.
func (s *server) entcod(sess *session, form url.Values, now time.Time) actionOutcome {
	g := s.state
	if g.EngineStopped {
		return actionOutcome{err: dzzzr.ErrEngineStopped}
	}
	cod := normalizeCode(form.Get("cod"))
	if cod == "" {
		return actionOutcome{err: dzzzr.ErrEmptyCode}
	}
	if cod == "PZDC" {
		sess.NetDown = now.Add(networkDownDuration)
		g.addLog(now, "имитация потери связи", cod, sess.Login)
		return actionOutcome{netDown: true}
	}
	if now.Before(g.Start) {
		return actionOutcome{err: dzzzr.ErrGameNotStartedYet}
	}
	if g.Blocked || g.onBreak(now) {
		return actionOutcome{err: dzzzr.ErrTeamPaused}
	}

	skvozReq := form.Get("skvoz") != ""
	var l *levelDef
	var p *levelProgress
	if skvozReq {
		order, _ := strconv.Atoi(form.Get("level"))
		l, p = g.level(order), g.Progress[order]
		if l == nil || p == nil || !l.Bonus {
			return actionOutcome{err: dzzzr.ErrSkvozCodeRejected}
		}
	} else {
		l, p = g.current()
		if l == nil {
			if g.Finished {
				return actionOutcome{err: dzzzr.ErrGameOver}
			}
			return actionOutcome{err: dzzzr.ErrNoLevelPlanned}
		}
	}

	if p.BlockedUntil.After(now) {
		return actionOutcome{err: dzzzr.ErrTooManyAttempts}
	}

	// The universal code of the game opens any level that allows it.
	if cod == normalizeCode(g.MasterCode) && g.MasterCode != "" {
		if skvozReq {
			return actionOutcome{err: dzzzr.ErrMasterCodeInSkvoz}
		}
		if l.NoMasterCode {
			return actionOutcome{err: dzzzr.ErrMasterCodeForbidden}
		}
		if g.MasterUsed {
			return actionOutcome{err: dzzzr.ErrMasterCodeUsed}
		}
		g.MasterUsed = true
		p.MasterCode = true
		g.PenaltyMin += g.MasterCodePenalty
		g.addLog(now, "принят универсальный код", cod, sess.Login)
		return s.afterMainCode(l, p, sess, now, false, true)
	}

	// A spoiler code entered in the ordinary field still unlocks the spoiler.
	for _, sp := range l.Spoilers {
		if matchCode(cod, []string{sp.Code}) == "" {
			continue
		}
		if !p.Spoilers[sp.Order] {
			p.Spoilers[sp.Order] = true
			g.PenaltyMin += sp.Penalty
		}
		g.addLog(now, "введен правильный код к спойлеру", cod, sess.Login)
		return actionOutcome{err: dzzzr.ErrSpoilerAccepted}
	}

	// The time allowed for the level runs out before any code is examined.
	if skvozReq && l.SkvozMin > 0 && now.After(p.IssuedAt.Add(time.Duration(l.SkvozMin)*time.Minute)) {
		return actionOutcome{err: dzzzr.ErrSkvozTimeUp}
	}
	if !skvozReq && g.expired(l, p, now) {
		g.addLog(now, "истекло время", cod, sess.Login)
		p.CompletedAt = now
		if g.nextMainOrder() == 0 {
			g.Current = 0
			g.Finished = true
			return actionOutcome{err: dzzzr.ErrTimeUp}
		}
		g.advance(now)
		return actionOutcome{err: dzzzr.ErrLevelTimeUp}
	}

	if primary := matchCode(cod, l.Codes); primary != "" {
		if p.Codes[primary] {
			g.addLog(now, "попытка повторного ввода уже принятого кода", cod, sess.Login)
			return actionOutcome{err: dzzzr.ErrCodeRepeated}
		}
		if g.bonusPending(l, p) {
			g.addLog(now, "отправлен не обязательный основной код", cod, sess.Login)
			return actionOutcome{err: dzzzr.ErrOptionalCode}
		}
		p.Codes[primary] = true
		g.addLog(now, "принят код", primary, sess.Login)
		return s.afterMainCode(l, p, sess, now, skvozReq, false)
	}

	if primary := matchCode(cod, l.BonusCodes); primary != "" {
		if p.BonusCodes[primary] {
			g.addLog(now, "повторно отправлен бонусный код", cod, sess.Login)
			return actionOutcome{err: dzzzr.ErrBonusRepeated}
		}
		p.BonusCodes[primary] = true
		g.BonusMin += l.bonusMinutesFor(primary)
		g.addLog(now, "принят бонусный код", primary, sess.Login)
		if skvozReq {
			return actionOutcome{err: dzzzr.ErrSkvozBonusAccepted}
		}
		return actionOutcome{err: dzzzr.ErrBonusAccepted}
	}

	if primary := matchCode(cod, l.FakeCodes); primary != "" {
		if p.FakeCodes[primary] {
			g.addLog(now, "повторно отправлен ложный код", cod, sess.Login)
			return actionOutcome{err: dzzzr.ErrCodeRepeated}
		}
		p.FakeCodes[primary] = true
		g.PenaltyMin += l.fakePenaltyFor(primary)
		g.addLog(now, "принят ложный код", primary, sess.Login)
		return actionOutcome{err: dzzzr.ErrFakeCode}
	}

	p.Wrong++
	if p.Wrong >= wrongTriesBeforeBlock {
		p.BlockedUntil = now.Add(wrongCodeBlock)
	}
	g.addLog(now, "отправлен неверный код", cod+" / "+strings.Join(l.Codes, "|"), sess.Login)
	if skvozReq {
		return actionOutcome{err: dzzzr.ErrSkvozCodeRejected}
	}
	return actionOutcome{err: dzzzr.ErrCodeRejected}
}

// afterMainCode reports what the engine says once a main code was accepted,
// and hands over the next level when the current one is done.
func (s *server) afterMainCode(l *levelDef, p *levelProgress, sess *session, now time.Time, skvozReq, master bool) actionOutcome {
	g := s.state
	if !g.mainDone(l, p) {
		switch {
		case skvozReq:
			return actionOutcome{err: dzzzr.ErrSkvozPartial}
		case master:
			return actionOutcome{err: dzzzr.ErrMasterCodePartial}
		default:
			return actionOutcome{err: dzzzr.ErrCodeAcceptedPartial}
		}
	}
	if g.bonusPending(l, p) {
		return actionOutcome{err: dzzzr.ErrAllMainCodesFound}
	}
	p.CompletedAt = now
	if skvozReq || l.Bonus {
		g.addLog(now, "сквозное задание выполнено", strconv.Itoa(l.Order), sess.Login)
		return actionOutcome{err: dzzzr.ErrSkvozCodeAccepted}
	}
	g.addLog(now, "приняты все коды", strconv.Itoa(l.Order), sess.Login)
	g.advance(now)
	if master {
		return actionOutcome{err: dzzzr.ErrMasterCodeNext}
	}
	return actionOutcome{err: dzzzr.ErrCodeAcceptedNext}
}

// abandon gives up the current level and asks for the next one.
func (s *server) abandon(sess *session, now time.Time) actionOutcome {
	g := s.state
	if !g.isCaptain(sess) {
		return actionOutcome{err: dzzzr.ErrNoPermission}
	}
	if s.has(quirkSilentRefusal) {
		// The live engine reported canAbandon empty and answered the whole
		// state with errNo 0, saying nothing about the refusal.
		return actionOutcome{}
	}
	if g.Abandons >= 2 {
		return actionOutcome{err: dzzzr.ErrAbandonsExhausted}
	}
	l, p := g.current()
	if l == nil {
		return actionOutcome{err: dzzzr.ErrNoNextLevel}
	}
	if g.nextMainOrder() == 0 {
		return actionOutcome{err: dzzzr.ErrNoNextLevel}
	}
	g.Abandons++
	p.CompletedAt = now
	g.addLog(now, "отказ от уровня", strconv.Itoa(l.Order), sess.Login)
	g.advance(now)
	return actionOutcome{}
}

// takeBreak schedules the 15-minute break that starts once the level ends.
func (s *server) takeBreak(sess *session, now time.Time) actionOutcome {
	g := s.state
	if !g.isCaptain(sess) {
		return actionOutcome{err: dzzzr.ErrNoPermission}
	}
	if g.Breaks >= 2 {
		return actionOutcome{err: dzzzr.ErrBreaksExhausted}
	}
	g.Breaks++
	g.BreakPending = g.Current
	g.addLog(now, "взятие перерыва", strconv.Itoa(g.Current), sess.Login)
	return actionOutcome{}
}

// stopBreak ends a running break early.
func (s *server) stopBreak(sess *session, now time.Time) actionOutcome {
	g := s.state
	if !g.isCaptain(sess) {
		return actionOutcome{err: dzzzr.ErrNoPermission}
	}
	g.BreakPending = 0
	g.BreakUntil = time.Time{}
	g.addLog(now, "досрочное возобновление игры после перерыва", "", sess.Login)
	g.tick(now)
	return actionOutcome{}
}

// takeClue requests a hint that the level issues on demand: event 4 is the
// first hint, event 5 the second.
func (s *server) takeClue(sess *session, form url.Values, now time.Time) actionOutcome {
	g := s.state
	if !g.isCaptain(sess) {
		return actionOutcome{err: dzzzr.ErrNoPermission}
	}
	order, _ := strconv.Atoi(form.Get("level"))
	p := g.Progress[order]
	if p == nil {
		return actionOutcome{}
	}
	if form.Get("event") == "5" {
		p.Hint2Taken = true
		g.addLog(now, "запрос второй подсказки", strconv.Itoa(order), sess.Login)
	} else {
		p.Hint1Taken = true
		g.addLog(now, "запрос первой подсказки", strconv.Itoa(order), sess.Login)
	}
	return actionOutcome{}
}

// beforeClue takes the next hint of the current level ahead of time, for the
// game's early-hint penalty plus the minutes skipped.
// beforeClue issues the hint the form names, before its time and for a
// penalty.
//
// The clue field decides which one. A caller that omits it does not get the
// next hint: the live engine, asked without it on a level whose first hint
// was still due, issued the second one, charged for it and left the first
// unissued. The mock reproduces that so a client which forgets the field
// fails here the way it fails against the engine.
func (s *server) beforeClue(sess *session, form url.Values, now time.Time) actionOutcome {
	g := s.state
	if !g.isCaptain(sess) {
		return actionOutcome{err: dzzzr.ErrNoPermission}
	}
	l, p := g.current()
	if l == nil {
		return actionOutcome{}
	}
	elapsed := now.Sub(p.IssuedAt)
	first := time.Duration(l.ClueMin) * time.Minute
	second := time.Duration(l.ClueMin+l.ClueMin2) * time.Minute

	if form.Get("clue") == "1" && p.Hint1Early.IsZero() && !p.Hint1Taken && elapsed < first {
		p.Hint1Early = now
		penalty := g.ClueBeforePenalty + int((first-elapsed+time.Minute-1)/time.Minute)
		g.PenaltyMin += penalty
		g.addLog(now, "досрочное получение первой подсказки", strconv.Itoa(l.Order), sess.Login)
		return actionOutcome{err: dzzzr.ErrEarlyHintPenalty, penalty: penalty}
	}
	if p.Hint2Early.IsZero() && !p.Hint2Taken {
		left := second - elapsed
		if left < 0 {
			left = 0
		}
		p.Hint2Early = now
		penalty := g.ClueBeforePenalty + int((left+time.Minute-1)/time.Minute)
		g.PenaltyMin += penalty
		g.addLog(now, "досрочное получение второй подсказки", strconv.Itoa(l.Order), sess.Login)
		return actionOutcome{err: dzzzr.ErrEarlyHintPenalty, penalty: penalty}
	}
	return actionOutcome{err: dzzzr.ErrEarlyHintPenalty}
}

// nextLevel moves on once the main codes are in but bonus codes remain.
func (s *server) nextLevel(sess *session, form url.Values, now time.Time) actionOutcome {
	g := s.state
	if !g.isCaptain(sess) {
		return actionOutcome{err: dzzzr.ErrNoPermission}
	}
	order, _ := strconv.Atoi(form.Get("level"))
	p := g.Progress[order]
	if p == nil {
		return actionOutcome{}
	}
	p.NextRequest = true
	g.addLog(now, "переход на следующий уровень до ввода всех бонусных кодов", strconv.Itoa(order), sess.Login)
	if l := g.level(order); l != nil && order == g.Current && g.mainDone(l, p) {
		p.CompletedAt = now
		g.advance(now)
	}
	return actionOutcome{}
}

// selectLevel picks the next level in games that let the team choose. A stale
// choice — llevel no longer the current level — is ignored, as in go2.php.
func (s *server) selectLevel(sess *session, form url.Values, now time.Time) actionOutcome {
	g := s.state
	if !g.isCaptain(sess) {
		return actionOutcome{err: dzzzr.ErrNoPermission}
	}
	llevel, _ := strconv.Atoi(form.Get("llevel"))
	if llevel != g.Current {
		return actionOutcome{}
	}
	order, _ := strconv.Atoi(form.Get("level"))
	l := g.level(order)
	if l == nil || l.Bonus || g.Progress[order] != nil {
		return actionOutcome{}
	}
	if p := g.Progress[g.Current]; p != nil {
		p.CompletedAt = now
	}
	g.issueLevel(order, now)
	g.addLog(now, "самостоятельный выбор уровня", strconv.Itoa(order), sess.Login)
	return actionOutcome{}
}

// spoilerCode unlocks a spoiler of the level named by the skvoz field.
func (s *server) spoilerCode(sess *session, form url.Values, now time.Time) actionOutcome {
	g := s.state
	if now.Before(g.Start) {
		return actionOutcome{err: dzzzr.ErrGameNotStartedYet}
	}
	order := g.Current
	if v, err := strconv.Atoi(form.Get("skvoz")); err == nil && v > 0 {
		order = v
	}
	l, p := g.level(order), g.Progress[order]
	if l == nil || p == nil || len(l.Spoilers) == 0 {
		return actionOutcome{}
	}
	code := form.Get("spoilerCode")
	for _, sp := range l.Spoilers {
		if matchCode(code, []string{sp.Code}) == "" {
			continue
		}
		if !p.Spoilers[sp.Order] {
			p.Spoilers[sp.Order] = true
			g.PenaltyMin += sp.Penalty
		}
		g.addLog(now, "введен правильный код к спойлеру", normalizeCode(code), sess.Login)
		return actionOutcome{err: dzzzr.ErrSpoilerAccepted}
	}
	g.addLog(now, "введен неверный код к спойлеру", normalizeCode(code), sess.Login)
	return actionOutcome{err: dzzzr.ErrSpoilerRejected}
}

// sendMessage posts a message to the organizer from the game interface.
func (s *server) sendMessage(sess *session, form url.Values, now time.Time) actionOutcome {
	g := s.state
	if !g.isCaptain(sess) {
		return actionOutcome{err: dzzzr.ErrNoPermission}
	}
	text := strings.TrimSpace(form.Get("content"))
	g.OrgMessages = append(g.OrgMessages, orgMessage{Time: now, Team: -g.TeamID, Text: text, User: sess.Login})
	g.addLog(now, "отправлено сообщение организатору", text, sess.Login)
	return actionOutcome{err: dzzzr.ErrMessageSent}
}

// nextLevelSelector renders the level-choice form the engine embeds in the
// game state as a string. Games with a fixed order send an empty one, which
// is why a client that never reads it looks correct until it meets a game
// that does.
func (s *server) nextLevelSelector(g *gameState) string {
	if !s.has(quirkChooseLevel) {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<form method=post><input name="action" type="hidden" value="selectlevel">`)
	b.WriteString(`<input name="llevel" type="hidden" value=` + strconv.Itoa(g.Current) + ">")
	for i, l := range g.mainLine() {
		checked := ""
		if i == 0 {
			checked = " checked"
		}
		b.WriteString(`<input type=radio name=level value=` + strconv.Itoa(l.Order) + checked + ">" + l.Title + "<br>")
	}
	b.WriteString(`<input type=submit value="выбрать уровень"></form>`)
	return b.String()
}

// renderGoState builds the go/?api=true document, field for field as
// templates/JSON/go.tpl renders it: numbers arrive quoted, a missing level is
// the placeholder "{ }" and the bonus list always ends with one.
func (s *server) renderGoState(g *gameState, sess *session, errNo int, now time.Time) string {
	var b strings.Builder
	started := !now.Before(g.Start)

	startTime, startDay, startAt := "", "", ""
	if !started {
		startTime = now.Format("15:04")
		startDay = formatDay(g.Start)
		startAt = g.Start.Format("15:04")
	}
	greeting := ""
	if g.Finished {
		greeting = g.Greeting
	}
	lastOrg := ""
	if n := len(g.OrgMessages); n > 0 {
		lastOrg = g.OrgMessages[n-1].Text
	}
	canControl := ""
	if g.isCaptain(sess) {
		canControl = "1"
	}
	countdown, onBreak := "", ""
	if g.onBreak(now) {
		countdown = strconv.Itoa(int(g.BreakUntil.Sub(now) / time.Second))
		onBreak = formatTime(g.BreakUntil)
	}
	blockedError := ""
	if g.Blocked {
		blockedError = dzzzr.ErrText(dzzzr.ErrTeamPaused)
	}

	b.WriteString("{\n")
	b.WriteString(`  "gameName": ` + jsonString(g.GameName) + ",\n")
	b.WriteString(`  "greeting": ` + jsonString(greeting) + ",\n")
	b.WriteString(`  "lastOrgMessage": ` + jsonString(lastOrg) + ",\n")
	b.WriteString(`  "gameId": "` + strconv.Itoa(g.GameID) + "\",\n")
	b.WriteString(`  "teamName": ` + jsonString(g.TeamName) + ",\n")
	b.WriteString(`  "gameStartTime": ` + jsonString(startTime) + ",\n")
	b.WriteString(`  "gameStartOnDay": ` + jsonString(startDay) + ",\n")
	b.WriteString(`  "gameStartOnTime": ` + jsonString(startAt) + ",\n")
	b.WriteString(`  "holdTime": "",` + "\n")
	b.WriteString(`  "finished": ` + strconv.FormatBool(g.Finished) + ",\n")
	b.WriteString(`  "countdown": ` + jsonString(countdown) + ",\n")
	b.WriteString(`  "onBreak": ` + jsonString(onBreak) + ",\n")
	b.WriteString(`  "currentTime": ` + jsonString(formatTime(now)) + ",\n")
	b.WriteString(`  "legend": ` + jsonString(g.Legend) + ",\n")
	b.WriteString(`  "canControl": ` + jsonString(canControl) + ",\n")
	b.WriteString(`  "totallevels": "` + strconv.Itoa(len(g.mainLine())) + "\",\n")
	b.WriteString(`  "tryLimitError": "",` + "\n")
	b.WriteString(`  "blockedError": ` + jsonString(blockedError) + ",\n")
	b.WriteString(`  "NextLevelSelector": ` + jsonString(s.nextLevelSelector(g)) + ",\n")

	level := "{ }"
	if started && !g.Finished {
		if l, p := g.current(); l != nil {
			level = s.renderLevel(g, l, p, now)
			// templates/JSON/go_level.tpl ends with
			// "}{if $r1.skvoz != ''},{/if}" while go.tpl writes its own
			// separator, so on a сквозное level the engine emits "},," and
			// the reply is not valid JSON. Reproduced here because a client
			// that cannot read it cannot play such a level at all.
			if l.Skvoz {
				level += ","
			}
		}
	}
	b.WriteString(`  "level": ` + level + ",\n")

	b.WriteString(`  "bonusLevels": [ `)
	if started && !g.Finished {
		if l, p := g.skvoz(); l != nil {
			b.WriteString(s.renderLevel(g, l, p, now) + ", ")
		}
	}
	b.WriteString("{ }],\n")

	b.WriteString("  \"messages\": [\n")
	visible := g.visibleOrgMessages()
	for i, m := range visible {
		b.WriteString("    {\n")
		b.WriteString(`    "timestamp": ` + jsonString(m.Time.Format("15:04:05")) + ",\n")
		b.WriteString(`    "destination": ` + jsonString(messageDestination(m)) + ",\n")
		b.WriteString(`    "text": ` + jsonString(m.Text) + "\n")
		b.WriteString("    }")
		if i != len(visible)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("  ],\n")

	b.WriteString(`  "bonusSkvozLevels": "",` + "\n")
	b.WriteString(`  "errNo": ` + strconv.Itoa(errNo) + ",\n")
	b.WriteString(`  "errText": ` + jsonString(dzzzr.ErrText(errNo)) + "\n")
	b.WriteString("}")
	return b.String()
}

// visibleOrgMessages returns the messages addressed to this team, oldest
// first.
func (g *gameState) visibleOrgMessages() []orgMessage {
	out := make([]orgMessage, 0, len(g.OrgMessages))
	for _, m := range g.OrgMessages {
		if m.Team == 0 || m.Team == g.TeamID || m.Team == -g.TeamID {
			out = append(out, m)
		}
	}
	return out
}

// messageDestination names the audience of a message the way go.tpl does.
func messageDestination(m orgMessage) string {
	switch {
	case m.Team > 0:
		return "команде"
	case m.Team == 0:
		return "всем"
	default:
		return "от команды"
	}
}

// renderLevel builds one level object of the game state, following
// templates/JSON/go_level.tpl.
func (s *server) renderLevel(g *gameState, l *levelDef, p *levelProgress, now time.Time) string {
	hint1, hint2 := g.hints(l, p, now)
	number := ""
	if !l.Bonus {
		number = strconv.Itoa(p.Number)
	}
	takeBreak := ""
	if g.BreakPending != 0 && g.BreakPending == l.Order {
		takeBreak = "1"
	}
	lat, lon, radius := l.Lat, l.Lon, l.Radius

	var b strings.Builder
	b.WriteString("{\n")
	b.WriteString(`  "isLevelFinished": ` + jsonString(boolDigitEmpty(g.mainDone(l, p))) + ",\n")
	b.WriteString(`  "levelNumber": ` + jsonString(number) + ",\n")
	b.WriteString(`  "isBonusLevel": "` + boolDigit(l.Bonus || l.ShowAsBonus) + "\",\n")
	b.WriteString(`  "bonusLevelTime": "` + strconv.Itoa(l.BonusTime) + "\",\n")
	b.WriteString(`  "codesFounded": "` + strconv.Itoa(p.codesFound()) + "\",\n")
	b.WriteString(`  "totalCodes": "` + strconv.Itoa(len(l.Codes)) + "\",\n")
	b.WriteString(`  "neededCodes": "` + strconv.Itoa(l.CodeCount) + "\",\n")
	b.WriteString(`  "bonusCodesFounded": "` + strconv.Itoa(len(p.BonusCodes)) + "\",\n")
	b.WriteString(`  "bonusCodesTotal": "` + strconv.Itoa(len(l.BonusCodes)) + "\",\n")
	b.WriteString(`  "skvoz": ` + strconv.FormatBool(l.Skvoz) + ",\n")
	times := make([]string, 0, len(l.BonusTimes))
	for _, t := range l.BonusTimes {
		times = append(times, `"`+strconv.Itoa(t)+`"`)
	}
	b.WriteString(`  "bonusCodesTimes": [ ` + strings.Join(times, ", ") + " ],\n")
	b.WriteString(`  "bonusAfter": "` + strconv.Itoa(g.BonusAfter) + "\",\n")
	b.WriteString(`  "noMasterCode": "` + boolDigit(l.NoMasterCode) + "\",\n")
	b.WriteString(`  "isSabotage": "0",` + "\n")
	b.WriteString(`  "tryLimit": "` + strconv.Itoa(l.TryLimit) + "\",\n")
	b.WriteString(`  "tryLimitUsed": "",` + "\n")
	b.WriteString(`  "koline": ` + jsonString(g.koline(l, p)) + ",\n")
	b.WriteString(`  "question": ` + jsonString(l.Question) + ",\n")
	b.WriteString(`  "locationComment": ` + jsonString(l.LocationComment) + ",\n")
	b.WriteString("  \"spoilers\": [\n")
	for i, sp := range l.Spoilers {
		text := ""
		if p.Spoilers[sp.Order] {
			text = sp.Text
		}
		b.WriteString("      {\n")
		b.WriteString(`        "spoilerText": ` + jsonString(text) + ",\n")
		b.WriteString(`        "spoilerSolved": "` + boolDigit(p.Spoilers[sp.Order]) + "\",\n")
		b.WriteString(`        "spoilerPenalty": "` + strconv.Itoa(sp.Penalty) + "\",\n")
		b.WriteString(`        "spoilerNumber": "` + strconv.Itoa(sp.Order) + "\"\n")
		b.WriteString("    }")
		if i != len(l.Spoilers)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("  ],\n")
	b.WriteString(`  "koinspoiler": "0",` + "\n")
	b.WriteString(`  "hint1": ` + jsonString(hint1) + ",\n")
	b.WriteString(`  "hint2": ` + jsonString(hint2) + ",\n")
	b.WriteString(`  "locationLat": ` + jsonString(lat) + ",\n")
	b.WriteString(`  "locationLon": ` + jsonString(lon) + ",\n")
	b.WriteString(`  "locationRadius": ` + jsonString(radius) + ",\n")
	b.WriteString(`  "timeOnLevel": ` + jsonString(formatClock(g.levelDuration(p, now))) + ",\n")
	b.WriteString(`  "tm": "` + strconv.Itoa(g.timerSeconds(l, p, now)) + "\",\n")
	b.WriteString(`  "takeBreak": ` + jsonString(takeBreak) + "\n")
	b.WriteString("}")
	return b.String()
}

// boolDigit renders a flag the way the engine's database columns do.
// boolDigitEmpty renders the engine's other boolean spelling: "1" or the
// empty string, which is what {$levelFinished} produces.
func boolDigitEmpty(v bool) string {
	if v {
		return "1"
	}
	return ""
}

func boolDigit(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// handleGameAPI implements API/game.php: the statistics, log and level views
// on GET, and the proxy that runs a go/ action on POST.
func (s *server) handleGameAPI(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.beginPlayer(w, r)
	if !ok {
		return
	}
	now := time.Now()

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		action := r.PostForm.Get("action")
		s.mu.Lock()
		s.state.tick(now)
		res := s.doAction(sess, action, r.PostForm, now)
		s.mu.Unlock()
		if res.netDown {
			sleepCtx(r, networkDownDuration)
			http.Error(w, "mock: network is down", http.StatusBadGateway)
			return
		}
		writeJSON(w, map[string]any{"err": res.err})
		return
	}

	q := r.URL.Query()
	if s.has(quirkStopped) {
		// API/game.php:96 prints this and exits while the organizer has the
		// engine switched off. It carries no "error" key, so an envelope
		// check that only looks for one reads it as an empty success.
		writeJSON(w, map[string]any{"err": dzzzr.ErrEngineStopped})
		return
	}
	if s.has(quirkEmptyLevel) && q.Get("level") != "" {
		// 200 with nothing at all, which API/game.php does when the team is
		// blocked and the handler simply exits.
		w.WriteHeader(http.StatusOK)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.state
	g.tick(now)

	result := map[string]any{}
	if q.Get("legend") != "" {
		result["legend"] = g.Legend
	}
	if q.Get("stat") != "" {
		s.fillStat(result, g, now)
	}
	if q.Get("bonuslevel") != "" {
		var skvoz []map[string]any
		if l, p := g.skvoz(); l != nil {
			skvoz = append(skvoz, s.levelInfo(g, sess, l, p, now, true))
		}
		result["skvoz"] = skvoz
	} else if q.Get("level") != "" {
		l, p := g.current()
		if l == nil {
			result["level"] = g.noLevelText()
		} else {
			for k, v := range s.levelInfo(g, sess, l, p, now, false) {
				result[k] = v
			}
		}
	}
	if q.Get("log") != "" {
		result["log"] = s.logRows(g)
		result["skvoz"] = []any{}
	}
	writeJSON(w, result)
}

// noLevelText is what the engine shows when the team has no level in hand.
func (g *gameState) noLevelText() string {
	if g.Finished {
		return "<p style='text-align:center'>Вы прошли все основные уровни.<br>" + g.Greeting + "<p>"
	}
	return dzzzr.ErrText(dzzzr.ErrNoLevelPlanned)
}

// fillStat adds the per-level statistics table to an API/game.php reply.
func (s *server) fillStat(result map[string]any, g *gameState, now time.Time) {
	names := make([]string, 0, len(g.Issued)+1)
	cells := make([]string, 0, len(g.Issued)+1)
	var total time.Duration
	for _, order := range g.Issued {
		p := g.Progress[order]
		names = append(names, strconv.Itoa(p.Number))
		if p.CompletedAt.IsZero() {
			cells = append(cells, "<td >&nbsp;</td>")
			continue
		}
		d := g.levelDuration(p, now)
		total += d
		cells = append(cells, "<td >"+formatClock(d)+"</td>")
	}
	names = append(names, "Итого")
	cells = append(cells, "<td ><b>"+formatClock(total)+"</b></td>")
	result["lnames"] = names
	result["tds"] = cells
	result["teamstatTime"] = formatTime(now)
}

// logRows renders the team's game log the way API/game.php does, table cells
// and all.
func (s *server) logRows(g *gameState) []map[string]string {
	rows := make([]map[string]string, 0, len(g.Log))
	for _, e := range g.Log {
		rows = append(rows, map[string]string{
			"time":  "<td nowrap>" + e.Time.Format("15:04:05") + "</td>",
			"event": "<td>" + e.Event + "</td>",
			"data":  e.Data + "&nbsp;",
			"user":  e.User + "&nbsp;",
		})
	}
	return rows
}

// levelInfo builds the bot-oriented level view of API/game.php. PHP renders
// its sparse arrays as objects keyed by index, so dangers, sectors, dangersB
// and bonus are maps here too.
func (s *server) levelInfo(g *gameState, sess *session, l *levelDef, p *levelProgress, now time.Time, asSkvoz bool) map[string]any {
	dangers := map[string]any{}
	for i := range l.Codes {
		danger, sector := "", ""
		if i < len(l.Dangers) {
			danger = l.Dangers[i]
		}
		if i < len(l.Sectors) {
			sector = l.Sectors[i]
		}
		dangers[strconv.Itoa(i)] = map[string]any{
			"danger":  danger,
			"founded": p.Codes[normalizeCode(strings.Split(l.Codes[i], "#")[0])],
			"sector":  sector,
		}
	}
	dangersB := map[string]any{}
	for i := range l.BonusCodes {
		danger := ""
		if i < len(l.BonusDangers) {
			danger = l.BonusDangers[i]
		}
		dangersB[strconv.Itoa(i)] = map[string]any{
			"danger":  danger,
			"founded": p.BonusCodes[normalizeCode(strings.Split(l.BonusCodes[i], "#")[0])],
		}
	}
	sectors := map[string]string{}
	for i, name := range l.SectorNames {
		sectors[strconv.Itoa(i+1)] = name
	}
	bonus := map[string]string{}
	for i, t := range l.BonusTimes {
		bonus[strconv.Itoa(i+1)] = strconv.Itoa(t)
	}
	spoilerText := map[string]string{}
	for _, sp := range l.Spoilers {
		if p.Spoilers[sp.Order] {
			spoilerText[strconv.Itoa(sp.Order)] = sp.Text
		} else {
			spoilerText[strconv.Itoa(sp.Order)] = ""
		}
	}

	hint1, hint2 := g.hints(l, p, now)
	title := s.levelTitle(g, l, p)
	body := s.levelBody(g, l, p, now, hint1, hint2)

	if asSkvoz {
		var spoiler any = ""
		if len(l.Spoilers) > 0 {
			spoiler = true
		}
		return map[string]any{
			"id":          l.Order,
			"spoilerText": spoilerText,
			"title":       title,
			"level":       body,
			"spoiler":     spoiler,
		}
	}

	captain := g.isCaptain(sess)
	var canBreak any = ""
	if captain && g.Breaks < 2 && g.BreakPending == 0 && !l.NoBreak {
		canBreak = l.Order
	}
	var canAbandon any = ""
	if captain && g.Abandons < 2 && g.BreakPending == 0 && now.After(p.IssuedAt.Add(time.Duration(l.ClueMin)*time.Minute)) {
		canAbandon = l.Order
	}
	var nextLevel any = ""
	if captain && g.bonusPending(l, p) {
		nextLevel = l.Order
	}
	var takeBreak any = ""
	if g.BreakPending == l.Order {
		takeBreak = 1
	}
	var restart any = ""
	if g.onBreak(now) {
		restart = 1
	}

	elapsed := now.Sub(p.IssuedAt)
	var beforeClue1 any = ""
	var beforeClue2 any = ""
	switch {
	case captain && hint1 == "" && elapsed > 10*time.Minute:
		beforeClue1 = 1
	case captain && hint1 != "" && hint2 == "":
		beforeClue2 = g.ClueBeforePenalty
	}

	var spoiler any = ""
	if len(l.Spoilers) > 0 {
		spoiler = true
	}

	return map[string]any{
		"spoilerText":        spoilerText,
		"level":              title + body,
		"title":              title,
		"spoiler":            spoiler,
		"restartAfterBreake": restart,
		"beforeClue1":        beforeClue1,
		"beforeClue2":        beforeClue2,
		"nextLevel":          nextLevel,
		"canBreake":          canBreak,
		"canAbandon":         canAbandon,
		"takeBreak":          takeBreak,
		"timer":              g.timerSeconds(l, p, now),
		"selectLevel":        "",
		"dangers":            dangers,
		"sectors":            sectors,
		"dangersB":           dangersB,
		"bonus":              bonus,
		"has_fake_codes":     len(l.FakeCodes) > 0,
		"isSabotage":         false,
		"number":             strconv.Itoa(p.Number),
		"noMasterCode":       l.NoMasterCode,
		"noBreak":            l.NoBreak,
		"hold":               false,
		"clue1":              hint1,
		"clue2":              hint2,
		"codeCount":          l.CodeCount,
		"skvoz":              s.skvozList(g, now),
	}
}

// skvozList renders the bonus levels that run alongside the main line.
func (s *server) skvozList(g *gameState, now time.Time) []map[string]any {
	var out []map[string]any
	l, p := g.skvoz()
	if l == nil {
		return out
	}
	h1, h2 := g.hints(l, p, now)
	spoilerText := map[string]string{}
	for _, sp := range l.Spoilers {
		spoilerText[strconv.Itoa(sp.Order)] = ""
	}
	var spoiler any = ""
	if len(l.Spoilers) > 0 {
		spoiler = true
	}
	out = append(out, map[string]any{
		"id":          l.Order,
		"spoilerText": spoilerText,
		"title":       s.levelTitle(g, l, p),
		"level":       s.levelBody(g, l, p, now, h1, h2),
		"spoiler":     spoiler,
	})
	return out
}

// levelTitle renders the heading block API/game.php builds for a level.
func (s *server) levelTitle(g *gameState, l *levelDef, p *levelProgress) string {
	number := ""
	if !l.Bonus {
		number = strconv.Itoa(p.Number)
	}
	var b strings.Builder
	b.WriteString("<div class=title>Задание " + number + " ")
	if l.Bonus {
		b.WriteString("БОНУСНОЕ (-" + strconv.Itoa(l.BonusTime) + " мин.) ")
	}
	b.WriteString("<br>получено в " + p.IssuedAt.Format("15:04:05"))
	b.WriteString("<div style='text-align:center'>найдено кодов: " + strconv.Itoa(p.codesFound()) + " из " + strconv.Itoa(len(l.Codes)))
	if l.CodeCount > 0 {
		b.WriteString(", для прохождения этого задания достаточно найти " + strconv.Itoa(l.CodeCount))
	}
	if len(l.BonusCodes) > 0 {
		b.WriteString(" (основных)<br>" + strconv.Itoa(len(p.BonusCodes)) + " из " + strconv.Itoa(len(l.BonusCodes)) + " (бонусных)")
	}
	if l.NoMasterCode {
		b.WriteString("<br>В этом задании нельзя использовать универсальный код.")
	}
	b.WriteString("</div></div>")
	return b.String()
}

// levelBody renders the task text, difficulty coefficients and hints.
func (s *server) levelBody(g *gameState, l *levelDef, p *levelProgress, now time.Time, hint1, hint2 string) string {
	var b strings.Builder
	b.WriteString("<div class=zad>" + l.Question)
	b.WriteString("<strong>Коды сложности </strong><br>" + g.koline(l, p))
	b.WriteString("</div>\n")
	if hint1 != "" {
		b.WriteString("<div class=title>Подсказка 1:</div><div class=zad>" + hint1 + "</div>\n")
	}
	if hint2 != "" {
		b.WriteString("<div class=title>Подсказка 2:</div><div class=zad>" + hint2 + "</div>\n")
	}
	b.WriteString("<p>Время на уровне: " + formatClock(g.levelDuration(p, now)) + "</p>")
	return b.String()
}

// handleMessages implements API/messages.php: organizer messages and team
// chat merged into one list, oldest first.
func (s *server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.beginPlayer(w, r); !ok {
		return
	}
	after, _ := url.QueryUnescape(r.URL.Query().Get("after"))

	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.state

	type row struct {
		t       time.Time
		who     string
		content string
		ava     string
	}
	var rows []row
	for _, m := range g.visibleOrgMessages() {
		who := "организатор"
		if m.Team < 0 {
			who = m.User
			if who == "" {
				who = "штаб"
			}
		}
		rows = append(rows, row{t: m.Time, who: who, content: m.Text, ava: "0"})
	}
	for _, c := range g.Chat {
		rows = append(rows, row{t: c.Time, who: c.Who, content: c.Content, ava: c.Ava})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].t.Before(rows[j].t) })

	var out []map[string]string
	for _, rr := range rows {
		stamp := formatTime(rr.t)
		if after != "" && stamp <= after {
			continue
		}
		content := strings.ReplaceAll(rr.content, "'", "&rsquo;")
		content = strings.ReplaceAll(content, `"`, "&quot;")
		out = append(out, map[string]string{
			"ava":     rr.ava,
			"who":     rr.who,
			"time":    stamp,
			"content": content,
		})
	}
	if out == nil {
		out = []map[string]string{}
	}
	writeJSON(w, map[string]any{"messages": out})
}

// handlePostMessage implements API/postmessage.php.
//
// It answers Ok and stores nothing, which is what Dozor Classic does: a
// message sent this way reaches neither API/messages.php nor the game state.
// Storing it here would let a client that uses this endpoint instead of
// action=send_message pass its tests and lose the player's messages in a real
// game.
func (s *server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.beginPlayer(w, r); !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(r.PostForm.Get("content")) == "" {
		writeJSON(w, map[string]string{"message": "Oops"})
		return
	}
	writeJSON(w, map[string]string{"message": "Ok"})
}

// gameListEntry is one game of API/gamesList.php.
type gameListEntry struct {
	ID                 int      `json:"id"`
	Project            string   `json:"project"`
	Number             string   `json:"number"`
	Name               string   `json:"name"`
	League             int      `json:"league"`
	Date               string   `json:"date"`
	Authors            string   `json:"authors"`
	Territory          string   `json:"territory"`
	AdditionalTools    string   `json:"additionalTools"`
	Legend             string   `json:"legend"`
	Information        string   `json:"information"`
	Image              *string  `json:"image"`
	Season             int      `json:"season"`
	Start              string   `json:"start"`
	IsPrequelAvailable bool     `json:"isPrequelAvailable"`
	IsLeague           bool     `json:"isLeague"`
	IsLinear           bool     `json:"isLinear"`
	Teams              []string `json:"teams"`
}

// handleGamesList implements API/gamesList.php. It includes APIconst.php like
// the rest of the API, so it needs both credentials even though it only
// returns public announcements; measured against classic.dzzzr.ru on
// 2026-09-08, an anonymous request gets the session-error envelope.
func (s *server) handleGamesList(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.beginPlayer(w, r); !ok {
		return
	}
	q := r.URL.Query()
	after, _ := url.QueryUnescape(q.Get("after"))
	now := time.Now()

	s.mu.Lock()
	g := s.state
	poster := "http://classic.dzzzr.ru/" + s.city + "/uploaded/" + s.city + "/Night/afisha/4243.jpg"
	games := []gameListEntry{
		{
			ID: g.GameID, Number: g.Number, Name: g.GameName, League: 1,
			Date: formatTime(g.Start), Authors: g.Authors, Territory: "ЦАО",
			AdditionalTools: "фонарь", Legend: g.Legend, Information: "Тестовая игра mock-сервера",
			Season: g.Season, Start: "Парковка у ТЦ", IsLinear: true,
			Teams: []string{g.TeamName},
		},
		{
			ID: g.GameID + 1, Number: "2", Name: "Будущая игра", League: 1,
			Date: formatTime(g.Start.Add(7 * 24 * time.Hour)), Authors: "Штаб",
			Image: &poster, Season: g.Season, IsLeague: true, IsLinear: true,
		},
	}
	s.mu.Unlock()

	out := make([]gameListEntry, 0, len(games))
	for _, game := range games {
		// The mock keeps no finished games, so archive never matches.
		if q.Get("archive") != "" {
			continue
		}
		if q.Get("new") != "" && game.Date <= formatTime(now) {
			continue
		}
		if after != "" && game.Date <= after {
			continue
		}
		out = append(out, game)
	}
	if len(out) == 0 {
		writeRaw(w, `{"games" : null}`)
		return
	}
	writeJSON(w, map[string]any{"games": out})
}
