package main

import (
	"cmp"
	"errors"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/skrashevich/dzzzr-cli/agentloop"
)

// The web settings panel configures the transports a user can set up from a
// browser: an OpenAI-compatible provider (Polza.ai among them), and a ChatGPT
// subscription.
var llmSettableAuthMethods = []string{authMethodAPIKey, authMethodCodex}

// llmAPIKeyStatus describes the key without disclosing it. The panel needs to
// know that a key exists and which one, not what it is: the server binds to localhost
// by default but the response would otherwise put the key in every browser cache
// and devtools log that ever displayed the panel.
type llmAPIKeyStatus struct {
	HasValue bool   `json:"has_value"`
	Masked   string `json:"masked,omitempty"`
	Source   string `json:"source"`
	EnvVar   string `json:"env_var,omitempty"`
}

type llmEffectiveSettings struct {
	AuthMethod llmFieldSource  `json:"auth_method"`
	BaseURL    llmFieldSource  `json:"base_url"`
	Model      llmFieldSource  `json:"model"`
	APIKey     llmAPIKeyStatus `json:"api_key"`
}

// llmStoredSettings is the file's own content, which is what the form edits.
// It is reported separately from the effective values so a field shadowed by
// the environment still shows the user what they saved.
type llmStoredSettings struct {
	AuthMethod   string `json:"auth_method"`
	BaseURL      string `json:"base_url"`
	Model        string `json:"model"`
	HasAPIKey    bool   `json:"has_api_key"`
	APIKeyMasked string `json:"api_key_masked,omitempty"`
}

// llmEnvOverride is one field the panel cannot change from here.
type llmEnvOverride struct {
	Field string `json:"field"`
	// Source is env; EnvVar names the variable.
	Source string `json:"source"`
	EnvVar string `json:"env_var,omitempty"`
	// ShadowsStored marks the case worth a loud badge: something IS saved for
	// this field and it is not the value in use.
	ShadowsStored bool `json:"shadows_stored"`
}

// envOverrideField pairs one resolved field with what the settings file holds
// for it, which is all collectEnvOverrides needs in order to judge it.
type envOverrideField struct {
	Field  string
	Source llmFieldSource
	Stored string
}

// collectEnvOverrides picks out the fields the settings panel cannot change
// from where it is: it may only offer to edit what it stores, so a value that
// arrived from the environment has to be explained rather than silently lost to
// the form.
func collectEnvOverrides(fields []envOverrideField) []llmEnvOverride {
	var out []llmEnvOverride
	for _, f := range fields {
		if f.Source.Source != llmSourceEnv {
			continue
		}
		out = append(out, llmEnvOverride{
			Field:         f.Field,
			Source:        f.Source.Source,
			EnvVar:        f.Source.EnvVar,
			ShadowsStored: strings.TrimSpace(f.Stored) != "",
		})
	}
	return out
}

