package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json/v2"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/gamesource"
)

// webDoRaw runs one request whose body is not JSON, or whose answer is not,
// and hands back the headers too. webDo covers everything else: it sends JSON
// and decodes JSON, which is every other request of the editor.
func webDoRaw(t *testing.T, srv *httptest.Server, method, path, contentType string, body []byte) (int, http.Header, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("%s %s: не прочитан ответ: %v", method, path, err)
	}
	return res.StatusCode, res.Header, data
}

// webDoMultipart posts one file plus whatever plain form fields go with it.
// webDoMultipart posts one file to the editor's upload route. Every caller
// uploads the same name into the fixture game — what the tests vary is the
// contents and the accompanying form fields — so both are fixed here rather
// than passed in.
func webDoMultipart(t *testing.T, srv *httptest.Server, data []byte, fields map[string]string) (int, []byte) {
	const path = "/api/v1/admin/games/4242/files"
	const filename = "note.txt"
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	code, _, answer := webDoRaw(t, srv, http.MethodPost, path, writer.FormDataContentType(), body.Bytes())
	return code, answer
}

// exportedScenario is the document GET .../scenario produced for game 4242,
// checked to be a file a browser can save as it stands.
func exportedScenario(t *testing.T) []byte {
	t.Helper()
	_, srv := editing(t, true)
	code, headers, data := webDoRaw(t, srv, http.MethodGet, "/api/v1/admin/games/4242/scenario", "", nil)
	if code != http.StatusOK {
		t.Fatalf("выгрузка сценария = %d: %s", code, data)
	}
	if got := headers.Get("Content-Disposition"); got != `attachment; filename="scenario-4242.json"` {
		t.Errorf("Content-Disposition = %q", got)
	}
	if got := headers.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	return data
}

func TestWebAdminExportScenario(t *testing.T) {
	data := exportedScenario(t)
	// What the browser saves has to be what admin-import-scenario reads.
	s, err := gamesource.DecodeScenario(data)
	if err != nil {
		t.Fatalf("выгруженный сценарий не разобран: %v\n%s", err, data)
	}
	// The fixture's game holds levels 901, 902 and 903, in that order.
	if len(s.Levels) != 3 {
		t.Fatalf("уровней = %d: %+v", len(s.Levels), s.Levels)
	}
	for i, want := range []int{901, 902, 903} {
		if s.Levels[i].SourceID != want {
			t.Errorf("levels[%d].source_id = %d, ожидался %d", i, s.Levels[i].SourceID, want)
		}
		if s.Levels[i].Key != "level_"+strconv.Itoa(want) {
			t.Errorf("levels[%d].key = %q", i, s.Levels[i].Key)
		}
	}
	if s.Source == nil || s.Source.GameID != 4242 {
		t.Fatalf("источник = %+v", s.Source)
	}
}

func TestWebAdminValidateScenarioAcceptsExport(t *testing.T) {
	data := exportedScenario(t)
	_, srv := editing(t, true)

	// The bare form: the saved file dropped straight into the request.
	code, _, raw := webDoRaw(t, srv, http.MethodPost, "/api/v1/admin/scenario/validate", "application/json", data)
	if code != http.StatusOK {
		t.Fatalf("проверка = %d: %s", code, raw)
	}
	var out adminScenarioCheck
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("ответ не разобран (%s): %v", raw, err)
	}
	if !out.OK || len(out.Errors) != 0 {
		t.Fatalf("корректный сценарий отклонён: %+v", out)
	}
	if out.Levels != 3 {
		t.Errorf("уровней = %d, ожидалось 3", out.Levels)
	}
}

func TestWebAdminValidateScenarioRejectsDuplicateLevelKey(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(exportedScenario(t), &doc); err != nil {
		t.Fatal(err)
	}
	levels, _ := doc["levels"].([]any)
	if len(levels) == 0 {
		t.Fatalf("в выгрузке нет уровней: %v", doc["levels"])
	}
	// The same level twice: two rows of the editor claiming one key.
	doc["levels"] = append(levels, levels[0])
	body, err := json.Marshal(map[string]any{"scenario": doc})
	if err != nil {
		t.Fatal(err)
	}

	_, srv := editing(t, true)
	code, _, raw := webDoRaw(t, srv, http.MethodPost, "/api/v1/admin/scenario/validate", "application/json", body)
	if code != http.StatusOK {
		t.Fatalf("проверка = %d: %s", code, raw)
	}
	var out adminScenarioCheck
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("ответ не разобран (%s): %v", raw, err)
	}
	if out.OK {
		t.Fatal("сценарий с повторяющимся ключом уровня принят")
	}
	if len(out.Errors) == 0 {
		t.Fatal("отказ без единой строки объяснения")
	}
	if !strings.Contains(strings.Join(out.Errors, "\n"), "duplicate level key") {
		t.Fatalf("в списке ошибок нет причины: %q", out.Errors)
	}
}

