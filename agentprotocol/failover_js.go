//go:build js && wasm

// Failover definitions adapted from PicoClaw, MIT, copyright 2026 PicoClaw contributors.
package agentprotocol

import "fmt"

// FailoverReason classifies why an LLM request failed for fallback decisions.
type FailoverReason string

const (
	FailoverAuth            FailoverReason = "auth"
	FailoverRateLimit       FailoverReason = "rate_limit"
	FailoverBilling         FailoverReason = "billing"
	FailoverNetwork         FailoverReason = "network"
	FailoverTimeout         FailoverReason = "timeout"
	FailoverFormat          FailoverReason = "format"
	FailoverContextOverflow FailoverReason = "context_overflow"
	FailoverOverloaded      FailoverReason = "overloaded"
	FailoverUnknown         FailoverReason = "unknown"
)

// FailoverError wraps an LLM provider error with classification metadata.
type FailoverError struct {
	Reason   FailoverReason
	Provider string
	Model    string
	Status   int
	Wrapped  error
}

func (e *FailoverError) Error() string {
	return fmt.Sprintf("failover(%s): provider=%s model=%s status=%d: %v",
		e.Reason, e.Provider, e.Model, e.Status, e.Wrapped)
}

func (e *FailoverError) Unwrap() error {
	return e.Wrapped
}

// IsRetriable returns true if this error should trigger fallback to next candidate.
// Non-retriable: Format errors (bad request structure, image dimension/size).
func (e *FailoverError) IsRetriable() bool {
	return e.Reason != FailoverFormat && e.Reason != FailoverContextOverflow
}
