package main

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/skrashevich/dzzzr-cli/agentfiles"
	"github.com/skrashevich/dzzzr-cli/agentloop"
	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// Defaults for the model the agent talks to. OpenRouter is the default because
// it is the one endpoint that serves every model behind one key.
const (
	defaultLLMBaseURL = "https://openrouter.ai/api/v1"
	defaultLLMModel   = "openrouter/auto"
)

// readCacheTTL memoizes a read for as long as it stays worth trusting. The
// game moves, but a model that asks for the level three times in one turn
// should not cost three requests.
const readCacheTTL = 20 * time.Second

// agentAuthorize prepares the client the tools will use. A missing session is
// not a hard error: the agent starts anyway and its tools report the
// authentication failure, which is more useful than a process that refuses to
// launch.
func agentAuthorize(ctx context.Context, cfg *config, c *dzzzr.Client) error {
	path, err := sessionPath(cfg.city)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(path); statErr == nil || canSignIn(cfg) {
		return requireAuth(ctx, cfg, c)
	}
	if _, _, err := loadSession(cfg, c); err != nil {
		return err
	}
	applyCredentialOverrides(cfg, c)
	_, _ = fmt.Fprintf(cfg.stderr, "Сессия не найдена (%s): для доступа к API задайте -login и -password или выполните «dzzzr login».\n", path)
	return nil
}

// newAgentCatalog builds the engine toolset for one agent surface. The
// confirmer is consulted only under the approve policy, so a surface that
// cannot ask passes nil and must not select that policy.
func newAgentCatalog(cfg *config, c *dzzzr.Client, confirmer agenttools.Confirmer) (*agenttools.Catalog, error) {
	policy, err := agenttools.ParsePolicy(cfg.security)
	if err != nil {
		return nil, fatal("неверное значение -security %q: допустимы readonly, approve и full", cfg.security)
	}
	opts := agenttools.Options{
		Policy:       policy,
		ReadCacheTTL: readCacheTTL,
		IncludeAdmin: c.HasAdminCredentials(),
	}
	if canSignIn(cfg) {
		opts.Reauthenticate = func(ctx context.Context) error {
			c.Logout()
			return signIn(ctx, cfg, c)
		}
	}
	if policy == agenttools.PolicyApprove {
		opts.Confirmer = confirmer
	}
	catalog, err := agenttools.NewCatalog(c, opts)
	if err != nil {
		return nil, fatal("не удалось собрать набор инструментов: %v", err)
	}
	return catalog, nil
}

// llmConfig resolves the provider the agent talks to. The DZZZR_-prefixed names
// win, so a machine that already runs another agent keeps both configured.
func llmConfig() (agentloop.Config, error) {
	out, err := resolveLLMConfig()
	if err != nil {
		return out, err
	}
	if value := os.Getenv("DZZZR_LLM_SOURCE_CONTEXT_BYTES"); value != "" {
		budget, err := strconv.Atoi(value)
		if err != nil || budget < 1 || budget > 16<<20 {
			return agentloop.Config{}, fatal("DZZZR_LLM_SOURCE_CONTEXT_BYTES должен быть числом от 1 до 16777216")
		}
		out.SourceContextBytes = budget
	}
	if value := os.Getenv("DZZZR_LLM_REQUEST_TIMEOUT_SECONDS"); value != "" {
		seconds, err := strconv.Atoi(value)
		if err != nil || seconds < 1 || seconds > 3600 {
			return agentloop.Config{}, fatal("DZZZR_LLM_REQUEST_TIMEOUT_SECONDS должен быть числом от 1 до 3600")
		}
		out.RequestTimeout = time.Duration(seconds) * time.Second
	}
	return out, nil
}

func resolveLLMConfig() (agentloop.Config, error) {
	switch provider := strings.ToLower(strings.TrimSpace(os.Getenv("DZZZR_LLM_PROVIDER"))); provider {
	case "codex", "chatgpt":
		out, err := codexLLMConfig()
		if err == nil && agentConfigHook != nil {
			agentConfigHook(&out)
		}
		return out, err
	case "", "openai", "openrouter":
	default:
		return agentloop.Config{}, fatal("неизвестный DZZZR_LLM_PROVIDER %q: используйте openai, openrouter или codex", provider)
	}
	baseURL := cmp.Or(
		os.Getenv("DZZZR_LLM_BASE_URL"),
		os.Getenv("LLM_BASE_URL"),
		os.Getenv("OPENROUTER_BASE_URL"),
		defaultLLMBaseURL,
	)
	apiKey := cmp.Or(
		os.Getenv("DZZZR_LLM_API_KEY"),
		os.Getenv("LLM_API_KEY"),
		os.Getenv("OPENROUTER_API_KEY"),
	)
	model := cmp.Or(
		os.Getenv("DZZZR_LLM_MODEL"),
		os.Getenv("LLM_MODEL"),
		os.Getenv("OPENROUTER_MODEL"),
		defaultLLMModel,
	)

	// A proxy on this machine usually wants no key at all; anywhere else a
	// missing key only produces a rejected request several seconds later.
	if apiKey == "" && !isLocalEndpoint(baseURL) {
		return agentloop.Config{}, fatal(
			"не задан ключ модели: укажите DZZZR_LLM_API_KEY (или LLM_API_KEY, OPENROUTER_API_KEY)")
	}

	out := agentloop.Config{
		APIKey:    apiKey,
		Model:     model,
		BaseURL:   baseURL,
		UserAgent: "dzzzr-cli/" + version,
	}
	if agentConfigHook != nil {
		agentConfigHook(&out)
	}
	return out, nil
}

