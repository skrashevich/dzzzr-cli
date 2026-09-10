package dzzzr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/textproto"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxAdminFileBytes limits a single organizer file upload to 32 MiB.
const MaxAdminFileBytes = 32 << 20

const adminFileUploadPath = "admin/tinymce/jscripts/tiny_mce/plugins/filemanager/stream/index.php"

// AdminFileUpload reports an explicit file-manager upload result.
type AdminFileUpload struct {
	Name          string `json:"name"`
	URL           string `json:"url"`
	AlreadyExists bool   `json:"already_exists"`
}

// AdminFileURL constructs the public URL for a file in a game's directory.
func (c *Client) AdminFileURL(gameID int, filename string) (string, error) {
	if gameID <= 0 {
		return "", fmt.Errorf("dzzzr: admin file: game ID must be positive")
	}
	if filename == "" || strings.TrimSpace(filename) == "" || filename == "." || filename == ".." || strings.ContainsAny(filename, "/\\") || !utf8.ValidString(filename) || strings.ContainsFunc(filename, unicode.IsControl) {
		return "", fmt.Errorf("dzzzr: admin file: filename must be a plain filename without path separators or control characters")
	}
	u := c.baseURL.Clone()
	u.Path = "/uploaded/" + c.city + "/Night/games/" + strconv.Itoa(gameID) + "/" + filename
	u.RawPath, u.RawQuery, u.Fragment, u.RawFragment = "", "", "", ""
	u.User = nil
	return u.String(), nil
}

// AdminUploadFile uploads binary data using the engine file manager. It first
// fetches the admin page to obtain the cookies required by the upload endpoint.
// name0 removes only the final extension, preserving multi-dot filenames
// (DzrSourceHelper truncates at the first dot). file0 retains the original name.
func (c *Client) AdminUploadFile(ctx context.Context, gameID int, filename string, data []byte) (*AdminFileUpload, error) {
	publicURL, err := c.AdminFileURL(gameID, filename)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxAdminFileBytes {
		return nil, fmt.Errorf("dzzzr: admin file: file exceeds %d bytes", MaxAdminFileBytes)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	options := requestOptions{admin: true, requireAuth: true}
	get, err := c.newRequest(ctx, http.MethodGet, "admin/admin.php", nil, nil, options)
	if err != nil {
		return nil, err
	}
	status, headers, _, err := c.do(get)
	if err != nil {
		return nil, err
	}
	if err := adminFileStatus(status); err != nil {
		return nil, err
	}
	// A local jar respects cookie scope and avoids mutating the shared client.
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	jar.SetCookies(get.URL, (&http.Response{Header: headers}).Cookies())

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	name, _, found := strings.CutLast(filename, ".")
	if !found {
		name = filename
	}
	fields := [][2]string{{"cmd", "fm.upload"}, {"path", "{0}/games/" + strconv.Itoa(gameID)}, {"domain", ""}, {"name0", name}, {"upload", "Загрузка"}}
	for _, field := range fields {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			return nil, err
		}
	}
	// Python requests' (filename, bytes, None) omits Content-Type on file0.
	part, err := writer.CreatePart(textproto.MIMEHeader{"Content-Disposition": {`form-data; name="file0"; filename="` + strings.ReplaceAll(filename, `"`, `\"`) + `"`}})
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	req, err := c.newRequest(ctx, http.MethodPost, adminFileUploadPath, nil, nil, options)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body.Bytes()))
	req.ContentLength = int64(body.Len())
	req.Header.Set("Content-Type", writer.FormDataContentType())
	for _, cookie := range jar.Cookies(req.URL) {
		req.AddCookie(cookie)
	}
	if err := c.paceAdmin(ctx); err != nil {
		return nil, err
	}
	status, _, response, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if err := adminFileStatus(status); err != nil {
		return nil, err
	}
	exists := bytes.Contains(response, []byte("#error.file_exists"))
	success := bytes.Contains(response, []byte("#message.upload_ok"))
	if exists == success {
		// Neither marker: the file manager either refused for some other
		// reason or answered with something else entirely. A refusal is
		// still structured, so report the engine's own words rather than
		// calling a perfectly readable answer undecodable.
		if st, msg := adminUploadResult(response); st != "" || msg != "" {
			return nil, &AdminFileError{Status: st, Message: msg}
		}
		return nil, &UndecodableResponseError{StatusCode: status, Context: "admin file upload", Err: stringError("missing or ambiguous file-manager result"), Body: truncate(response)}
	}
	return &AdminFileUpload{Name: filename, URL: publicURL, AlreadyExists: exists}, nil
}

// AdminFileError is the file manager's own refusal of an upload.
type AdminFileError struct {
	// Status is the engine's status token, e.g. "RW_ERROR".
	Status string
	// Message is its message key, e.g. "{#error.upload_failed}".
	Message string
}

// Error implements error. Either half may be missing; the engine sends both.
func (e *AdminFileError) Error() string {
	return "dzzzr: admin file upload refused: " + strings.TrimSpace(e.Status+" "+e.Message)
}

// adminUploadResult reads the status row out of the file manager's reply.
// The endpoint answers with a TinyMCE bridge page; measured against
// classic.dzzzr.ru on 2026-09-09 an upload this organizer may not perform
// comes back as
//
//	parent.handleJSON({method:'upload',result:{"header":null,
//	  "columns":["status","file","message"],
//	  "data":[["RW_ERROR","{0}/games/1383/qa.png","{#error.upload_failed}"]]},
//	  error:null,id:'m0'});
//
// so both the status token and the message key are there for the taking.
// Returns empty strings when the reply carries no such row.
func adminUploadResult(response []byte) (status, message string) {
	i := bytes.Index(response, []byte(`"data":`))
	if i < 0 {
		return "", ""
	}
	dec := json.NewDecoder(bytes.NewReader(response[i+len(`"data":`):]))
	var rows [][]string
	if err := dec.Decode(&rows); err != nil || len(rows) == 0 {
		return "", ""
	}
	row := rows[0]
	// The columns are status, file, message; a shorter row still yields what
	// it has rather than nothing.
	if len(row) > 0 {
		status = row[0]
	}
	if len(row) > 2 {
		message = row[2]
	}
	return status, message
}

func adminFileStatus(status int) error {
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return &AuthError{Kind: AuthAdmin, Message: "file manager authentication failed"}
	}
	if status != http.StatusOK {
		return &HTTPError{StatusCode: status, Context: "admin file upload"}
	}
	return nil
}
