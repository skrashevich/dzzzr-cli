package main

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/skrashevich/dzzzr-cli/gamesource"
)

func TestAdminScenarioExportAndOfflineValidate(t *testing.T) {
	_, base := administering(t)
	file := filepath.Join(t.TempDir(), "scenario.json")
	code, out, stderr := runCLI(t, with(base, "admin-export-scenario", "4242", file, "linked")...)
	if code != 0 {
		t.Fatalf("export: %d %s %s", code, out, stderr)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	s, err := gamesource.DecodeScenario(data)
	if err != nil {
		t.Fatal(err)
	}
	if s.Format != "dzzzr-scenario" || s.Source.GameID != 4242 {
		t.Fatalf("%+v", s.Source)
	}
	code, out, stderr = runCLI(t, "admin-validate-scenario", file)
	if code != 0 {
		t.Fatalf("validate: %d %s %s", code, out, stderr)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result["valid"] != true {
		t.Fatal(out)
	}
	if findCommand("admin-validate-scenario").Auth != authNone {
		t.Fatal("validation requires authentication")
	}
}
