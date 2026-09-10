package agenttools

import (
	"context"
	"errors"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func TestSessionRecoveryDoesNotReplayUnsafeCalls(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mutating bool
		err      error
	}{
		{"mutation", true, &dzzzr.AuthError{Kind: dzzzr.AuthSession}},
		{"basic credentials", false, &dzzzr.AuthError{Kind: dzzzr.AuthBasic}},
		{"transport failure", false, errors.New("connection lost")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			tool := &Tool{
				name: "test", mutating: tc.mutating,
				gate: &gate{policy: PolicyFull}, cache: newReadCache(0),
				run:            func(context.Context, arguments) (any, error) { calls++; return nil, tc.err },
				reauthenticate: func(context.Context) error { t.Fatal("unexpected reauthentication"); return nil },
			}
			if r := tool.Execute(t.Context(), nil); !r.IsError {
				t.Fatal("failure hidden")
			}
			if calls != 1 {
				t.Fatalf("call replayed %d times", calls)
			}
		})
	}
}
