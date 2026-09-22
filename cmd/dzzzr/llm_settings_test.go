package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const codexEndpoint = "https://chatgpt.com/backend-api/codex"

// isolateLLMEnv is isolate for the LLM tests: an empty environment and a fresh
// configuration directory, which it returns. The settings, the onboarding state
// and the ChatGPT sign-in all live under that directory.
func isolateLLMEnv(t *testing.T) string {
	t.Helper()
	isolate(t)
	return os.Getenv("DZZZR_CONFIG_DIR")
}

// writeTestLLMSettings stores settings through the production writer, so the
// tests read back exactly what the settings panel would have produced.
func writeTestLLMSettings(t *testing.T, s llmSettings) {
	t.Helper()
	if err := saveLLMSettings(s); err != nil {
		t.Fatalf("saveLLMSettings: %v", err)
	}
}

// useCodexCLIFixture signs Codex CLI in, the way every dzzzr release before the
// web sign-in expected to find it.
func useCodexCLIFixture(t *testing.T) {
	t.Helper()
	path := codexAuthFixture(t, "cli-token", "cli-account")
	t.Setenv("CODEX_HOME", filepath.Dir(path))
}

func TestLLMSettingsRoundTrip(t *testing.T) {
	isolateLLMEnv(t)
	writeTestLLMSettings(t, llmSettings{
		AuthMethod: "  APIKEY ",
		BaseURL:    " https://api.example.com/v1 ",
		APIKey:     " sk-file-key ",
		Model:      " gpt-4o ",
	})

	got, err := loadLLMSettings()
	if err != nil {
		t.Fatalf("loadLLMSettings: %v", err)
	}
	want := llmSettings{
		AuthMethod: authMethodAPIKey,
		BaseURL:    "https://api.example.com/v1",
		APIKey:     "sk-file-key",
		Model:      "gpt-4o",
	}
	if got != want {
		t.Fatalf("settings = %+v, want %+v (the writer and the reader must both trim)", got, want)
	}

	if err := deleteLLMSettings(); err != nil {
		t.Fatalf("deleteLLMSettings: %v", err)
	}
	if got, err := loadLLMSettings(); err != nil || got != (llmSettings{}) {
		t.Fatalf("after delete: settings = %+v, err = %v", got, err)
	}
	if err := deleteLLMSettings(); err != nil {
		t.Fatalf("deleteLLMSettings on a missing file: %v", err)
	}
}

// The file holds an API key, so neither it nor its directory may be readable
// by another account on the machine.
func TestLLMSettingsFilePermissions(t *testing.T) {
	isolateLLMEnv(t)
	path := filepath.Join(t.TempDir(), "llm", "settings.json")
	t.Setenv(llmSettingsFileEnvVar, path)

	writeTestLLMSettings(t, llmSettings{APIKey: "sk-file-key"})
	checkFileMode(t, path, 0o600)
	checkFileMode(t, filepath.Dir(path), 0o700)

	// os.WriteFile applies its mode only when it creates the file, so a save
	// over a world-readable file has to tighten it rather than inherit it.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	writeTestLLMSettings(t, llmSettings{APIKey: "sk-file-key-2"})
	checkFileMode(t, path, 0o600)
}

func TestLLMSettingsMissingFileIsEmpty(t *testing.T) {
	isolateLLMEnv(t)
	t.Setenv(llmSettingsFileEnvVar, filepath.Join(t.TempDir(), "absent", "settings.json"))

	got, err := loadLLMSettings()
	if err != nil || got != (llmSettings{}) {
		t.Fatalf("settings = %+v, err = %v, want the zero value", got, err)
	}
}

