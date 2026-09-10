package main

import (
	"strings"
	"testing"
	"time"
)

func TestModelRequestTimeoutConfig(t *testing.T) {
	for _, provider := range []string{"openai", "codex"} {
		t.Run(provider, func(t *testing.T) {
			isolate(t)
			t.Setenv("DZZZR_LLM_PROVIDER", provider)
			t.Setenv("DZZZR_LLM_BASE_URL", "http://127.0.0.1:8080/v1")
			if provider == "codex" {
				t.Setenv("DZZZR_CODEX_AUTH_FILE", codexAuthFixture(t, "test-token", "test-account"))
			}
			t.Setenv("DZZZR_LLM_REQUEST_TIMEOUT_SECONDS", "120")
			cfg, err := llmConfig()
			if err != nil || cfg.RequestTimeout != 2*time.Minute {
				t.Fatalf("timeout=%s err=%v", cfg.RequestTimeout, err)
			}
			for _, raw := range []string{"0", "-1", "wrong", "3601"} {
				t.Setenv("DZZZR_LLM_REQUEST_TIMEOUT_SECONDS", raw)
				if _, err := llmConfig(); err == nil || !strings.Contains(err.Error(), "DZZZR_LLM_REQUEST_TIMEOUT_SECONDS") {
					t.Fatalf("invalid timeout %s: %v", raw, err)
				}
			}
		})
	}
}

func TestSourceContextBudgetConfig(t *testing.T) {
	for _, provider := range []string{"openai", "codex"} {
		t.Run(provider, func(t *testing.T) {
			isolate(t)
			t.Setenv("DZZZR_LLM_PROVIDER", provider)
			t.Setenv("DZZZR_LLM_BASE_URL", "http://127.0.0.1:8080/v1")
			if provider == "codex" {
				t.Setenv("DZZZR_CODEX_AUTH_FILE", codexAuthFixture(t, "test-token", "test-account"))
			}
			for _, raw := range []string{"1", "1048576", "16777216"} {
				t.Setenv("DZZZR_LLM_SOURCE_CONTEXT_BYTES", raw)
				cfg, err := llmConfig()
				if err != nil || cfg.SourceContextBytes < 1 {
					t.Fatalf("valid budget %s: %+v %v", raw, cfg, err)
				}
			}
			for _, raw := range []string{"0", "-1", "wrong", "16777217"} {
				t.Setenv("DZZZR_LLM_SOURCE_CONTEXT_BYTES", raw)
				if _, err := llmConfig(); err == nil || !strings.Contains(err.Error(), "DZZZR_LLM_SOURCE_CONTEXT_BYTES") {
					t.Fatalf("invalid budget %s: %v", raw, err)
				}
			}
		})
	}
}
