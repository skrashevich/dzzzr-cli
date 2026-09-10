package main

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeUploadFilename(t *testing.T) {
	cases := map[string]string{
		"заметки.md":            "заметки.md",
		"../../.config/dzzzr":   "dzzzr",
		"/etc/passwd":           "passwd",
		"..":                    "file",
		".":                     "file",
		"   ":                   "file",
		"сценарий.pdf":          "сценарий.pdf",
		"a/b/c/сценарий (1).md": "сценарий (1).md",
	}
	for in, want := range cases {
		if got := sanitizeUploadFilename(in); got != want {
			t.Errorf("sanitizeUploadFilename(%q) = %q, ожидалось %q", in, got, want)
		}
	}
}

func TestWebUploadStoresFileUnderChat(t *testing.T) {
	hub := newTestHub(t, nil)
	srv := httptest.NewServer(hub.newMux())
	defer srv.Close()

	snap := hub.store.create("moscow", "approve")

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "../заметки.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, "коды уровня"); err != nil {
		t.Fatal(err)
	}
	_ = mw.Close()

	res, err := srv.Client().Post(srv.URL+"/api/v1/chats/"+snap.ID+"/files", mw.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusCreated {
		data, _ := io.ReadAll(res.Body)
		t.Fatalf("загрузка вернула %d: %s", res.StatusCode, data)
	}
	data, _ := io.ReadAll(res.Body)
	var info uploadedFileInfo
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("ответ не разобран (%s): %v", data, err)
	}
	if info.Name != "заметки.md" || info.Size != int64(len("коды уровня")) {
		t.Fatalf("неожиданное вложение: %+v", info)
	}

	root, err := chatUploadsRoot()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(info.Path, filepath.Join(root, snap.ID)+string(os.PathSeparator)) {
		t.Fatalf("файл сохранён вне каталога чата: %s", info.Path)
	}
	stored, err := os.ReadFile(info.Path)
	if err != nil || string(stored) != "коды уровня" {
		t.Fatalf("файл не сохранён: %q, %v", stored, err)
	}
}

func TestWebUploadRejectsUnknownChat(t *testing.T) {
	hub := newTestHub(t, nil)
	srv := httptest.NewServer(hub.newMux())
	defer srv.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("file", "x.md")
	_, _ = io.WriteString(part, "x")
	_ = mw.Close()

	res, err := srv.Client().Post(srv.URL+"/api/v1/chats/0123456789abcdef/files", mw.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("загрузка в несуществующий чат вернула %d", res.StatusCode)
	}
}

// The note is what makes the model open an attachment, so it must name the
// tool that can actually read the file.
func TestAppendUploadedFilesNoteNamesRealTools(t *testing.T) {
	note := appendUploadedFilesNote("посмотри", []uploadedFileRef{
		{Path: "/tmp/чат/заметки.md", Name: "заметки.md"},
		{Path: "/tmp/чат/сценарий.PDF"},
	})
	if !strings.HasPrefix(note, "посмотри\n\n[Прикреплённые файлы]") {
		t.Fatalf("текст пользователя потерян: %q", note)
	}
	if !strings.Contains(note, "заметки.md: /tmp/чат/заметки.md (прочитай через read_local_file)") {
		t.Errorf("для файла не назван read_local_file: %q", note)
	}
	if !strings.Contains(note, "сценарий.PDF: /tmp/чат/сценарий.PDF (прочитай через read_pdf)") {
		t.Errorf("для PDF не назван read_pdf: %q", note)
	}
	if got := appendUploadedFilesNote("только текст", nil); got != "только текст" {
		t.Errorf("без вложений сообщение изменено: %q", got)
	}
}
