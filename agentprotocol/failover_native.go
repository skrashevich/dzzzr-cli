//go:build !js

package agentprotocol

import "github.com/sipeed/picoclaw/pkg/providers"

type FailoverReason = providers.FailoverReason
type FailoverError = providers.FailoverError

const (
	FailoverAuth            = providers.FailoverAuth
	FailoverRateLimit       = providers.FailoverRateLimit
	FailoverBilling         = providers.FailoverBilling
	FailoverNetwork         = providers.FailoverNetwork
	FailoverTimeout         = providers.FailoverTimeout
	FailoverFormat          = providers.FailoverFormat
	FailoverContextOverflow = providers.FailoverContextOverflow
	FailoverOverloaded      = providers.FailoverOverloaded
	FailoverUnknown         = providers.FailoverUnknown
)
