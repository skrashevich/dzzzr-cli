package main

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
	"github.com/skrashevich/dzzzr-cli/gamesource"
)

func sourceFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.json")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAdminValidateSourceOffline(t *testing.T) {
	isolate(t)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); http.Error(w, "unexpected", 500) }))
	defer srv.Close()
	path := sourceFile(t, `{"game_id":4242,"mode":"create","levels":[{"params":{"title":"Первое"}}]}`)
	code, out, errOut := runCLI(t, "-base-url", srv.URL+"/moscow/", "-json", "admin-validate-source", path)
	if code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out, errOut)
	}
	var plan gamesource.Plan
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.GameID != 4242 || len(plan.Levels) != 1 {
		t.Fatalf("plan=%+v", plan)
	}
	if requests.Load() != 0 {
		t.Fatal("offline validation made requests")
	}
	if findCommand("admin-validate-source").Auth != authNone {
		t.Fatal("validator requires credentials")
	}
}

func TestAdminUploadSourceInvalidLaterRowWritesNothing(t *testing.T) {
	e, base := administering(t)
	path := sourceFile(t, `{"game_id":4242,"mode":"create","levels":[{"params":{"title":"First"}},{"params":{"title":"Second","codes":[{"code":"A","danger":"bad"}]}}]}`)
	code, _, _ := runCLI(t, with(base, "admin-upload-source", path)...)
	if code == 0 {
		t.Fatal("invalid plan accepted")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.posts) != 0 || len(e.gets) != 0 {
		t.Fatal("invalid plan touched engine")
	}
}

func TestAdminUploadSourceCreateOnlyRequestedRows(t *testing.T) {
	e, base := administering(t)
	path := sourceFile(t, `{"game_id":4242,"mode":"create","levels":[{"params":{"title":"First"}},{"params":{"title":"Second"}}]}`)
	code, out, errOut := runCLI(t, with(base, "admin-upload-source", path)...)
	if code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out, errOut)
	}
	var result gamesource.Result
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Completed) != 2 || result.Completed[0].ID != 777 {
		t.Fatalf("result=%+v", result)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.posts) != 2 {
		t.Fatalf("posts=%d, technical level must be explicit", len(e.posts))
	}
}

func TestAdminUploadSourceReportsPartialFailure(t *testing.T) {
	isolate(t)
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "unexpected read", 500)
			return
		}
		if posts.Add(1) == 1 {
			http.Redirect(w, r, "/moscow/admin/?action=zadanie&edit=1&id=777", http.StatusFound)
			return
		}
		http.Error(w, "failed", http.StatusInternalServerError)
	}))
	defer srv.Close()
	path := sourceFile(t, `{"game_id":4242,"mode":"create","levels":[{"params":{"title":"First"}},{"params":{"title":"Second"}},{"params":{"title":"Third"}}]}`)
	code, out, errOut := runCLI(t, "-base-url", srv.URL+"/moscow/", "-admin-login", "org", "-admin-password", "secret", "-json", "admin-upload-source", path)
	if code == 0 || errOut != "" {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	var result gamesource.Result
	decoder := jsontext.NewDecoder(strings.NewReader(out))
	progress, err := decoder.ReadValue()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(progress, &result); err != nil {
		t.Fatalf("%v output=%q", err, out)
	}
	if !strings.Contains(string(progress), `"error"`) {
		t.Fatalf("missing error in progress response: %s", progress)
	}
	if _, err := decoder.ReadValue(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected one JSON response, got %v", err)
	}
	if len(result.Completed) != 1 || result.FailedIndex == nil || *result.FailedIndex != 1 || posts.Load() != 2 {
		t.Fatalf("result=%+v posts=%d", result, posts.Load())
	}
}

func TestAdminCreateTechnicalLevelExplicit(t *testing.T) {
	e, base := administering(t)
	code, out, errOut := runCLI(t, with(base, "-json", "admin-create-technical-level", "4242")...)
	if code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out, errOut)
	}
	if !strings.Contains(out, `"id": 777`) {
		t.Fatalf("output=%q", out)
	}
	if !strings.Contains(fmt.Sprint(e.lastPost()), "where.games") {
		t.Fatalf("form=%v", e.lastPost())
	}
}

func TestAdminLocalFileLimits(t *testing.T) {
	path := sourceFile(t, "12345")
	if _, err := readAdminLocalFile(path, 4); err == nil {
		t.Fatal("oversize accepted")
	}
	if _, err := readAdminLocalFile(filepath.Dir(path), 100); err == nil {
		t.Fatal("directory accepted")
	}
	if data, err := readAdminLocalFile(path, 5); err != nil || string(data) != "12345" {
		t.Fatalf("data=%q err=%v", data, err)
	}
}

func TestAdminUploadFileCLI(t *testing.T) {
	for _, override := range []string{"", "renamed.png"} {
		t.Run(override, func(t *testing.T) {
			isolate(t)
			var posts atomic.Int32
			path := filepath.Join(t.TempDir(), "original.png")
			if err := os.WriteFile(path, []byte{0, 1, 255}, 0600); err != nil {
				t.Fatal(err)
			}
			wantName := filepath.Base(path)
			if override != "" {
				wantName = override
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					return
				}
				posts.Add(1)
				if err := r.ParseMultipartForm(1024); err != nil {
					t.Error(err)
					return
				}
				defer func() { _ = r.MultipartForm.RemoveAll() }()
				file, header, err := r.FormFile("file0")
				if err != nil {
					t.Error(err)
					return
				}
				defer func() { _ = file.Close() }()
				data, err := io.ReadAll(file)
				if err != nil || string(data) != string([]byte{0, 1, 255}) || header.Filename != wantName {
					t.Errorf("filename=%s data=%v err=%v", header.Filename, data, err)
				}
				_, _ = io.WriteString(w, `#message.upload_ok`)
			}))
			defer srv.Close()
			args := []string{"-base-url", srv.URL + "/moscow/", "-admin-login", "org", "-admin-password", "secret", "-json", "admin-upload-file", "4242", path}
			if override != "" {
				args = append(args, override)
			}
			code, out, errOut := runCLI(t, args...)
			if code != 0 {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, out, errOut)
			}
			var result dzzzr.AdminFileUpload
			if err := json.Unmarshal([]byte(out), &result); err != nil {
				t.Fatal(err)
			}
			if result.Name != wantName || !strings.Contains(result.URL, wantName) || posts.Load() != 1 {
				t.Fatalf("result=%+v posts=%d", result, posts.Load())
			}
		})
	}
}