// Validation reads the document and nothing else, so it has to answer before
// an organizer has signed in: an author checks a file first and signs in
// later, and refusing here would only teach them to do it the other way round.
func TestWebAdminValidateScenarioNeedsNoCredentials(t *testing.T) {
	data := exportedScenario(t)
	_, srv := editing(t, false)

	code, _, raw := webDoRaw(t, srv, http.MethodPost, "/api/v1/admin/scenario/validate", "application/json", data)
	if code != http.StatusOK {
		t.Fatalf("проверка без организатора = %d: %s", code, raw)
	}
	var out adminScenarioCheck
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("ответ не разобран (%s): %v", raw, err)
	}
	if !out.OK {
		t.Fatalf("корректный сценарий отклонён без организатора: %+v", out)
	}
}

func TestWebAdminScenarioWritesRequireCredentials(t *testing.T) {
	_, srv := editing(t, false)

	var out struct {
		Error string `json:"error"`
	}
	if code := webDo(t, srv, http.MethodPost, "/api/v1/admin/scenario/import", `{"scenario":{},"game_id":0}`, &out); code != http.StatusForbidden {
		t.Fatalf("импорт без организатора = %d, ожидался 403", code)
	}
	if !strings.Contains(out.Error, "организатора") {
		t.Errorf("сообщение = %q, в нём нет подсказки про организатора", out.Error)
	}

	code, raw := webDoMultipart(t, srv, []byte("привет"), nil)
	if code != http.StatusForbidden {
		t.Fatalf("загрузка файла без организатора = %d, ожидался 403: %s", code, raw)
	}
	if !strings.Contains(string(raw), "организатора") {
		t.Errorf("сообщение = %q, в нём нет подсказки про организатора", raw)
	}
}

// fileManagerEngine answers the two paths dzzzr.AdminUploadFile touches: the
// admin page it takes its cookies from, and the TinyMCE file manager itself.
// The shared double serves neither, so the upload gets one of its own here
// rather than widening a harness every other test would then carry.
type fileManagerEngine struct {
	names []string
}

func newFileManagerEngine(t *testing.T, reply string) (*fileManagerEngine, *httptest.Server) {
	t.Helper()
	e := &fileManagerEngine{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("org:secret")) {
			w.Header().Set("WWW-Authenticate", `Basic realm="Admin area"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/moscow/admin/admin.php":
			http.SetCookie(w, &http.Cookie{Name: "PHPSESSID", Value: "s1", Path: "/"})
			w.Header().Set("Content-Type", "text/html; charset=windows-1251")
			_, _ = w.Write([]byte("<html><body>admin</body></html>"))
		case "/moscow/admin/tinymce/jscripts/tiny_mce/plugins/filemanager/stream/index.php":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("форма менеджера файлов не разобрана: %v", err)
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			e.names = append(e.names, r.FormValue("name0"))
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(reply))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return e, srv
}

// uploading is the pair an upload test starts from: the file manager and the
// editor served over it.
func uploading(t *testing.T, reply string) (*fileManagerEngine, *httptest.Server, string) {
	t.Helper()
	e, engine := newFileManagerEngine(t, reply)
	return e, adminWebServer(t, newAdminWebHub(t, engine.URL+"/moscow/", true)), engine.URL
}

// uploadOK is the file manager's own answer to an accepted upload.
const uploadOK = `parent.handleJSON({method:'upload',result:{"header":null,` +
	`"columns":["status","file","message"],` +
	`"data":[["OK","{0}/games/4242/note.txt","{#message.upload_ok}"]]},error:null,id:'m0'});`

func TestWebAdminUploadFile(t *testing.T) {
	double, srv, base := uploading(t, uploadOK)

	code, raw := webDoMultipart(t, srv, []byte("содержимое"), nil)
	if code != http.StatusCreated {
		t.Fatalf("загрузка = %d: %s", code, raw)
	}
	var out struct {
		Name          string `json:"name"`
		URL           string `json:"url"`
		AlreadyExists bool   `json:"already_exists"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("ответ не разобран (%s): %v", raw, err)
	}
	if out.Name != "note.txt" {
		t.Errorf("name = %q", out.Name)
	}
	if out.URL != base+"/uploaded/moscow/Night/games/4242/note.txt" {
		t.Errorf("url = %q", out.URL)
	}
	if out.AlreadyExists {
		t.Error("новый файл объявлен уже существующим")
	}
	// The file manager truncates at the final dot, so the form carries the
	// stem beside the full name.
	if len(double.names) != 1 || double.names[0] != "note" {
		t.Errorf("name0 = %v", double.names)
	}
}

// The name field renames the file in the game's directory, and a name the
// browser made up is as client-supplied as the uploaded one.
func TestWebAdminUploadFileHonoursName(t *testing.T) {
	_, srv, _ := uploading(t, uploadOK)

	code, raw := webDoMultipart(t, srv, []byte("x"), map[string]string{"name": "../map.png"})
	if code != http.StatusCreated {
		t.Fatalf("загрузка = %d: %s", code, raw)
	}
	var out struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("ответ не разобран (%s): %v", raw, err)
	}
	if out.Name != "map.png" {
		t.Fatalf("имя вышло за каталог игры: %q", out.Name)
	}
	if !strings.HasSuffix(out.URL, "/games/4242/map.png") {
		t.Errorf("url = %q", out.URL)
	}
}

