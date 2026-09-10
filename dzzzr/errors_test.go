package dzzzr

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestErrTextKnownCodes(t *testing.T) {
	if got := ErrText(9); got != "Код принят. Выполняйте следующее задание." {
		t.Errorf("ErrText(9) = %q", got)
	}
	if got := ErrText(57); !strings.HasPrefix(got, "У вас недостаточно прав") {
		t.Errorf("ErrText(57) = %q", got)
	}
	if got := ErrText(999); got != "Неизвестный код 999" {
		t.Errorf("ErrText(999) = %q", got)
	}
	if got := ErrText(0); got != "" {
		t.Errorf("ErrText(0) = %q, want empty", got)
	}
	for code, text := range errTexts {
		if strings.Contains(text, "<") {
			t.Errorf("code %d text contains HTML: %q", code, text)
		}
	}
}

func TestAcceptedCodes(t *testing.T) {
	for _, c := range []int{8, 9, 16, 32, 34, 36, 37, 47, 48, 49, 50, 51, 52, 55} {
		if !IsAcceptedCode(c) {
			t.Errorf("code %d should be accepted", c)
		}
	}
	for _, c := range []int{7, 11, 12, 13, 14, 44, 45, 56, 57, 0} {
		if IsAcceptedCode(c) {
			t.Errorf("code %d should not be accepted", c)
		}
	}
	// A successful break or abandon reports nothing at all, so these codes
	// never arrive from a redirect and must not be listed as outcomes.
	for _, c := range []int{ErrAuthOK, ErrLoginOK, ErrLevelAbandoned} {
		if IsAcceptedCode(c) {
			t.Errorf("code %d is never sent as an action result", c)
		}
	}
}

func TestLoginCodeText(t *testing.T) {
	if LoginCodeText(2) != "Успешная авторизация" {
		t.Error("LoginCodeText(2)")
	}
	if !strings.Contains(LoginCodeText(77), "77") {
		t.Error("unknown login code should name the code")
	}
}

func TestErrorPredicates(t *testing.T) {
	wrapped := fmt.Errorf("outer: %w", &AuthError{Kind: AuthSession})
	if !IsAuthError(wrapped) || AuthErrorKindOf(wrapped) != AuthSession {
		t.Error("AuthError not detected through wrapping")
	}
	if IsAuthError(errors.New("plain")) {
		t.Error("plain error must not be an AuthError")
	}
	und := &UndecodableResponseError{StatusCode: 200, Context: "x", Err: errors.New("bad json")}
	if !IsUndecodableAccepted(und) {
		t.Error("2xx undecodable must be accepted")
	}
	und.StatusCode = 502
	if IsUndecodableAccepted(und) {
		t.Error("5xx undecodable must not be accepted")
	}
	if !IsUndecodable(und) {
		t.Error("IsUndecodable")
	}
	if !errors.Is(und, und.Err) {
		t.Error("Unwrap")
	}
	if !IsEngineError(fmt.Errorf("w: %w", &EngineError{Code: 57})) {
		t.Error("EngineError")
	}
	if !strings.Contains((&EngineError{Code: 57}).Error(), "57") {
		t.Error("EngineError text")
	}
}
