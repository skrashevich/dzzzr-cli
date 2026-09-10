package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// maxChatUploadBytes bounds one attached file. The agent only ever needs the
// text inside it, so this is headroom for a scanned PDF rather than a limit
// tuned to what a note weighs.
const maxChatUploadBytes = 20 << 20

// chatUploadsRoot is where files attached in the browser land. It is kept
// apart from DZZZR_FILES_ROOT so an attachment never depends on the directory
// «dzzzr web» happened to be started in.
func chatUploadsRoot() (string, error) {
	dir, err := sessionDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "web", "uploads"), nil
}

// sanitizeUploadFilename strips the directory part of a client-supplied name.
// The browser can send anything there, and the result becomes a path.
func sanitizeUploadFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." || name == ".." || name == string(os.PathSeparator) {
		return "file"
	}
	return name
}

// uploadedFileInfo is what the browser gets back for one stored attachment.
type uploadedFileInfo struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// uploadedFileRef is one attachment as the browser sends it back with the
// message it belongs to.
type uploadedFileRef struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

func (h *webHub) httpUploadChatFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := h.store.get(id); !ok {
		webError(w, http.StatusNotFound, "чат не найден")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxChatUploadBytes)
	if err := r.ParseMultipartForm(maxChatUploadBytes); err != nil {
		webError(w, http.StatusBadRequest, "файл слишком велик или форма повреждена")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		webError(w, http.StatusBadRequest, "нужно поле формы «file»")
		return
	}
	defer file.Close()

	root, err := chatUploadsRoot()
	if err != nil {
		webError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	// The identifier becomes a directory name, so it is checked rather than
	// trusted: a hand-made request must not name a path of its choosing.
	if !validChatID(id) {
		webError(w, http.StatusBadRequest, "недопустимый идентификатор чата")
		return
	}
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, sessionDirPerm); err != nil {
		webError(w, http.StatusInternalServerError, "не удалось создать каталог %s: %v", dir, err)
		return
	}

	name := sanitizeUploadFilename(header.Filename)
	dest := filepath.Join(dir, uploadPrefix()+"_"+name)

	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, sessionFilePerm)
	if err != nil {
		webError(w, http.StatusInternalServerError, "не удалось создать %s: %v", dest, err)
		return
	}
	defer out.Close()

	n, err := io.Copy(out, file)
	if err != nil {
		webError(w, http.StatusInternalServerError, "не удалось записать %s: %v", dest, err)
		return
	}

	webWriteJSON(w, http.StatusCreated, uploadedFileInfo{Path: dest, Name: name, Size: n})
}

// uploadPrefix keeps two attachments with the same filename in one chat from
// overwriting each other.
func uploadPrefix() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "upload"
	}
	return hex.EncodeToString(b[:])
}

// appendUploadedFilesNote adds the attached files to the user's message by
// path and by the tool that opens them, so the model reads them instead of
// describing a link it cannot follow.
func appendUploadedFilesNote(content string, files []uploadedFileRef) string {
	if len(files) == 0 {
		return content
	}
	var b strings.Builder
	b.WriteString(content)
	if content != "" {
		b.WriteString("\n\n")
	}
	b.WriteString("[Прикреплённые файлы]\n")
	for _, f := range files {
		name := f.Name
		if name == "" {
			name = filepath.Base(f.Path)
		}
		tool := "read_local_file"
		if strings.EqualFold(filepath.Ext(f.Path), ".pdf") {
			tool = "read_pdf"
		}
		fmt.Fprintf(&b, "- %s: %s (прочитай через %s)\n", name, f.Path, tool)
	}
	return b.String()
}