func TestLLMSettingsEmptyFileIsEmpty(t *testing.T) {
	isolateLLMEnv(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	t.Setenv(llmSettingsFileEnvVar, path)
	if err := os.WriteFile(path, []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadLLMSettings()
	if err != nil || got != (llmSettings{}) {
		t.Fatalf("settings = %+v, err = %v, want the zero value", got, err)
	}
}

func TestLLMSettingsRejectsBrokenJSON(t *testing.T) {
	isolateLLMEnv(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	t.Setenv(llmSettingsFileEnvVar, path)
	if err := os.WriteFile(path, []byte(`{"auth_method": `), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadLLMSettings()
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("err = %v, want it to name the file to delete", err)
	}
}

// Settings must survive a logout, which deletes <config>/<city>.json.
func TestLLMSettingsLiveInTheirOwnDirectory(t *testing.T) {
	dir := isolateLLMEnv(t)
	path, err := llmSettingsFile()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "llm", "settings.json"); path != want {
		t.Fatalf("settings path = %s, want %s", path, want)
	}
}

func TestLLMSettingsValidateBaseURL(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		wantErr bool
	}{
		{"", false},
		{"   ", false},
		{"https://api.example.com/v1", false},
		{"http://127.0.0.1:8317/v1", false},
		{"ftp://x", true},
		{"не-url", true},
		{"api.example.com/v1", true},
		{"https://", true},
		{"://nope", true},
	} {
		err := validateLLMBaseURL(tc.raw)
		if tc.wantErr != (err != nil) {
			t.Errorf("validateLLMBaseURL(%q) = %v, wantErr %v", tc.raw, err, tc.wantErr)
		}
	}
}

func TestLLMSettingsMaskAPIKey(t *testing.T) {
	if got := maskAPIKey("   "); got != "" {
		t.Errorf("maskAPIKey on blank = %q", got)
	}
	const key = "sk-secret-value-1234"
	masked := maskAPIKey(key)
	if strings.Contains(masked, "sk-secret") || !strings.HasSuffix(masked, "1234") {
		t.Fatalf("maskAPIKey(%q) = %q", key, masked)
	}
	if got := maskAPIKey("abcd"); strings.Contains(got, "abcd") {
		t.Errorf("maskAPIKey(%q) = %q, which discloses the whole key", "abcd", got)
	}
}

func TestLLMSettingsFirstEnvNamesTheWinner(t *testing.T) {
	isolateLLMEnv(t)
	if value, from := firstEnv(llmAPIKeyEnvVars); value != "" || from != "" {
		t.Fatalf("firstEnv on a clean environment = (%q, %q)", value, from)
	}
	t.Setenv("OPENROUTER_API_KEY", "sk-fallback")
	if value, from := firstEnv(llmAPIKeyEnvVars); value != "sk-fallback" || from != "OPENROUTER_API_KEY" {
		t.Fatalf("firstEnv = (%q, %q)", value, from)
	}
	t.Setenv("LLM_API_KEY", "  sk-generic  ")
	if value, from := firstEnv(llmAPIKeyEnvVars); value != "sk-generic" || from != "LLM_API_KEY" {
		t.Fatalf("firstEnv = (%q, %q)", value, from)
	}
	t.Setenv("DZZZR_LLM_API_KEY", "sk-dzzzr")
	if value, from := firstEnv(llmAPIKeyEnvVars); value != "sk-dzzzr" || from != "DZZZR_LLM_API_KEY" {
		t.Fatalf("firstEnv = (%q, %q)", value, from)
	}
}

func TestLLMSettingsResolveLLMFieldPrecedence(t *testing.T) {
	isolateLLMEnv(t)
	if got := resolveLLMField(llmModelEnvVars, "", ""); got.Source != llmSourceUnset || got.Value != "" {
		t.Fatalf("nothing configured = %+v", got)
	}
	if got := resolveLLMField(llmModelEnvVars, "", "def"); got.Source != llmSourceDefault || got.Value != "def" {
		t.Fatalf("default only = %+v", got)
	}
	if got := resolveLLMField(llmModelEnvVars, " stored ", "def"); got.Source != llmSourceSettings || got.Value != "stored" {
		t.Fatalf("stored over default = %+v", got)
	}
	t.Setenv("LLM_MODEL", "from-env")
	got := resolveLLMField(llmModelEnvVars, "stored", "def")
	if got.Source != llmSourceEnv || got.Value != "from-env" || got.EnvVar != "LLM_MODEL" {
		t.Fatalf("env over stored = %+v", got)
	}
}

// --- resolveLLMConfig: environment > settings file > default ---

func TestResolveLLMConfigUsesStoredSettings(t *testing.T) {
	isolateLLMEnv(t)
	writeTestLLMSettings(t, llmSettings{
		BaseURL: "https://api.example.com/v1",
		APIKey:  "sk-file-key",
		Model:   "file/model",
	})
	got, err := resolveLLMConfig()
	if err != nil {
		t.Fatalf("resolveLLMConfig: %v", err)
	}
	if got.APIKey != "sk-file-key" || got.BaseURL != "https://api.example.com/v1" || got.Model != "file/model" || got.Provider != nil {
		t.Fatalf("config = %+v, want the stored transport", got)
	}
}

func TestResolveLLMConfigUsesAStoredLocalEndpointWithoutAKey(t *testing.T) {
	isolateLLMEnv(t)
	writeTestLLMSettings(t, llmSettings{BaseURL: "http://127.0.0.1:8317/v1"})
	got, err := resolveLLMConfig()
	if err != nil {
		t.Fatalf("resolveLLMConfig: %v", err)
	}
	if got.BaseURL != "http://127.0.0.1:8317/v1" || got.Model != defaultLLMModel {
		t.Fatalf("config = %+v", got)
	}
}

func TestResolveLLMConfigEnvironmentOutranksStoredSettings(t *testing.T) {
	for _, tc := range []struct {
		name, envVar, env string
		stored            llmSettings
		ok                func(cfgBaseURL, cfgKey, cfgModel string) bool
	}{
		{"api key", "DZZZR_LLM_API_KEY", "sk-env", llmSettings{APIKey: "sk-file"},
			func(_, key, _ string) bool { return key == "sk-env" }},
		{"base url", "LLM_BASE_URL", "https://env.example.com/v1", llmSettings{APIKey: "k", BaseURL: "https://file.example.com/v1"},
			func(u, _, _ string) bool { return u == "https://env.example.com/v1" }},
		{"model", "OPENROUTER_MODEL", "env/model", llmSettings{APIKey: "k", Model: "file/model"},
			func(_, _, m string) bool { return m == "env/model" }},
		{"auth method", "DZZZR_LLM_PROVIDER", "codex", llmSettings{AuthMethod: authMethodAPIKey, APIKey: "k"},
			func(u, _, _ string) bool { return u == codexEndpoint }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateLLMEnv(t)
			useCodexCLIFixture(t)
			writeTestLLMSettings(t, tc.stored)
			t.Setenv(tc.envVar, tc.env)
			got, err := resolveLLMConfig()
			if err != nil {
				t.Fatalf("resolveLLMConfig: %v", err)
			}
			if !tc.ok(got.BaseURL, got.APIKey, got.Model) {
				t.Fatalf("config = %+v, want the environment's value", got)
			}
		})
	}
}

