package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testState struct {
	Name string `json:"name"`
}

func TestStateFilePathDefaultsUnderConfigDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DZZZR_CONFIG_DIR", dir)
	t.Setenv("DZZZR_TEST_STATE_FILE", "")
	got, err := stateFilePath("DZZZR_TEST_STATE_FILE", "sub", "x.json")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "sub", "x.json"); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	t.Setenv("DZZZR_TEST_STATE_FILE", "/elsewhere/y.json")
	if got, _ := stateFilePath("DZZZR_TEST_STATE_FILE", "sub", "x.json"); got != "/elsewhere/y.json" {
		t.Fatalf("override ignored: %q", got)
	}
}

func TestJSONStateRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "s.json")
	if v, err := loadJSONState[testState](path, "test state"); err != nil || v.Name != "" {
		t.Fatalf("missing file: %+v %v", v, err)
	}
	if err := saveJSONState(path, "test state", testState{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
	v, err := loadJSONState[testState](path, "test state")
	if err != nil || v.Name != "x" {
		t.Fatalf("round trip: %+v %v", v, err)
	}
	if err := deleteJSONState(path); err != nil {
		t.Fatal(err)
	}
	if err := deleteJSONState(path); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}

func TestJSONStateBrokenFileNamesThePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(path, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadJSONState[testState](path, "test state")
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("err = %v", err)
	}
	if err := os.WriteFile(path, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadJSONState[testState](path, "test state"); err != nil {
		t.Fatalf("blank file: %v", err)
	}
}