type llmCodexStatus struct {
	SignedIn bool `json:"signed_in"`
	// CLISignIn reports that Codex CLI is signed in on this machine. dzzzr runs
	// on that sign-in when it has none of its own and ChatGPT is chosen.
	CLISignIn bool `json:"cli_signed_in"`
	// CLIPath is the Codex CLI file that sign-in was looked for in.
	CLIPath   string `json:"cli_path,omitempty"`
	AccountID string `json:"account_id,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Expired   bool   `json:"expired"`
	Error     string `json:"error,omitempty"`
	Path      string `json:"path"`
}

// llmAgentSummary is what resolveLLMConfig would hand the agent right now, so
// the panel can show the outcome rather than make the user infer it.
type llmAgentSummary struct {
	AuthMethod string `json:"auth_method"`
	Model      string `json:"model"`
	BaseURL    string `json:"base_url"`
	Error      string `json:"error,omitempty"`
}

type llmSettingsPayload struct {
	Effective    llmEffectiveSettings `json:"effective"`
	Stored       llmStoredSettings    `json:"stored"`
	EnvOverrides []llmEnvOverride     `json:"env_overrides"`
	Defaults     map[string]string    `json:"defaults"`
	AuthMethods  []string             `json:"auth_methods"`
	Codex        llmCodexStatus       `json:"codex"`
	Agent        llmAgentSummary      `json:"agent"`
	SettingsPath string               `json:"settings_path"`
	Error        string               `json:"error,omitempty"`
}

// llmSettingsUpdate is the form submission.
//
// The non-secret fields are replaced verbatim, so clearing one in the UI clears
// it in the file. The key cannot work that way: the form never receives it, so
// an empty string means "leave it alone" and clearing is an explicit act.
type llmSettingsUpdate struct {
	AuthMethod  string `json:"auth_method"`
	BaseURL     string `json:"base_url"`
	Model       string `json:"model"`
	APIKey      string `json:"api_key"`
	ClearAPIKey bool   `json:"clear_api_key"`
}

func (h *webHub) httpLLMSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.httpGetLLMSettings(w, r)
	case http.MethodPut:
		h.httpPutLLMSettings(w, r)
	case http.MethodDelete:
		h.httpDeleteLLMSettings(w, r)
	default:
		webError(w, http.StatusMethodNotAllowed, "метод не поддерживается")
	}
}

func (h *webHub) httpGetLLMSettings(w http.ResponseWriter, r *http.Request) {
	payload, err := h.llmSettingsPayload()
	if err != nil {
		// A settings file that does not parse still has to be reportable, or the
		// panel that exists to fix it cannot open.
		payload.Error = err.Error()
	}
	webWriteJSON(w, http.StatusOK, payload)
}

func (h *webHub) httpPutLLMSettings(w http.ResponseWriter, r *http.Request) {
	var req llmSettingsUpdate
	if !webReadJSON(w, r, &req) {
		return
	}
	authMethod, err := normalizeAuthMethod(req.AuthMethod)
	if err != nil || (authMethod != "" && !slices.Contains(llmSettableAuthMethods, authMethod)) {
		webError(w, http.StatusBadRequest, "неизвестный способ подключения %q: используйте %s", req.AuthMethod, strings.Join(llmSettableAuthMethods, ", "))
		return
	}
	if err := validateLLMBaseURL(req.BaseURL); err != nil {
		webError(w, http.StatusBadRequest, "%s", err.Error())
		return
	}

	// Start from what is stored so the key survives a form that never saw it.
	// A file that does not parse is replaced rather than refused: the user came
	// here to set the transport, and there is nothing worth preserving.
	current, err := loadLLMSettings()
	if err != nil {
		h.cfg.debugf("настройки LLM не прочитаны и будут заменены: %v", err)
		current = llmSettings{}
	}
	next := llmSettings{
		AuthMethod: authMethod,
		BaseURL:    strings.TrimSpace(req.BaseURL),
		Model:      strings.TrimSpace(req.Model),
		APIKey:     current.APIKey,
	}
	switch {
	case req.ClearAPIKey:
		next.APIKey = ""
	case strings.TrimSpace(req.APIKey) != "":
		next.APIKey = strings.TrimSpace(req.APIKey)
	case next.APIKey != "" && next.AuthMethod != authMethodCodex && !sameLLMHost(current.BaseURL, next.BaseURL):
		// The kept key was issued by the provider the form is moving away
		// from; carried along, it would be sent to the new one on the next
		// turn. A server on this machine needs no key, so it is dropped there;
		// anywhere else the user has to say which key to use.
		if !isLocalEndpoint(cmp.Or(next.BaseURL, defaultLLMBaseURL)) {
			webError(w, http.StatusBadRequest, "адрес API изменился: введите ключ для нового провайдера")
			return
		}
		next.APIKey = ""
	}

	if err := saveLLMSettings(next); err != nil {
		webError(w, http.StatusInternalServerError, "%s", err.Error())
		return
	}
	payload, err := h.llmSettingsPayload()
	if err != nil {
		payload.Error = err.Error()
	}
	webWriteJSON(w, http.StatusOK, payload)
}

func (h *webHub) httpDeleteLLMSettings(w http.ResponseWriter, r *http.Request) {
	if err := deleteLLMSettings(); err != nil {
		webError(w, http.StatusInternalServerError, "%s", err.Error())
		return
	}
	payload, err := h.llmSettingsPayload()
	if err != nil {
		payload.Error = err.Error()
	}
	webWriteJSON(w, http.StatusOK, payload)
}

// llmSettingsPayload assembles what the panel renders: the stored form values,
// the values actually in force, and where each of those came from.
func (h *webHub) llmSettingsPayload() (llmSettingsPayload, error) {
	stored, loadErr := loadLLMSettings()

	authField := resolveLLMField(llmAuthEnvVars, stored.AuthMethod, "")
	baseField := resolveLLMField(llmBaseURLEnvVars, stored.BaseURL, defaultLLMBaseURL)
	keyField := resolveLLMField(llmAPIKeyEnvVars, stored.APIKey, "")
	agentCfg, agentErr := llmConfig()
	modelField := resolveLLMField(llmModelEnvVars, stored.Model, defaultLLMModel)
	if agentAuthMethod(agentCfg) == authMethodCodex {
		modelField = codexModelField(stored.Model, agentCfg.Model)
	}

	payload := llmSettingsPayload{
		Effective: llmEffectiveSettings{
			AuthMethod: authField,
			BaseURL:    baseField,
			Model:      modelField,
			APIKey: llmAPIKeyStatus{
				HasValue: keyField.Value != "",
				Masked:   maskAPIKey(keyField.Value),
				Source:   keyField.Source,
				EnvVar:   keyField.EnvVar,
			},
		},
		Stored: llmStoredSettings{
			AuthMethod:   stored.AuthMethod,
			BaseURL:      stored.BaseURL,
			Model:        stored.Model,
			HasAPIKey:    stored.APIKey != "",
			APIKeyMasked: maskAPIKey(stored.APIKey),
		},
		Defaults: map[string]string{
			"base_url": defaultLLMBaseURL,
			"model":    defaultLLMModel,
		},
		AuthMethods: llmSettableAuthMethods,
		Codex:       codexStatusForWeb(),
	}
	payload.SettingsPath, _ = llmSettingsFile()

	payload.EnvOverrides = collectEnvOverrides([]envOverrideField{
		{"auth_method", authField, stored.AuthMethod},
		{"base_url", baseField, stored.BaseURL},
		{"model", modelField, stored.Model},
		{"api_key", keyField, stored.APIKey},
	})

	payload.Agent = llmAgentSummary{
		AuthMethod: agentAuthMethod(agentCfg),
		Model:      agentCfg.Model,
		BaseURL:    agentCfg.BaseURL,
	}
	if agentErr != nil {
		payload.Agent.Error = agentErr.Error()
	}
	return payload, loadErr
}

// codexStatusForWeb reports the stored ChatGPT sign-in for the panel. A missing
// credential is a state, not a failure, so only a broken file carries an error.
func codexStatusForWeb() llmCodexStatus {
	var status llmCodexStatus
	status.Path, _ = codexAuthFile()
	// The Codex CLI file the agent would fall back to: the one named by
	// DZZZR_CODEX_AUTH_FILE when it is set, Codex's own otherwise.
	cliPath := os.Getenv("DZZZR_CODEX_AUTH_FILE")
	if cliPath == "" {
		cliPath, _ = codexCLIAuthFile()
	}
	if cliPath != "" {
		_, _, cliErr := readCodexAuth(cliPath)
		status.CLISignIn = cliErr == nil
		status.CLIPath = cliPath
	}
	cred, err := loadCodexCredential()
	if err != nil {
		if !errors.Is(err, errNoCodexCredential) {
			status.Error = err.Error()
		}
		return status
	}
	status.SignedIn = true
	status.AccountID = cred.AccountID
	if !cred.ExpiresAt.IsZero() {
		status.ExpiresAt = cred.ExpiresAt.Format(time.RFC3339)
		status.Expired = time.Now().After(cred.ExpiresAt)
	}
	return status
}

// codexModelField is the model field as a subscription run sees it: only the
// variables it reads (not OPENROUTER_MODEL), and the model it ends up with
// rather than an OpenRouter name it will not serve.
func codexModelField(stored, running string) llmFieldSource {
	if env := resolveLLMField(llmCodexModelEnvVars, "", ""); env.Source == llmSourceEnv {
		return env
	}
	name := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(stored)), "openai/")
	if name != "" && name == running {
		return llmFieldSource{Value: running, Source: llmSourceSettings}
	}
	return llmFieldSource{Value: running, Source: llmSourceDefault}
}

// agentAuthMethod names the transport a resolved configuration runs on.
func agentAuthMethod(cfg agentloop.Config) string {
	switch {
	case cfg.BaseURL == "" && cfg.Model == "":
		return ""
	case cfg.BaseURL == codexBaseURL:
		return authMethodCodex
	default:
		return authMethodAPIKey
	}
}

// sameLLMHost reports whether two stored base URLs reach the same host; an
// empty one stands for the default endpoint.
func sameLLMHost(a, b string) bool {
	host := func(raw string) string {
		u, err := url.Parse(cmp.Or(strings.TrimSpace(raw), defaultLLMBaseURL))
		if err != nil {
			return raw
		}
		return strings.ToLower(u.Host)
	}
	return host(a) == host(b)
}
