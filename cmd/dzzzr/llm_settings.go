package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// llmSettingsFileEnvVar relocates the settings file; tests use it to stay out
// of the developer's real configuration.
const llmSettingsFileEnvVar = "DZZZR_LLM_SETTINGS_FILE"

// Transport choices. An empty method leaves the choice to what else is
// configured; openai and openrouter are accepted as the API-key path because
// DZZZR_LLM_PROVIDER has always accepted them.
const (
	authMethodAPIKey = "apikey"
	authMethodCodex  = "codex"
)

// llmSettings is the transport configured in the web settings panel. It is the
// persistent counterpart of the DZZZR_LLM_* environment variables, and it is
// read by exactly one function: resolveLLMConfig.
//
// A field left empty means "not configured here", so clearing a field in the
// panel hands the choice back to the environment rather than forcing an empty
// value.
type llmSettings struct {
	AuthMethod string `json:"auth_method,omitempty"`
	BaseURL    string `json:"base_url,omitempty"`
	APIKey     string `json:"api_key,omitempty"`
	Model      string `json:"model,omitempty"`
}

// llmSettingsWhat is the user-facing noun in this file's errors.
const llmSettingsWhat = "настройки LLM"

// llmSettingsFile resolves the settings path. The "llm" subdirectory keeps the
// file away from the per-city session files a logout deletes.
func llmSettingsFile() (string, error) {
	return stateFilePath(llmSettingsFileEnvVar, "llm", "settings.json")
}

// loadLLMSettings reads the stored transport, normalised on the way out.
//
// An unparseable file is an error, and it is deliberately propagated all the
// way to resolveLLMConfig: ignoring it would silently run the agent against a
// provider the user did not choose.
func loadLLMSettings() (llmSettings, error) {
	path, err := llmSettingsFile()
	if err != nil {
		return llmSettings{}, err
	}
	s, err := loadJSONState[llmSettings](path, llmSettingsWhat)
	if err != nil {
		return llmSettings{}, err
	}
	return s.trimmed(), nil
}

func (s llmSettings) trimmed() llmSettings {
	return llmSettings{
		AuthMethod: strings.ToLower(strings.TrimSpace(s.AuthMethod)),
		BaseURL:    strings.TrimSpace(s.BaseURL),
		APIKey:     strings.TrimSpace(s.APIKey),
		Model:      strings.TrimSpace(s.Model),
	}
}

// saveLLMSettings writes the settings for the next turn and the next process.
func saveLLMSettings(s llmSettings) error {
	path, err := llmSettingsFile()
	if err != nil {
		return err
	}
	return saveJSONState(path, llmSettingsWhat, s.trimmed())
}

func deleteLLMSettings() error {
	path, err := llmSettingsFile()
	if err != nil {
		return err
	}
	return deleteJSONState(path)
}

// normalizeAuthMethod maps every accepted spelling onto the two transports.
func normalizeAuthMethod(raw string) (string, error) {
	switch method := strings.ToLower(strings.TrimSpace(raw)); method {
	case "":
		return "", nil
	case authMethodAPIKey, "openai", "openrouter":
		return authMethodAPIKey, nil
	case authMethodCodex, "chatgpt":
		return authMethodCodex, nil
	default:
		return "", fmt.Errorf("неизвестный способ подключения %q (DZZZR_LLM_PROVIDER или настройки LLM): используйте openai, openrouter или codex", method)
	}
}

// --- resolution ---

// The environment variables consulted for each field, in the order the first
// non-empty one wins. They are listed here rather than inline so the settings
// API can name the variable that is shadowing a stored value.
var (
	llmAuthEnvVars    = []string{"DZZZR_LLM_PROVIDER"}
	llmAPIKeyEnvVars  = []string{"DZZZR_LLM_API_KEY", "LLM_API_KEY", "OPENROUTER_API_KEY"}
	llmBaseURLEnvVars = []string{"DZZZR_LLM_BASE_URL", "LLM_BASE_URL", "OPENROUTER_BASE_URL"}
	llmModelEnvVars   = []string{"DZZZR_LLM_MODEL", "LLM_MODEL", "OPENROUTER_MODEL"}
	// The ChatGPT backend serves OpenAI models only, so an OpenRouter model
	// name exported for the other transport is not a choice made for it.
	llmCodexModelEnvVars = []string{"DZZZR_LLM_MODEL", "LLM_MODEL"}
)

// firstEnv returns the first non-empty variable among names and the name that
// carried it.
func firstEnv(names []string) (value, from string) {
	for _, name := range names {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v, name
		}
	}
	return "", ""
}

// llmFieldSource is where one resolved field came from.
type llmFieldSource struct {
	Value  string `json:"value"`
	Source string `json:"source"`            // env | settings | default | unset
	EnvVar string `json:"env_var,omitempty"` // the variable that won, when source is env
}

const (
	llmSourceEnv      = "env"
	llmSourceSettings = "settings"
	llmSourceDefault  = "default"
	llmSourceUnset    = "unset"
)

// resolveLLMField applies the precedence the user was promised: the
// environment, then what the settings panel stored, then the default.
//
// The environment deliberately outranks the settings file. A key exported into
// a shell or a container is the deployment speaking, and it must not be
// overridden by a value someone typed into the panel weeks ago — so the panel
// reports the shadowing instead of losing to it silently.
func resolveLLMField(envNames []string, stored, def string) llmFieldSource {
	if v, from := firstEnv(envNames); v != "" {
		return llmFieldSource{Value: v, Source: llmSourceEnv, EnvVar: from}
	}
	if v := strings.TrimSpace(stored); v != "" {
		return llmFieldSource{Value: v, Source: llmSourceSettings}
	}
	if def != "" {
		return llmFieldSource{Value: def, Source: llmSourceDefault}
	}
	return llmFieldSource{Source: llmSourceUnset}
}

// validateLLMBaseURL rejects an endpoint the transport could never reach, so
// the settings panel fails at save time rather than on the next message.
func validateLLMBaseURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("адрес API не является URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("адрес API должен начинаться с http:// или https://, получено %q", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("в адресе API нет хоста: %q", raw)
	}
	return nil
}

// maskAPIKey renders a stored key for display. Only the tail is shown: enough
// to tell two keys apart and useless to anyone reading the response.
func maskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	const tail = 4
	if len(key) <= tail {
		return strings.Repeat("•", len(key))
	}
	return "••••" + key[len(key)-tail:]
}
