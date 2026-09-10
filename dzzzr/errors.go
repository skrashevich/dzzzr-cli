package dzzzr

import (
	"errors"
	"fmt"
	"net/http"
)

// Engine result codes. The engine reports the outcome of every action as a
// number in the "err" query parameter of its redirect; despite the name most
// of them are successes. The table comes from go/errors.php of Dozor Classic.
const (
	ErrGameNotStarted        = 1
	ErrWrongPIN              = 2
	ErrAuthOK                = 3
	ErrEmptyCode             = 4
	ErrLevelTimeUp           = 5
	ErrCodeRepeated          = 7
	ErrCodeAcceptedPartial   = 8
	ErrCodeAcceptedNext      = 9
	ErrGameOver              = 10
	ErrCodeRejected          = 11
	ErrTooManyAttempts       = 12
	ErrEngineStopped         = 13
	ErrTeamPaused            = 14
	ErrNoLevelPlanned        = 15
	ErrCodeAccepted          = 16
	ErrTimeUp                = 17
	ErrAccountBlocked        = 21
	ErrLoginOK               = 22
	ErrWrongPassword         = 23
	ErrUnknownUser           = 24
	ErrLoginFailed           = 25
	ErrBreaksExhausted       = 26
	ErrOnBreak               = 27
	ErrAbandonsExhausted     = 28
	ErrNextLevelAlreadyGiven = 29
	ErrNoNextLevel           = 30
	ErrSkvozCodeRejected     = 31
	ErrSkvozCodeAccepted     = 32
	ErrMessageSent           = 33
	ErrSkvozPartial          = 34
	ErrBonusRepeated         = 35
	ErrBonusAccepted         = 36
	ErrSkvozBonusAccepted    = 37
	ErrTryLimitExceeded      = 38
	ErrLevelAbandoned        = 39
	ErrFakeCode              = 40
	ErrAllMainCodesFound     = 41
	ErrEarlyHintPenalty      = 42
	ErrOptionalCode          = 43
	ErrMasterCodeForbidden   = 44
	ErrMasterCodeUsed        = 45
	ErrMasterCodeInSkvoz     = 46
	ErrMasterCodePartial     = 47
	ErrMasterCodeNext        = 48
	ErrMasterCodeAccepted    = 49
	ErrLevelMasterPartial    = 50
	ErrLevelMasterNext       = 51
	ErrLevelMasterAccepted   = 52
	ErrCodeTakenByOthers     = 53
	ErrSkvozTimeUp           = 54
	ErrSpoilerAccepted       = 55
	ErrSpoilerRejected       = 56
	ErrNoPermission          = 57
	ErrGameNotStartedYet     = 58
)

var errTexts = map[int]string{
	1:  "Игра не началась.",
	2:  "Неверный PIN",
	3:  "Авторизация пройдена успешно",
	4:  "Не введен код",
	5:  "Время на отправку кода вышло. Решайте следующее задание.",
	7:  "Код не принят. Вы уже ввели этот код.",
	8:  "Код принят. Ищите следующий составной код.",
	9:  "Код принят. Выполняйте следующее задание.",
	10: "Игра закончена. Вы прошли все уровни.",
	11: "Код не принят. Если вы уверены в правильности вводимого кода, проверьте, не выдано ли вам следующее задание. Может быть кто-то из вашей команды уже ввел этот код и вы пытаетесь отправить его повторно к пройденному уже уровню.",
	12: "Вы вводили неверный код слишком много раз. Прием данных от Вас заблокирован на три минуты. Повторите попытку позже.",
	13: "Движок остановлен организатором.",
	14: "Игра вашей команды приостановлена.",
	15: "Вам не запланировано следующее задание. Организатор видит, что вы бездействуете и назначит вам уровень в ближайшее время. Периодически обновляйте страницу. Время за задержку будет вычтено из вашего результата. Если новое задание не будет выдаваться длительное время, свяжитесь с организатором.",
	16: "Код принят.",
	17: "Время на отправку кода вышло.",
	21: "Акаунт заблокирован или не активирован. Выполните инструкции, высланные вам в письме-подтверждении.",
	22: "Авторизация пройдена успешно",
	23: "Неверный пароль",
	24: "Неизвестный пользователь",
	25: "Ошибка авторизации",
	26: "Вы уже взяли 2 перерыва. Больше вы не можете приостанавливать игру своей команды.",
	27: "Игра вашей команды приостановлена на 15 минут по решению штаба.",
	28: "Вы уже дважды отказывались от заданий. Больше вы не можете завершать задания досрочно.",
	29: "Вам уже выдано следующее задание.",
	30: "Вам не запланировано следующее задание. Чтобы отказаться от текущего, свяжитесь с организатором и попросите его назначить вам следующий уровень.",
	31: "Код к сквозному бонусному заданию не принят.",
	32: "Код к сквозному бонусному заданию принят.",
	33: "Ваше сообщение отправлено организатору",
	34: "Код принят. Ищите следующий составной код к сквозному бонусному заданию.",
	35: "Код не принят. Вы пытаетесь повторно отправить уже принятый бонусный код.",
	36: "Принят бонусный код.",
	37: "Принят бонусный код к сквозному заданию.",
	38: "Код не принят. Вы превысили лимит попыток неправильного ввода кода. Предыдущее задание считается невзятым. Вам выдано следующее задание",
	39: "Вы решили отказаться от выполнения задания. Вам выдано новое задание.",
	40: "Это ложный код. За его нахождение ваша команда получила штраф.",
	41: "Вы нашли все основные коды. Вы можете продолжать искать бонусные коды или перейти на следующий уровень.",
	42: "За досрочное использование подсказки вам начислен штраф",
	43: "Вы ввели не обязательный основной код, задание уже считается выполненным. Ищите бонусные коды.",
	44: "В этом задании нельзя использовать универсальный код.",
	45: "Вы уже использовали универсальный код, повторное его использование невозможно.",
	46: "В бонусном сквозном задании нельзя использовать универсальный код.",
	47: "Универсальный код принят. Ищите другие коды к этому заданию.",
	48: "Универсальный код принят. Выполняйте следующее задание.",
	49: "Универсальный код принят.",
	50: "Мастер-код принят. Ищите другие коды к этому заданию.",
	51: "Мастер-код принят. Выполняйте следующее задание.",
	52: "Мастер-код принят.",
	53: "Данный код уже был найден вашей или другой командой. Ищите другой код.",
	54: "Код к сквозному заданию не принят, так как истекло время его выполнения",
	55: "Код к спойлеру принят",
	56: "Код к спойлеру не принят",
	57: "У вас недостаточно прав для использования данной функции",
	58: "Игра еще не началась",
}

