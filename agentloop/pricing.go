package agentloop

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// pricing holds what one token costs with the provider the run talks to.
type pricing struct {
	promptPerToken     float64
	completionPerToken float64
	// isLocal marks a proxy on this machine, where a request costs nothing
	// whatever the model would cost upstream.
	isLocal bool
}

// cost returns the price of a run in US dollars.
func (p *pricing) cost(promptTokens, completionTokens int) float64 {
	if p == nil || p.isLocal {
		return 0
	}
	return float64(promptTokens)*p.promptPerToken + float64(completionTokens)*p.completionPerToken
}

// isLocalBaseURL reports whether the endpoint is a proxy on this machine.
func isLocalBaseURL(baseURL string) bool {
	s := strings.ToLower(baseURL)
	return strings.Contains(s, "localhost") || strings.Contains(s, "127.0.0.1") || strings.Contains(s, "[::1]")
}

// pricingCache remembers what a model costs for the life of the process. A
// chat runs the loop once per message, and a price list that has not changed
// is not worth a megabyte-scale download before every answer. A failed lookup
// is cached as well: it will not start working mid-conversation.
var (
	pricingMu    sync.Mutex
	pricingCache = map[string]*pricing{}
)

// fetchPricing resolves what the model costs. Only OpenRouter publishes a
// catalog we can read, so anywhere else the report is printed without a cost
// line rather than with a guessed one. Every failure returns nil: a run must
// not stop because a price list was unavailable.
func fetchPricing(ctx context.Context, baseURL, apiKey, model string, debugf func(string, ...any)) *pricing {
	key := baseURL + "\x00" + model
	pricingMu.Lock()
	cached, ok := pricingCache[key]
	pricingMu.Unlock()
	if ok {
		return cached
	}

	found := lookupPricing(ctx, baseURL, apiKey, model, debugf)
	pricingMu.Lock()
	pricingCache[key] = found
	pricingMu.Unlock()
	return found
}

func lookupPricing(ctx context.Context, baseURL, apiKey, model string, debugf func(string, ...any)) *pricing {
	if isLocalBaseURL(baseURL) {
		return &pricing{isLocal: true}
	}
	if !strings.Contains(strings.ToLower(baseURL), "openrouter.ai") {
		return nil
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	url := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		debugf("тарифы: не удалось собрать запрос: %v", err)
		return nil
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		debugf("тарифы: запрос не выполнен: %v", err)
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		debugf("тарифы: HTTP %d", resp.StatusCode)
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		debugf("тарифы: ответ не прочитан: %v", err)
		return nil
	}

	var catalog struct {
		Data []struct {
			ID      string `json:"id"`
			Pricing *struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &catalog); err != nil {
		debugf("тарифы: ответ не разобран: %v", err)
		return nil
	}

	// A model may be asked for by a variant name — "…:free", "…:extended" —
	// that the catalog lists under its base name.
	base := model
	if i := strings.LastIndex(model, ":"); i > 0 {
		base = model[:i]
	}
	for _, m := range catalog.Data {
		if m.ID != model && m.ID != base {
			continue
		}
		if m.Pricing == nil {
			return nil
		}
		prompt, _ := strconv.ParseFloat(m.Pricing.Prompt, 64)
		completion, _ := strconv.ParseFloat(m.Pricing.Completion, 64)
		return &pricing{promptPerToken: prompt, completionPerToken: completion}
	}

	debugf("тарифы: модель %q не найдена среди %d", model, len(catalog.Data))
	return nil
}
