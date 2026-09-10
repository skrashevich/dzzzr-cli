package main

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/skrashevich/dzzzr-cli/agentloop"
)

// codexLLMConfig uses the subscription endpoint, never the generic API key or
// base URL. Credentials remain owned by Codex and are reread before each call.
func codexLLMConfig() (agentloop.Config, error) {
	path := os.Getenv("DZZZR_CODEX_AUTH_FILE")
	if path == "" {
		dir := os.Getenv("CODEX_HOME")
		if dir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return agentloop.Config{}, err
			}
			dir = filepath.Join(home, ".codex")
		}
		path = filepath.Join(dir, "auth.json")
	}
	source := func() (string, string, error) { return readCodexAuth(path) }
	token, account, err := source()
	if err != nil {
		return agentloop.Config{}, err
	}
	p := providers.NewCodexProviderWithTokenSource(token, account, source)
	model := cmp.Or(os.Getenv("DZZZR_LLM_MODEL"), os.Getenv("LLM_MODEL"), p.GetDefaultModel())
	model = strings.ToLower(strings.TrimSpace(model))
	model = strings.TrimPrefix(model, "openai/")
	// PicoClaw otherwise silently substitutes its default for another family.
	if strings.Contains(model, "/") || !(strings.HasPrefix(model, "gpt-") || strings.HasPrefix(model, "o3") || strings.HasPrefix(model, "o4")) {
		return agentloop.Config{}, fatal("модель %q несовместима с Codex: задайте DZZZR_LLM_MODEL с именем модели OpenAI", model)
	}
	return agentloop.Config{
		Model:    model,
		BaseURL:  "https://chatgpt.com/backend-api/codex",
		Provider: &codexErrorProvider{LLMProvider: p},
	}, nil
}

// Codex returns some failures as {"detail": ...}, whereas the SDK expects
// {"error": ...}. Recover just that message, never dump request credentials.
type codexErrorProvider struct{ providers.LLMProvider }

func (p *codexErrorProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]any) (*providers.LLMResponse, error) {
	response, err := p.LLMProvider.Chat(ctx, messages, tools, model, options)
	apiErr, ok := errors.AsType[*openai.Error](err)
	if !ok || apiErr.Message != "" || apiErr.Response == nil || apiErr.Response.Body == nil {
		return response, err
	}
	defer apiErr.Response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(apiErr.Response.Body, 64<<10))
	var detail struct {
		Detail string `json:"detail"`
	}
	if readErr == nil && json.Unmarshal(body, &detail) == nil && strings.TrimSpace(detail.Detail) != "" {
		return response, fmt.Errorf("%w: %s", err, detail.Detail)
	}
	return response, err
}

func readCodexAuth(path string) (string, string, error) {
	fail := func(reason string) (string, string, error) {
		return "", "", fmt.Errorf("авторизация ChatGPT/Codex (%s): %s; выполните codex -c 'cli_auth_credentials_store=\"file\"' login через ChatGPT и проверьте CODEX_HOME или DZZZR_CODEX_AUTH_FILE", path, reason)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fail("не удалось прочитать файл авторизации")
	}
	var auth struct {
		Mode   string `json:"auth_mode"`
		Tokens struct {
			AccessToken string `json:"access_token"`
			AccountID   string `json:"account_id"`
		} `json:"tokens"`
	}
	// Parser errors may contain parts of the credential: never include them.
	if err := json.Unmarshal(data, &auth); err != nil {
		return fail("неверный формат JSON")
	}
	if auth.Mode != "" && auth.Mode != "chatgpt" {
		return fail("нужен вход через ChatGPT, а не API key")
	}
	if strings.TrimSpace(auth.Tokens.AccessToken) == "" || strings.TrimSpace(auth.Tokens.AccountID) == "" {
		return fail("нет access_token или account_id")
	}
	// The JWT expiry is a local hint, not signature verification. The service
	// validates the token. File modification time does not measure token validity.
	parts := strings.Split(auth.Tokens.AccessToken, ".")
	if len(parts) == 3 {
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		var claims struct {
			Exp int64 `json:"exp"`
		}
		if err == nil && json.Unmarshal(payload, &claims) == nil && claims.Exp > 0 && time.Now().Unix() >= claims.Exp {
			return fail("срок действия токена истёк; обновите вход в Codex")
		}
	}
	return auth.Tokens.AccessToken, auth.Tokens.AccountID, nil
}
