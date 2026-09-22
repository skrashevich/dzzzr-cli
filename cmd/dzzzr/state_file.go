package main

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sipeed/picoclaw/pkg/fileutil"
)

// This file holds the load/save/delete layer shared by the small JSON state
// files dzzzr keeps under sessionDir(): the LLM settings, the ChatGPT sign-in
// and the onboarding state. They differ only in what they hold, so the rules
// that are easy to get wrong — where the file lives, what a missing file
// means, and how it is written — are stated once here.
//
// Normalisation and validation stay with each type: they are statements about
// the value, not about the file, and have to run before anything is written.

// stateFilePath resolves a state file's location: envVar wins when it is set —
// tests use it to stay out of the developer's real configuration — and the
// default is a per-feature subdirectory of sessionDir().
//
// The subdirectory is not stylistic. sessionDir() holds one <city>.json per
// city, and «dzzzr logout» and the web logout delete that file; state kept
// beside it under a name a city could have would be one unlucky -city away
// from being read as a session or removed with one.
func stateFilePath(envVar, subdir, name string) (string, error) {
	if envVar != "" {
		if path := strings.TrimSpace(os.Getenv(envVar)); path != "" {
			return path, nil
		}
	}
	dir, err := sessionDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, subdir, name), nil
}

// loadJSONState reads one state file; what is the user-facing noun the errors
// are phrased with («настройки LLM», «состояние мастера настройки»).
//
// A missing or whitespace-only file yields the zero value and no error: nothing
// configured yet is the normal state of a fresh machine.
//
// A file that does not parse IS an error, and callers propagate it. Treating
// unreadable content as "nothing configured" would silently run dzzzr on
// choices the user did not make, so the message names the path and says how
// to recover.
func loadJSONState[T any](path, what string) (T, error) {
	var zero T
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return zero, nil
		}
		return zero, fmt.Errorf("не удалось прочитать %s %s: %w", what, path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return zero, nil
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return zero, fmt.Errorf("не удалось разобрать %s %s: %w (удалите файл, чтобы начать заново)", what, path, err)
	}
	return v, nil
}

// saveJSONState writes one state file for the next process to read.
//
// These files hold API keys and tokens, so each is created 0600 inside a 0700
// directory and written through a temp file: os.WriteFile applies its mode only
// when creating, and a half-written file would make the state unreadable.
func saveJSONState[T any](path, what string, v T) error {
	data, err := json.Marshal(v, jsontext.Multiline(true))
	if err != nil {
		return fmt.Errorf("не удалось сохранить %s: %w", what, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("не удалось создать каталог %s: %w", filepath.Dir(path), err)
	}
	if err := fileutil.WriteFileAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("не удалось записать %s %s: %w", what, path, err)
	}
	return nil
}

// deleteJSONState removes a state file. An absent file is already the outcome
// the caller asked for, so it is not reported as a failure.
func deleteJSONState(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
