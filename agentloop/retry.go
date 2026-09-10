package agentloop

import (
	"errors"
	"strings"

	"github.com/sipeed/picoclaw/pkg/providers"
)

var errEmptyModelResponse = errors.New("провайдер вернул пустой ответ без текста и вызовов инструментов; задача не завершена")

var errProviderResponse = errors.New("провайдер прервал ответ (finish_reason=error); вызовы инструментов не выполнены")

// retryable reports whether repeating the request could succeed. A wrong key
// or an unknown model will fail the same way every time, so retrying only
// wastes the user's game time; a rate limit or a dropped connection will not.
func retryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errEmptyModelResponse) || errors.Is(err, errProviderResponse) {
		return true
	}
	var failover *providers.FailoverError
	if errors.As(err, &failover) {
		switch failover.Reason {
		case providers.FailoverRateLimit, providers.FailoverNetwork,
			providers.FailoverTimeout, providers.FailoverOverloaded:
			return true
		case providers.FailoverAuth, providers.FailoverBilling,
			providers.FailoverFormat, providers.FailoverContextOverflow:
			return false
		case providers.FailoverUnknown:
		}
	}

	s := strings.ToLower(err.Error())
	for _, permanent := range []string{
		"unknown provider", "invalid model", "model not found", "unauthorized", "forbidden",
	} {
		if strings.Contains(s, permanent) {
			return false
		}
	}
	for _, transient := range []string{
		"context deadline exceeded", "http 429", "http 502", "http 503", "http 504",
		"connection reset", "eof", "rate limit", "rate_limit", "overloaded",
		"temporarily unavailable", "failover(network)", "failover(timeout)",
	} {
		if strings.Contains(s, transient) {
			return true
		}
	}
	return false
}
