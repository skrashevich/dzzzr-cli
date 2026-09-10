package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/agentfiles"
)

func TestResolveGeneratedFileContainment(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "sub")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dump.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inside, "part.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	roots := []string{root}

	if got, ok := resolveGeneratedFile(roots, "dump.json"); !ok || filepath.Base(got) != "dump.json" {
		t.Fatalf("относительный путь внутри корня: got %q ok=%v", got, ok)
	}
	if got, ok := resolveGeneratedFile(roots, filepath.Join(inside, "part.json")); !ok || filepath.Base(got) != "part.json" {
		t.Fatalf("абсолютный путь внутри корня: got %q ok=%v", got, ok)
	}
	for _, bad := range []string{"", "  ", "../escape.json", "missing.json", "/etc/passwd"} {
		if got, ok := resolveGeneratedFile(roots, bad); ok {
			t.Fatalf("путь %q должен быть отклонён, вернулось %q", bad, got)
		}
	}
}

// TestHarvestFindsFileAcrossRootResolutions covers the real mismatch: the
// writer tool resolves DZZZR_FILES_ROOT without following symlinks, while
// agentfiles.RootFromEnv does follow them, so the two disagree on the root
// string even though they point at the same directory.
func TestHarvestFindsFileAcrossRootResolutions(t *testing.T) {
	hub := newTestHub(t, nil)

	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("символические ссылки недоступны: %v", err)
	}
	t.Setenv(agentfiles.RootEnv, link)

	const name = "1383_scenario_dump_v2.json"
	const content = `{"levels":51}`
	if err := os.WriteFile(filepath.Join(real, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(hub.newMux())
	defer srv.Close()
	snap := hub.store.create("moscow", "approve")

	// admin_export_scenario reports the path relative to the root.
	hub.harvestGeneratedFiles(snap.ID, "admin_export_scenario", `{"path":"`+name+`","levels":51}`)
	// A re-report across turns does not duplicate it.
	hub.harvestGeneratedFiles(snap.ID, "admin_export_scenario", `{"path":"`+name+`","levels":51}`)
	// A read-only tool naming the same file contributes nothing.
	hub.harvestGeneratedFiles(snap.ID, "read_local_file", `{"path":"`+name+`"}`)

	got, ok := hub.store.get(snap.ID)
	if !ok {
		t.Fatal("чат пропал")
	}
	if len(got.Files) != 1 {
		t.Fatalf("ожидался ровно один файл, получено %d: %+v", len(got.Files), got.Files)
	}
	if got.Files[0].Name != name || got.Files[0].Tool != "admin_export_scenario" || got.Files[0].Size != int64(len(content)) {
		t.Fatalf("неожиданная запись о файле: %+v", got.Files[0])
	}

	res, err := srv.Client().Get(srv.URL + "/api/v1/chats/" + snap.ID + "/files/" + name)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("скачивание вернуло %d: %s", res.StatusCode, body)
	}
	if string(body) != content {
		t.Fatalf("тело файла = %q, ожидалось %q", body, content)
	}
	if cd := res.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") {
		t.Fatalf("Content-Disposition = %q, ожидалось вложение", cd)
	}

	// A name that was never recorded for this chat is not reachable.
	res, err = srv.Client().Get(srv.URL + "/api/v1/chats/" + snap.ID + "/files/other.json")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("незаписанный файл вернул %d, ожидался 404", res.StatusCode)
	}
}

// TestHarvestFindsFileInWorkingDir covers DZZZR_FILES_ROOT being unset, where
// the tools write into the process working directory.
func TestHarvestFindsFileInWorkingDir(t *testing.T) {
	hub := newTestHub(t, nil)
	t.Setenv(agentfiles.RootEnv, "")

	wd := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(wd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	const name = "part-001.json"
	if err := os.WriteFile(filepath.Join(wd, name), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}

	snap := hub.store.create("moscow", "approve")
	hub.harvestGeneratedFiles(snap.ID, "save_local_json", `{"path":"`+name+`","saved":true}`)

	got, _ := hub.store.get(snap.ID)
	if len(got.Files) != 1 || got.Files[0].Name != name {
		t.Fatalf("файл из рабочего каталога не подхвачен: %+v", got.Files)
	}
}