// acceptedCodes lists the result codes that mean the engine accepted the
// submitted code or action.
//
// Only codes the engine actually sends in a redirect are here. A successful
// break, abandon, hint request or level change reports nothing at all —
// go2.php redirects with an empty err — so those are not codes but the
// absence of one; the caller decides what an empty result means for the
// action it asked for.
var acceptedCodes = map[int]bool{
	ErrCodeAcceptedPartial: true, ErrCodeAcceptedNext: true, ErrCodeAccepted: true,
	ErrSkvozCodeAccepted: true, ErrMessageSent: true, ErrSkvozPartial: true,
	ErrBonusAccepted: true, ErrSkvozBonusAccepted: true, ErrAllMainCodesFound: true,
	ErrEarlyHintPenalty: true, ErrOptionalCode: true, ErrMasterCodePartial: true, ErrMasterCodeNext: true,
	ErrMasterCodeAccepted: true, ErrLevelMasterPartial: true, ErrLevelMasterNext: true,
	ErrLevelMasterAccepted: true, ErrSpoilerAccepted: true,
}

// IsAcceptedCode reports whether an engine result code means success.
func IsAcceptedCode(code int) bool { return acceptedCodes[code] }

// ErrText returns the engine's own wording for a result code, with HTML
// markup removed. Unknown codes produce a generic sentence naming the code.
func ErrText(code int) string {
	if code == 0 {
		return ""
	}
	if s, ok := errTexts[code]; ok {
		return s
	}
	return fmt.Sprintf("Неизвестный код %d", code)
}

// Site sign-in codes returned by API/login.php.
const (
	LoginBlocked        = 1
	LoginOK             = 2
	LoginWrongPassword  = 4
	LoginUnknownUser    = 5
	LoginBadParameters  = 6
	LoginBadSessionCode = 0 // no "code" at all: the reply was an auth-error envelope
)

var loginTexts = map[int]string{
	1: "Доступ заблокирован",
	2: "Успешная авторизация",
	4: "Неверный пользователь или пароль",
	5: "Неверный пользователь или пароль",
	6: "Неверные входные параметры",
}

// LoginCodeText returns a human-readable description for a login code.
func LoginCodeText(code int) string {
	if s, ok := loginTexts[code]; ok {
		return s
	}
	return fmt.Sprintf("Неизвестный код авторизации %d", code)
}

// EngineError is returned when an action's result code is a refusal that the
// caller cannot act on as a result (for example a permission or PIN error).
// Most result codes are not errors and arrive in ActionResult instead.
type EngineError struct {
	Code int
	Text string
}

// Error implements error.
func (e *EngineError) Error() string {
	if e.Text == "" {
		e.Text = ErrText(e.Code)
	}
	return fmt.Sprintf("dzzzr: engine error %d: %s", e.Code, e.Text)
}