// agentConfigHook lets a test point a run at a provider of its own. It is
// applied after the environment has been read, so the test still exercises
// the resolution above.
var agentConfigHook func(*agentloop.Config)

// isLocalEndpoint reports whether the model is served from this machine.
func isLocalEndpoint(baseURL string) bool {
	s := strings.ToLower(baseURL)
	return strings.Contains(s, "localhost") ||
		strings.Contains(s, "127.0.0.1") ||
		strings.Contains(s, "[::1]")
}

// agentFileTools returns the local-file tools, or nothing at all when the root
// cannot be resolved: an agent without them still plays the game.
func agentFileTools(cfg *config) []agentloop.Tool {
	root, err := agentfiles.RootFromEnv()
	if err != nil {
		cfg.debugf("локальные файлы недоступны: %v", err)
		return nil
	}
	list, err := agentfiles.Tools(root)
	if err != nil {
		cfg.debugf("локальные файлы недоступны: %v", err)
		return nil
	}
	out := make([]agentloop.Tool, 0, len(list))
	for _, t := range list {
		out = append(out, t)
	}
	return out
}

// agentSystemPrompt opens the conversation. It states what cannot be inferred
// from the tools — where the agent is, what time it is, and what it must not
// invent — and then hands over to the catalog's own description of the game
// and the active policy.
func agentSystemPrompt(cfg *config, catalog *agenttools.Catalog, withFiles bool) string {
	now := systemPromptNow()
	var b strings.Builder
	b.WriteString("You are an autonomous agent for dzzzr, a command-line client of the Dozor Classic city-game engine.\n")
	_, _ = fmt.Fprintf(&b, "The city segment of this session is: %s\n", cfg.city)
	_, _ = fmt.Fprintf(&b, "The current date and time is: %s (local, RFC3339; %s in UTC).\n",
		now.Format(time.RFC3339), now.UTC().Format(time.RFC3339))
	b.WriteString("\nRules:\n")
	b.WriteString("- TIME: treat the date and time above as authoritative. Never infer today's date from memory. " +
		"Resolve every relative time the user gives against that value and echo the absolute time you computed.\n")
	b.WriteString("- NEVER FABRICATE: do not invent codes, level text, timings or tool results. " +
		"If something is missing, call a tool for it; if the tools cannot supply it, say so plainly.\n")
	if canSignIn(cfg) {
		b.WriteString("- AUTHENTICATION: the CLI holds site credentials privately and automatically reauthenticates and retries a read once when the session expires. To restore access, retry the relevant read tool. Never ask for the password or invent browser/token refresh instructions. If automatic login fails, report the actual tool error.\n")
		b.WriteString("- TOOL ERROR RECOVERY: do not abandon the task after a recoverable tool error. Inspect the diagnostic and try up to two corrected calls for that operation. Fix invalid arguments instead of replaying them unchanged; for invalid save_local_json content use the structured data argument. Preserve all intended source fields when correcting a mapping. Reuse successful results and saved parts. Before retrying a write with uncertain outcome, inspect whether it already succeeded; never duplicate an engine create or overwrite a local artifact. Respect permission refusals and cancellation. If recovery still fails, report the exact error and saved progress.\n")
	}
	b.WriteString("- Call tools one at a time and use each result to decide the next step.\n")
	b.WriteString("- Read the game state again before acting on it: another player may have changed it.\n")
	b.WriteString("- Finish the task you were given. Do not stop halfway and offer to continue.\n")
	if withFiles {
		_, _ = fmt.Fprintf(&b, "- LOCAL FILES: read_local_file, list_local_dir and search_local_files reach the team's own "+
			"notes under %s. You cannot read anything outside that directory.\n", agentfiles.RootEnv)
		b.WriteString("- LOCAL JSON EXPORT: local artifacts can be created under the file root even with readonly engine access. For PDF scenarios alternate index_pdf and save_local_json for SMALL STRUCTURAL MAPPING PARTS (one level per response), then save a version:2 manifest listing part paths and use extract_pdf to create the final file. Save parts while reading, never wait until all pages are read to generate one huge mapping. Reuse saved parts after interruption. Never transcribe document text into save_local_json or assemble_local_json. Other non-PDF JSON can use save_local_json normally. Never overwrite existing files or invent a target game ID for local export. Report success only after a successful extraction result.\n")
	}
	b.WriteString("- Answer in the language the user wrote in. The team plays in Russian.\n\n")
	b.WriteString(catalog.SystemPromptAddendum())
	return b.String()
}

// systemPromptNow is the clock stamped into the system prompt. It is a
// variable so a test can pin it.
var systemPromptNow = time.Now