// The file manager's own refusal is the engine's answer, so it comes back as
// a bad gateway carrying the engine's words rather than as an internal error.
func TestWebAdminUploadFileReportsEngineRefusal(t *testing.T) {
	refusal := `parent.handleJSON({method:'upload',result:{"header":null,` +
		`"columns":["status","file","message"],` +
		`"data":[["RW_ERROR","{0}/games/4242/note.txt","{#error.upload_failed}"]]},error:null,id:'m0'});`
	_, srv, _ := uploading(t, refusal)

	code, raw := webDoMultipart(t, srv, []byte("x"), nil)
	if code != http.StatusBadGateway {
		t.Fatalf("отказ движка = %d, ожидался 502: %s", code, raw)
	}
	for _, want := range []string{"RW_ERROR", "upload_failed"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("в сообщении нет %q: %s", want, raw)
		}
	}
}

func TestWebAdminHandoffCreatesChat(t *testing.T) {
	_, base := newAdminEngine(t)
	hub := newAdminWebHub(t, base, true)
	srv := adminWebServer(t, hub)

	var snap chatSnapshot
	body := `{"game_id":4242,"level_id":901,"note":"проверь коды"}`
	if code := webDo(t, srv, http.MethodPost, "/api/v1/admin/handoff", body, &snap); code != http.StatusCreated {
		t.Fatalf("передача = %d", code)
	}
	if snap.ID == "" || snap.City != "moscow" {
		t.Fatalf("чат = %+v", snap)
	}
	if len(snap.Lines) != 1 || snap.Lines[0].Role != chatRoleUser {
		t.Fatalf("строки чата = %+v", snap.Lines)
	}
	content := snap.Lines[0].Content
	for _, want := range []string{"4242", "901", "moscow", "проверь коды"} {
		if !strings.Contains(content, want) {
			t.Errorf("в сообщении нет %q:\n%s", want, content)
		}
	}
	// The names are read off the engine here, not sent by the browser.
	if !strings.Contains(content, "Гаражи") {
		t.Errorf("в сообщении нет названия задания:\n%s", content)
	}
	// Seeding the chat is the whole job: the author sends it themselves.
	if snap.Running {
		t.Error("передача запустила агента")
	}
	if _, ok := hub.store.get(snap.ID); !ok {
		t.Fatal("чат не сохранён в хранилище")
	}
}

// Without organizer credentials the names cannot be read, and the handoff
// still has to happen: the identifiers alone name the game unambiguously.
func TestWebAdminHandoffWithoutCredentials(t *testing.T) {
	_, base := newAdminEngine(t)
	srv := adminWebServer(t, newAdminWebHub(t, base, false))

	var snap chatSnapshot
	if code := webDo(t, srv, http.MethodPost, "/api/v1/admin/handoff", `{"game_id":4242}`, &snap); code != http.StatusCreated {
		t.Fatalf("передача без организатора = %d", code)
	}
	if len(snap.Lines) != 1 || !strings.Contains(snap.Lines[0].Content, "4242") {
		t.Fatalf("строки чата = %+v", snap.Lines)
	}
}

func TestWebAdminHandoffRejectsMissingGame(t *testing.T) {
	_, srv := editing(t, true)

	var out struct {
		Error string `json:"error"`
	}
	if code := webDo(t, srv, http.MethodPost, "/api/v1/admin/handoff", `{"note":"без игры"}`, &out); code != http.StatusBadRequest {
		t.Fatalf("status = %d, error = %q", code, out.Error)
	}
}
