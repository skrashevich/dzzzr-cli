//go:build !js

package agentloop

import (
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
)

func disableProviderLogging() { logger.DisableConsole() }

// newProvider builds the provider the run talks to.
func newProvider(cfg Config) providers.LLMProvider {
	if cfg.Provider != nil {
		return cfg.Provider
	}
	userAgent := cfg.UserAgent
	if userAgent == "" {
		userAgent = "dzzzr-cli"
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}
	p := providers.NewHTTPProviderWithMaxTokensFieldAndRequestTimeout(
		cfg.APIKey,
		strings.TrimRight(cfg.BaseURL, "/"),
		"", "",
		userAgent,
		int((timeout+time.Second-1)/time.Second),
		cfg.ExtraBody, nil,
	)
	// OpenRouter needs to be recognized by name for its own request fields.
	if strings.Contains(strings.ToLower(cfg.BaseURL), "openrouter.ai") {
		p.SetProviderName("openrouter")
	}
	return p
}
