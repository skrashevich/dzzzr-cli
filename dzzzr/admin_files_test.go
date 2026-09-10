package dzzzr

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdminUploadFileMultipart(t *testing.T) {
	for _, filename := range []string{"map.final.png", "карта 1.png", `map"quoted.png`} {
		t.Run(filename, func(t *testing.T) {
			data := []byte{0x89, 'P', 'N', 'G', 0, 0xff, 0xc0, 13, 10}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				login, password, ok := r.BasicAuth()
				if !ok || login != "organizer" || password != "secret" {
					t.Error("missing organizer Basic auth")
				}
				if r.Method == http.MethodGet {
					if r.URL.Path != "/moscow/admin/admin.php" {
						t.Errorf("GET path = %s", r.URL.Path)
					}
					http.SetCookie(w, &http.Cookie{Name: "session", Value: "upload-token", Path: "/moscow/admin/"})
					_, _ = io.WriteString(w, "admin page")
					return
				}
				if r.Method != http.MethodPost || r.URL.Path != "/moscow/"+adminFileUploadPath {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				cookie, err := r.Cookie("session")
				if err != nil || cookie.Value != "upload-token" {
					t.Errorf("cookie = %v, %v", cookie, err)
				}
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Error(err)
					return
				}
				defer func() { _ = r.MultipartForm.RemoveAll() }()
				name, _, _ := strings.CutLast(filename, ".")
				expected := map[string]string{"cmd": "fm.upload", "path": "{0}/games/42", "domain": "", "name0": name, "upload": "Загрузка"}
				if len(r.MultipartForm.Value) != len(expected) {
					t.Errorf("fields: %v", r.MultipartForm.Value)
				}
				for key, value := range expected {
					if got := r.MultipartForm.Value[key]; len(got) != 1 || got[0] != value {
						t.Errorf("field %s = %v; want %q", key, got, value)
					}
				}
				file, header, err := r.FormFile("file0")
				if err != nil {
					t.Error(err)
					return
				}
				defer func() { _ = file.Close() }()
				got, err := io.ReadAll(file)
				if err != nil || !bytes.Equal(got, data) {
					t.Errorf("binary changed: %x (%v)", got, err)
				}
				if header.Filename != filename || header.Header.Get("Content-Type") != "" {
					t.Errorf("file header: %v", header)
				}
				_, _ = io.WriteString(w, `{"result":"#message.upload_ok"}`)
			}))
			defer server.Close()
			client := New("moscow", WithBaseURL(server.URL+"/moscow/"), WithAdminCredentials("organizer", "secret"), WithAdminDelay(0))
			result, err := client.AdminUploadFile(t.Context(), 42, filename, data)
			if err != nil {
				t.Fatal(err)
			}
			expectedURL := server.URL + "/uploaded/moscow/Night/games/42/" + url.PathEscape(filename)
			if result.Name != filename || result.URL != expectedURL || result.AlreadyExists {
				t.Errorf("result = %+v, want URL %s", result, expectedURL)
			}
			if requests.Load() != 2 {
				t.Errorf("requests = %d", requests.Load())
			}
		})
	}
}

func TestAdminUploadFileResponses(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		getStatus, postStatus int
		body                  string
		wantErr, auth, exists bool
	}{
		{"exists", 200, 200, "#error.file_exists", false, false, true},
		{"unknown", 200, 200, "<html>login</html>", true, false, false},
		{"ambiguous", 200, 200, "#error.file_exists #message.upload_ok", true, false, false},
		{"get unauthorized", 401, 200, "#message.upload_ok", true, true, false},
		{"post forbidden", 200, 403, "#message.upload_ok", true, true, false},
		{"server error", 200, 500, "#message.upload_ok", true, false, false},
		{"redirect", 200, 302, "#message.upload_ok", true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var posts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(tc.getStatus)
					return
				}
				posts.Add(1)
				w.WriteHeader(tc.postStatus)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			client := New("moscow", WithBaseURL(server.URL+"/moscow/"), WithAdminCredentials("a", "b"), WithAdminDelay(0))
			result, err := client.AdminUploadFile(t.Context(), 42, "image.png", []byte("image"))
			if (err != nil) != tc.wantErr {
				t.Fatalf("result %v, error %v", result, err)
			}
			if tc.auth && AuthErrorKindOf(err) != AuthAdmin {
				t.Errorf("auth error: %v", err)
			}
			if !tc.wantErr && result.AlreadyExists != tc.exists {
				t.Errorf("result: %+v", result)
			}
			if tc.getStatus != 200 && posts.Load() != 0 {
				t.Error("uploaded despite failed cookie initialization")
			}
		})
	}
}

