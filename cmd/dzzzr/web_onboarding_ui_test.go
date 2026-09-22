package main

import (
	"strings"
	"testing"
)

// onboardingUIIDs — идентификаторы мастера первого запуска. Поведение живёт в
// webui/onboarding.js (и в общих потоках входа webui/llm.js), разметка — в
// webui/index.html.
var onboardingUIIDs = []string{
	// Корень, заголовок, индикатор шагов.
	"onboarding",
	"onboarding-title",
	"onboarding-subtitle",
	"onboarding-steps",

	// Шаги.
	"onboarding-pane-welcome",
	"onboarding-pane-llm",
	"onboarding-pane-auth",

	// Шаг «Модель».
	"onboarding-llm-tab-polza",
	"onboarding-llm-tab-codex",
	"onboarding-llm-tab-apikey",
	"onboarding-llm-pane-polza",
	"onboarding-llm-pane-codex",
	"onboarding-llm-pane-apikey",
	"onboarding-llm-polza-model",
	"onboarding-llm-polza-login",
	"onboarding-llm-polza-check",
	"onboarding-llm-polza-result",
	"onboarding-llm-polza-key",
	"onboarding-llm-polza-connect",
	"onboarding-codex-status",
	"btn-onboarding-codex-login",
	"onboarding-codex-progress",
	"onboarding-codex-manual",
	"onboarding-codex-code",
	"btn-onboarding-codex-code-submit",
	"onboarding-llm-base-url",
	"onboarding-llm-api-key",
	"onboarding-llm-model",
	"onboarding-llm-hint",
	"btn-onboarding-llm-test",
	"onboarding-llm-result",
	"onboarding-llm-overrides",

	// Шаг «Вход».
	"onboarding-auth-form",
	"onboarding-auth-role-player",
	"onboarding-auth-role-organizer",
	"onboarding-auth-city",
	"onboarding-auth-login",
	"onboarding-auth-password",
	"onboarding-auth-status",

	// Подвал и повторный запуск мастера.
	"onboarding-error",
	"btn-onboarding-back",
	"btn-onboarding-next",
	"btn-onboarding-open",
}

func TestOnboardingUIMarkupCarriesRequiredIDs(t *testing.T) {
	checkUIIDs(t, onboardingUIIDs, "onboarding.js", "llm.js")
}

func TestOnboardingUIMarkupWiring(t *testing.T) {
	html := readWebUIAsset(t, "webui/index.html")
	for _, c := range []struct{ what, snippet string }{
		{"оверлей мастера скрыт до запуска", `<div class="onboarding-overlay" id="onboarding" hidden>`},
		{"диалог помечен как модальный", `role="dialog" aria-modal="true" aria-labelledby="onboarding-title"`},
		{"ошибка мастера объявляется ассистивным технологиям", `id="onboarding-error" role="alert" hidden`},
		{"логин подсказывается менеджером паролей", `autocomplete="username"`},
		{"пароль подсказывается менеджером паролей", `autocomplete="current-password"`},
		{"кнопка повторного запуска подписана", `aria-label="Мастер настройки"`},
		{"роль передаётся полем role", `name="role" value="organizer"`},
		{"скрипт мастера подключён", `<script src="onboarding.js" defer></script>`},
		{"текст «Далее» в отдельном span", `id="btn-onboarding-next"><span id="onboarding-next-label">Далее</span>`},
	} {
		if !strings.Contains(html, c.snippet) {
			t.Errorf("webui/index.html: %s — не найдено %q", c.what, c.snippet)
		}
	}
	for _, step := range []string{"welcome", "llm", "auth"} {
		if !strings.Contains(html, `data-step="`+step+`"`) {
			t.Errorf("webui/index.html: нет шага data-step=%q", step)
		}
	}
	for _, id := range []string{"onboarding-llm-base-url", "onboarding-llm-api-key", "onboarding-llm-model", "onboarding-auth-login", "onboarding-auth-password"} {
		if !strings.Contains(html, `for="`+id+`"`) {
			t.Errorf("webui/index.html: у поля %q нет <label for=...>", id)
		}
	}
	// Мастер — не про en.cx: никаких доменов и чужого имени.
	for _, stale := range []string{"encli", "en.cx", "onboarding-auth-domain"} {
		if strings.Contains(html, stale) {
			t.Errorf("webui/index.html: остался %q из encli", stale)
		}
	}
	if !strings.Contains(readWebUIAsset(t, "webui/app.js"), "initOnboarding()") {
		t.Error("webui/app.js: boot не запускает мастер")
	}
}

func TestOnboardingUIIsStyled(t *testing.T) {
	css := readWebUIAsset(t, "webui/onboarding.css")
	for _, selector := range []string{
		".onboarding-overlay[hidden]",
		".onboarding-overlay:not([hidden])",
		".onboarding-pane[hidden]",
		".onboarding-step.is-active",
		".onboarding-step.is-done",
		"z-index: 160;",
		"@media (max-width: 480px)",
	} {
		if !strings.Contains(css, selector) {
			t.Errorf("webui/onboarding.css: нет %s", selector)
		}
	}
}
