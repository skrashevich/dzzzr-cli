//go:build js && wasm

package agentloop

import providers "github.com/skrashevich/dzzzr-cli/agentprotocol"

func disableProviderLogging() {}
func newProvider(cfg Config) providers.LLMProvider {
	if cfg.Provider != nil {
		return cfg.Provider
	}
	return &providers.HTTPProvider{Endpoint: cfg.BaseURL, Key: cfg.APIKey, Model: cfg.Model, ExtraBody: cfg.ExtraBody}
}
