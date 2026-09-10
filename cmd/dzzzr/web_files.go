package main

import (
	"encoding/json/v2"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/skrashevich/dzzzr-cli/agentfiles"
)

// generatedFileTools names the tools that create a new file inside the local
// files root. Only their results are scanned for a path to offer for download:
// a tool that merely reads a file has not produced anything.
var generatedFileTools = map[string]bool{
	"save_local_json":       true,
	"assemble_local_json":   true,
	"admin_export_scenario": true,
	"extract_pdf":           true,
	"index_pdf":             true,
}

// generatedFileResultKeys are the result fields those tools use to report where
// they wrote.
var generatedFileResultKeys = []string{"path", "output_path", "file"}

// filesRoots returns every directory a tool might legitimately have written to.
// The two tool families disagree on how the root is resolved — agentfiles
// follows symlinks, agenttools does not, and either falls back to the working
// directory — so a downloadable file is looked for under all of them.
func filesRoots() []string {
	var roots []string
	add := func(p string) {
		if p == "" {
			return
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return
		}
		for _, seen := range roots {
			if seen == abs {
				return
			}
		}
		roots = append(roots, abs)
	}

	if env := strings.TrimSpace(os.Getenv(agentfiles.RootEnv)); env != "" {
		add(env)
		if resolved, err := filepath.EvalSymlinks(env); err == nil {
			add(resolved)
		}
	}
	if root, err := agentfiles.RootFromEnv(); err == nil {
		add(root)
	}
	if wd, err := os.Getwd(); err == nil {
		add(wd)
		if resolved, err := filepath.EvalSymlinks(wd); err == nil {
			add(resolved)
		}
	}
	return roots
}

// resolveGeneratedFile turns a path from a tool result into an absolute path to
// an existing regular file confined to one of roots. A relative path is tried
// under each root; the returned path is symlink-resolved so a later open is
// stable.
func resolveGeneratedFile(roots []string, p string) (string, bool) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", false
	}
	clean := filepath.Clean(p)

	var candidates []string
	if filepath.IsAbs(clean) {
		candidates = append(candidates, clean)
	} else {
		for _, root := range roots {
			candidates = append(candidates, filepath.Join(root, clean))
		}
	}

	for _, cand := range candidates {
		info, err := os.Stat(cand)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		real := cand
		if resolved, err := filepath.EvalSymlinks(cand); err == nil {
			real = resolved
		}
		if containedInAny(roots, cand) || containedInAny(roots, real) {
			return real, true
		}
	}
	return "", false
}

// containedInAny reports whether path is one of roots or sits under it.
func containedInAny(roots []string, path string) bool {
	for _, root := range roots {
		if path == root || strings.HasPrefix(path, root+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

// harvestGeneratedFiles records any file a finished tool call created, so the
// browser can download what the agent saved during the session. A result that
// does not name an existing regular file inside a files root is ignored.
func (h *webHub) harvestGeneratedFiles(chatID, toolName, result string) {
	if !generatedFileTools[toolName] {
		return
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		h.cfg.debugf("файлы сессии: результат %s не разобран как JSON: %v", toolName, err)
		return
	}
	roots := filesRoots()
	for _, key := range generatedFileResultKeys {
		raw, ok := payload[key].(string)
		if !ok {
			continue
		}
		abs, ok := resolveGeneratedFile(roots, raw)
		if !ok {
			h.cfg.debugf("файлы сессии: %s вернул %s=%q, файл не найден в корнях %v", toolName, key, raw, roots)
			continue
		}
		info, err := os.Stat(abs)
		if err != nil {
			continue
		}
		stored, added := h.store.recordFile(chatID, chatFile{
			Name:      filepath.Base(abs),
			Path:      abs,
			Size:      info.Size(),
			Tool:      toolName,
			CreatedAt: time.Now().UTC(),
		})
		if added {
			h.cfg.debugf("файлы сессии: зарегистрирован %s (%s)", stored.Name, abs)
			_ = h.store.persist(chatID)
			h.publishSSE(chatID, "file", stored)
		}
	}
}

// httpDownloadChatFile serves one file the agent generated in this chat. Only
// files already recorded for the chat are reachable, and only by the base name
// under which they were recorded: the request never builds a path of its own.
func (h *webHub) httpDownloadChatFile(w http.ResponseWriter, r *http.Request) {
	snap, ok := h.store.get(r.PathValue("id"))
	if !ok {
		webError(w, http.StatusNotFound, "чат не найден")
		return
	}
	name := r.PathValue("name")
	var target chatFile
	for _, f := range snap.Files {
		if f.Name == name {
			target = f
			break
		}
	}
	if target.Path == "" {
		webError(w, http.StatusNotFound, "файл не найден среди сгенерированных в этом чате")
		return
	}
	file, err := os.Open(target.Path)
	if err != nil {
		webError(w, http.StatusGone, "файл больше недоступен: %v", err)
		return
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		webError(w, http.StatusGone, "файл больше недоступен")
		return
	}

	ctype := mime.TypeByExtension(filepath.Ext(target.Name))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(target.Name))
	http.ServeContent(w, r, target.Name, info.ModTime(), file)
}
