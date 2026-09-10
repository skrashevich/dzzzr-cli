package dzzrmobile

import "github.com/skrashevich/dzzzr-cli/dzzzr"

// ErrText returns the engine's own wording for a result code from
// ActionResult.err or GameState.errNo.
func ErrText(code int64) string { return dzzzr.ErrText(int(code)) }

// IsAcceptedCode reports whether a result code means the engine accepted the
// code or performed the action.
func IsAcceptedCode(code int64) bool { return dzzzr.IsAcceptedCode(int(code)) }

// LoginCodeText describes a sign-in code from LoginResponse.code.
func LoginCodeText(code int64) string { return dzzzr.LoginCodeText(int(code)) }

// IsAuthError reports whether an error means a credential was refused.
func IsAuthError(err error) bool { return dzzzr.IsAuthError(err) }

// AuthErrorKind names which credential was refused: "captain/pin",
// "session", "admin" or "" when the error is something else.
func AuthErrorKind(err error) string {
	k := dzzzr.AuthErrorKindOf(err)
	if k == 0 {
		return ""
	}
	return k.String()
}

// IsUndecodableAccepted reports whether an error means the engine received
// the request and only its reply was unreadable. A code submitted in that
// state was already recorded, so resending it would count as a repeat.
func IsUndecodableAccepted(err error) bool { return dzzzr.IsUndecodableAccepted(err) }

// StripHTML turns an engine HTML fragment (a task, a hint, a message) into
// plain text, keeping line breaks.
func StripHTML(s string) string { return dzzzr.StripHTML(s) }
