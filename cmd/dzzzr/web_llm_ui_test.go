package main

import (
	"strings"
	"testing"
)

// llmPanelUIIDs — идентификаторы панели «Настройки LLM». Поведение живёт в
// webui/llm.js, разметка — в webui/index.html; список держит их в согласии.
var llmPanelUIIDs = []string{
	"btn-llm-settings",
	"llm-modal",
	"llm-modal-dialog",
	"llm-modal-title",
	"btn-llm-close",
	"llm-tab-polza",
	"llm-tab-codex",
	"llm-tab-apikey",
	"llm-override-auth_method",
	"llm-transport-note",
	"llm-pane-polza",
	"llm-polza-model",
	"llm-polza-models-retry",
	"llm-polza-login",
	"llm-polza-cancel",
	"llm-polza-check",
	"llm-polza-result",
	"llm-polza-authorize",
	"llm-polza-callback-details",
	"llm-polza-code",
	"llm-polza-submit",
	"llm-polza-key",
	"llm-polza-connect",
	"llm-pane-codex",
	"llm-codex-status",
	"btn-codex-login",
	"btn-codex-logout",
	"btn-codex-cancel",
	"llm-codex-progress",
	"llm-codex-manual",
	"llm-codex-code",
	"btn-codex-code-submit",
	"llm-pane-apikey",
	"llm-base-url",
	"llm-override-base_url",
	"llm-api-key",
	"btn-llm-clear-key",
	"llm-api-key-hint",
	"llm-override-api_key",
	"llm-model",
	"llm-override-model",
	"llm-alert",
	"llm-summary",
	"llm-settings-path",
	"btn-llm-reset",
	"btn-llm-cancel",
	"btn-llm-save",
}

// readWebUIAsset читает файл через embed.FS, а не с диска: проверять нужно
// именно то, что уезжает внутрь собранного бинаря.
func readWebUIAsset(t *testing.T, name string) string {
	t.Helper()
	data, err := webUIFiles.ReadFile(name)
	if err != nil {
		t.Fatalf("webUIFiles.ReadFile(%q): %v", name, err)
	}
	if len(data) == 0 {
		t.Fatalf("webUIFiles.ReadFile(%q): пустой файл", name)
	}
	return string(data)
}

// checkUIIDs проверяет, что каждый id встречается в разметке ровно раз и
// упоминается хотя бы в одном из скриптов.
func checkUIIDs(t *testing.T, ids []string, scripts ...string) {
	t.Helper()
	html := readWebUIAsset(t, "webui/index.html")
	js := ""
	for _, name := range scripts {
		js += readWebUIAsset(t, "webui/"+name)
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			t.Errorf("id %q перечислен дважды", id)
			continue
		}
		seen[id] = true
		attr := `id="` + id + `"`
		if n := strings.Count(html, attr); n != 1 {
			t.Errorf("webui/index.html: %s встречается %d раз, ожидается 1", attr, n)
		}
		if !uiIDReferenced(js, id) {
			t.Errorf("%v: id %q нигде не используется", scripts, id)
		}
	}
}

// uiIDReferenced reports whether the scripts reach an element. Polza and
// override slots exist once per panel, so the scripts build those ids from a
// prefix — `${pre}-polza-model`, `llm-override-${field}` — and what has to
// appear literally is the part after it. Titles are reached by aria.
func uiIDReferenced(js, id string) bool {
	if strings.Contains(js, id) || strings.HasSuffix(id, "-title") {
		return true
	}
	if _, suffix, ok := strings.Cut(id, "-polza-"); ok {
		return strings.Contains(js, "-polza-"+suffix)
	}
	if _, field, ok := strings.Cut(id, "-override-"); ok {
		return strings.Contains(js, "-override-") && strings.Contains(js, "'"+field+"'")
	}
	// A family of ids built from one template: `onboarding-llm-pane-${kind}`.
	if i := strings.LastIndex(id, "-"); i > 0 && strings.Contains(js, id[:i+1]+"${") {
		return strings.Contains(js, "'"+id[i+1:]+"'")
	}
	// A form field is read by its name: fd.get('login').
	if i := strings.LastIndex(id, "-"); i > 0 && strings.Contains(js, "fd.get('"+id[i+1:]+"')") {
		return true
	}
	return false
}

func TestLLMUIMarkupCarriesRequiredIDs(t *testing.T) {
	checkUIIDs(t, llmPanelUIIDs, "llm.js")
}

// Панель вызывает функции app.js (api, toast, loadAgentConfig), а app.js при
// загрузке — bindLLMSettings из llm.js; оба скрипта должны быть подключены.
func TestLLMUIScriptsAndStylesAreLinked(t *testing.T) {
	html := readWebUIAsset(t, "webui/index.html")
	for _, snippet := range []string{
		`<script src="llm.js" defer></script>`,
		`<link rel="stylesheet" href="onboarding.css" />`,
		`<div class="modal-overlay" id="llm-modal" hidden>`,
		`aria-label="Настройки LLM"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Errorf("webui/index.html: нет %q", snippet)
		}
	}
	if !strings.Contains(readWebUIAsset(t, "webui/app.js"), "bindLLMSettings()") {
		t.Error("webui/app.js: boot не вызывает bindLLMSettings()")
	}
	css := readWebUIAsset(t, "webui/onboarding.css")
	for _, selector := range []string{".modal-overlay[hidden]", ".llm-tab.is-active", "--modal-scrim", "--danger-text"} {
		if !strings.Contains(css, selector) {
			t.Errorf("webui/onboarding.css: нет %s", selector)
		}
	}
}