func TestResolveLLMConfigDZZZREnvOutranksGenericAndStored(t *testing.T) {
	isolateLLMEnv(t)
	writeTestLLMSettings(t, llmSettings{APIKey: "stored", Model: "stored/model"})
	t.Setenv("LLM_MODEL", "generic/model")
	t.Setenv("DZZZR_LLM_MODEL", "dzzzr/model")
	cfg, err := resolveLLMConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "dzzzr/model" || cfg.APIKey != "stored" || cfg.BaseURL != defaultLLMBaseURL {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestResolveLLMConfigStoredProviderOpenRouterIsAPIKeyPath(t *testing.T) {
	isolateLLMEnv(t)
	writeTestLLMSettings(t, llmSettings{AuthMethod: "openrouter", APIKey: "k"})
	cfg, err := resolveLLMConfig()
	if err != nil || cfg.APIKey != "k" || cfg.Model != defaultLLMModel {
		t.Fatalf("cfg = %+v, err = %v", cfg, err)
	}
}

func TestResolveLLMConfigStoredAuthMethodSelectsCodex(t *testing.T) {
	isolateLLMEnv(t)
	useCodexCLIFixture(t)
	// A stored base URL and an OpenRouter model left from an earlier setup must
	// not divert the transport once ChatGPT was chosen.
	writeTestLLMSettings(t, llmSettings{AuthMethod: authMethodCodex, BaseURL: "https://file.example.com/v1", Model: "anthropic/claude"})
	got, err := resolveLLMConfig()
	if err != nil {
		t.Fatalf("resolveLLMConfig: %v", err)
	}
	if got.BaseURL != codexEndpoint || got.Provider == nil || got.APIKey != "" {
		t.Fatalf("config = %+v, want the subscription", got)
	}
	if strings.Contains(got.Model, "/") {
		t.Fatalf("model = %q, want a model the ChatGPT backend serves", got.Model)
	}
}

func TestResolveLLMConfigRejectsAnUnknownStoredAuthMethod(t *testing.T) {
	isolateLLMEnv(t)
	writeTestLLMSettings(t, llmSettings{AuthMethod: "gigachat"})
	_, err := resolveLLMConfig()
	if err == nil || !strings.Contains(err.Error(), "gigachat") {
		t.Fatalf("error = %v, want the rejected value named", err)
	}
}

func TestResolveLLMConfigBrokenSettingsFileIsAnError(t *testing.T) {
	dir := isolateLLMEnv(t)
	path := filepath.Join(dir, "llm", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DZZZR_LLM_API_KEY", "k")
	if _, err := resolveLLMConfig(); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveLLMConfigMissingKeyPointsAtTheSettingsPanel(t *testing.T) {
	isolateLLMEnv(t)
	_, err := resolveLLMConfig()
	if err == nil || !strings.Contains(err.Error(), "Настройки LLM") || !strings.Contains(err.Error(), "DZZZR_LLM_API_KEY") {
		t.Fatalf("err = %v", err)
	}
}