// TestAdminUploadRefusalIsReported covers the file manager's own refusal,
// measured against classic.dzzzr.ru on 2026-09-09. It answers HTTP 200 with a
// TinyMCE bridge page carrying a status row, which the client used to call an
// undecodable response — advising the user, on a file upload, not to resend a
// code.
func TestAdminUploadRefusalIsReported(t *testing.T) {
	const refusal = `<html><body><script type="text/javascript">parent.handleJSON({method:'upload',result:{"header":null,"columns":["status","file","message"],"data":[["RW_ERROR","{0}/games/1383/qa.png","{#error.upload_failed}"]]},error:null,id:'m0'});</script></body></html>`
	for _, tc := range []struct {
		name          string
		body          string
		wantRefusal   bool
		wantStatus    string
		wantInMessage string
	}{
		{"engine refusal", refusal, true, "RW_ERROR", "upload_failed"},
		{"no data key", "<html>login</html>", false, "", ""},
		{"short row", `{"data":[["RW_ERROR"]]}`, true, "RW_ERROR", ""},
		{"not rows", `{"data":{"status":"RW_ERROR"}}`, false, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					return
				}
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			client := New("moscow", WithBaseURL(server.URL+"/moscow/"), WithAdminCredentials("a", "b"), WithAdminDelay(0))
			_, err := client.AdminUploadFile(t.Context(), 1383, "qa.png", []byte("image"))
			if err == nil {
				t.Fatal("отказ принят за успех")
			}
			var refused *AdminFileError
			if !errors.As(err, &refused) {
				if tc.wantRefusal {
					t.Fatalf("err = %v, want *AdminFileError", err)
				}
				// Anything the status row cannot be read from stays what it
				// was: an undecodable reply, not an invented refusal.
				var undecodable *UndecodableResponseError
				if !errors.As(err, &undecodable) {
					t.Fatalf("err = %v, want an undecodable response", err)
				}
				return
			}
			if !tc.wantRefusal {
				t.Fatalf("выдумал отказ: %v", err)
			}
			if refused.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", refused.Status, tc.wantStatus)
			}
			if tc.wantInMessage != "" && !strings.Contains(refused.Message, tc.wantInMessage) {
				t.Errorf("message = %q, want it to mention %q", refused.Message, tc.wantInMessage)
			}
			if !strings.Contains(err.Error(), tc.wantStatus) {
				t.Errorf("текст ошибки не называет статус движка: %v", err)
			}
		})
	}
}

func TestAdminUploadFileValidationAndCancellation(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, "#message.upload_ok")
	}))
	defer server.Close()
	client := New("moscow", WithBaseURL(server.URL+"/moscow/"), WithAdminCredentials("a", "b"), WithAdminDelay(0))
	for _, filename := range []string{"", ".", "..", "../x.png", "a/b.png", `a\b.png`, "a\n.png", "a\x00.png", "a\x7f.png", "\xff"} {
		if _, err := client.AdminUploadFile(t.Context(), 42, filename, nil); err == nil {
			t.Errorf("accepted filename %q", filename)
		}
	}
	for _, id := range []int{0, -1} {
		if _, err := client.AdminUploadFile(t.Context(), id, "x.png", nil); err == nil {
			t.Errorf("accepted ID %d", id)
		}
	}
	if _, err := client.AdminUploadFile(t.Context(), 42, "x.png", make([]byte, MaxAdminFileBytes+1)); err == nil {
		t.Error("accepted oversized file")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.AdminUploadFile(ctx, 42, "x.png", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled upload: %v", err)
	}
	missing := New("moscow", WithBaseURL(server.URL+"/moscow/"))
	if _, err := missing.AdminUploadFile(t.Context(), 42, "x.png", nil); AuthErrorKindOf(err) != AuthAdmin {
		t.Errorf("missing credentials: %v", err)
	}
	if requests.Load() != 0 {
		t.Errorf("invalid uploads made %d requests", requests.Load())
	}
	client.SetAdminDelay(time.Hour)
	client.lastAdminPost = time.Now()
	ctx, stop := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer stop()
	if _, err := client.AdminUploadFile(ctx, 42, "x.png", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("pacing cancellation: %v", err)
	}
	if requests.Load() != 1 {
		t.Errorf("pacing should perform GET only; got %d requests", requests.Load())
	}
}