// IsEngineError reports whether err carries an engine result code.
func IsEngineError(err error) bool {
	var e *EngineError
	return errors.As(err, &e)
}

// AuthErrorKind classifies an authentication failure.
type AuthErrorKind int

// Authentication failure kinds.
const (
	// AuthBasic means the HTTP Basic captain/PIN pair was refused (401).
	AuthBasic AuthErrorKind = iota + 1
	// AuthSession means the site session token is missing or expired.
	AuthSession
	// AuthAdmin means the organizer credentials were refused by the admin area.
	AuthAdmin
	// AuthAdminScope means the organizer credentials were accepted (HTTP 200)
	// but the engine's own "Вы не имеете доступа" page says this account has
	// no rights to the requested section, e.g. because it is not the game's
	// assigned organizer. Distinct from AuthAdmin so the message does not
	// send the caller to re-check a login/password that is actually correct.
	AuthAdminScope
)

func (k AuthErrorKind) String() string {
	switch k {
	case AuthBasic:
		return "captain/pin"
	case AuthSession:
		return "session"
	case AuthAdmin:
		return "admin"
	case AuthAdminScope:
		return "admin-scope"
	}
	return "unknown"
}

// AuthError reports a rejected credential. Kind says which one so the caller
// can ask for the right thing again.
type AuthError struct {
	Kind    AuthErrorKind
	Message string
}

// Error implements error.
func (e *AuthError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("dzzzr: %s authentication failed", e.Kind)
	}
	return fmt.Sprintf("dzzzr: %s authentication failed: %s", e.Kind, e.Message)
}

// IsAuthError reports whether err is an authentication failure.
func IsAuthError(err error) bool {
	var e *AuthError
	return errors.As(err, &e)
}

// AuthErrorKindOf returns the kind of an authentication error, or 0.
func AuthErrorKindOf(err error) AuthErrorKind {
	var e *AuthError
	if errors.As(err, &e) {
		return e.Kind
	}
	return 0
}

// UndecodableResponseError is returned when the engine replied but the body
// could not be interpreted. StatusCode tells whether the request reached the
// engine: a 2xx means it did and only the reply was unreadable, so a submitted
// code was recorded and must not be resent; a non-2xx means an intermediary
// answered and the request stays retryable.
type UndecodableResponseError struct {
	StatusCode int
	Context    string
	Err        error
	Body       string
}

// Error implements error.
func (e *UndecodableResponseError) Error() string {
	return fmt.Sprintf("dzzzr: %s: undecodable response (HTTP %d): %v", e.Context, e.StatusCode, e.Err)
}

// Unwrap implements errors.Unwrap.
func (e *UndecodableResponseError) Unwrap() error { return e.Err }

// IsUndecodableAccepted reports whether err is an unreadable reply to a
// request the engine did receive (2xx status).
func IsUndecodableAccepted(err error) bool {
	var e *UndecodableResponseError
	if !errors.As(err, &e) {
		return false
	}
	return e.StatusCode >= 200 && e.StatusCode < 300
}

// IsUndecodable reports whether err is any unreadable-reply error.
func IsUndecodable(err error) bool {
	var e *UndecodableResponseError
	return errors.As(err, &e)
}

// HTTPError is returned for unexpected HTTP status codes.
type HTTPError struct {
	StatusCode int
	Context    string
}

// Error implements error.
func (e *HTTPError) Error() string {
	return fmt.Sprintf("dzzzr: %s: HTTP %d %s", e.Context, e.StatusCode, http.StatusText(e.StatusCode))
}

// The game page answers with one of these states instead of a game. They are
// the "error" values of templates/JSON/go_nogame.tpl and its siblings, and
// share no numbering with go/errors.php.
const (
	GameStateNotAuthorized = 1 // go_lite_noauth.tpl
	GameStateNoGame        = 2 // go_nogame.tpl: not entered in any game
	GameStateReserve       = 3 // go_reserv.tpl: the player is in the team's reserve
	GameStateWrongTeam     = 4 // go_wrongteam.tpl: another team's interface
)

// GameAccessError says the engine will not show a game: the team is not
// entered in one, the player sits in the reserve, or they opened another
// team's interface. The engine reports all of these with HTTP 200, so without
// this the reply decodes as an empty, successful game.
type GameAccessError struct {
	State int
	Text  string
}

// Error implements error.
func (e *GameAccessError) Error() string {
	return fmt.Sprintf("dzzzr: игра недоступна (состояние %d): %s", e.State, e.Text)
}

// IsGameAccessError reports whether err says the engine has no game to show.
func IsGameAccessError(err error) bool {
	var e *GameAccessError
	return errors.As(err, &e)
}
