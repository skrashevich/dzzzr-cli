// build-browser assembles only explicitly listed public assets; no local logs,
// credentials or scenario files are copied into the Pages artifact.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/skrashevich/dzzzr-cli/internal/statsoffline"
)

func main() {
	out := flag.String("out", "dist/browser", "output directory")
	flag.Parse()
	if err := build(*out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func build(out string) error {
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	for _, name := range []string{"stats.js", "stats.css", "stats-offline.js"} {
		if err := copyFile("cmd/dzzzr/webui/"+name, filepath.Join(out, name)); err != nil {
			return err
		}
	}
	for _, name := range []string{"browser.js", "browser.css"} {
		if err := copyFile("docs/browser/"+name, filepath.Join(out, name)); err != nil {
			return err
		}
	}
	if err := copyFile("internal/statsoffline/wasm_exec.js", filepath.Join(out, "wasm_exec.js")); err != nil {
		return err
	}
	if err := copyFile("internal/statsoffline/GO-LICENSE", filepath.Join(out, "GO-LICENSE")); err != nil {
		return err
	}
	offline, err := statsoffline.WASM()
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(out, "offline.wasm"), offline, 0644); err != nil {
		return err
	}
	template, err := os.ReadFile("cmd/dzzzr/webui/stats.html")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(out, "stats-template.html"), template, 0644); err != nil {
		return err
	}
	shell, err := os.ReadFile("docs/browser/shell.html")
	if err != nil {
		return err
	}
	html := strings.Replace(string(template), `<script src="stats.js" defer></script>`, `<script src="wasm_exec.js" defer></script><script src="browser.js" defer></script><script src="stats.js" defer></script><link rel="stylesheet" href="browser.css">`, 1)
	html = strings.Replace(html, `<body class="stats-app">`, `<body class="stats-app browser-page">`+string(shell), 1)
	html = strings.Replace(html, "<title>Статистика DozoR</title>", "<title>dzzzr — инструменты игры в браузере</title>", 1)
	if err = os.WriteFile(filepath.Join(out, "index.html"), []byte(html), 0644); err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "-p", "1", "-trimpath", "-ldflags=-s -w", "-o", filepath.Join(out, "browser.wasm"), "./cmd/dzzzr-wasm")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err = cmd.Run(); err != nil {
		return err
	}
	// Collect dependency licenses from the actual WASM dependency graph.
	deps := exec.Command("go", "list", "-deps", "-f", `{{if .Module}}{{.Module.Dir}}{{end}}`, "./cmd/dzzzr-wasm")
	deps.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	raw, err := deps.Output()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var licenses strings.Builder
	for dir := range strings.SplitSeq(string(raw), "\n") {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		for _, n := range []string{"LICENSE", "LICENSE.md", "LICENSE.txt", "COPYING"} {
			b, e := os.ReadFile(filepath.Join(dir, n))
			if e == nil {
				fmt.Fprintf(&licenses, "\n--- %s ---\n%s\n", filepath.Base(dir), b)
				break
			}
		}
	}
	if err = os.WriteFile(filepath.Join(out, "THIRD_PARTY_LICENSES.txt"), []byte(licenses.String()), 0644); err != nil {
		return err
	}
	files := []string{"index.html", "browser.js", "browser.css", "stats.js", "stats.css", "stats-offline.js", "stats-template.html", "wasm_exec.js", "GO-LICENSE", "offline.wasm", "browser.wasm", "THIRD_PARTY_LICENSES.txt"}
	hash := sha256.New()
	for _, f := range files {
		b, e := os.ReadFile(filepath.Join(out, f))
		if e != nil {
			return e
		}
		hash.Write(b)
	}
	version := hex.EncodeToString(hash.Sum(nil))[:16]
	list, err := json.Marshal(files)
	if err != nil {
		return err
	}
	sw := `const CACHE='dzzzr-browser-` + version + `';const FILES=` + string(list) + `;self.addEventListener('install',e=>e.waitUntil(caches.open(CACHE).then(c=>c.addAll(FILES)).then(()=>self.skipWaiting())));self.addEventListener('activate',e=>e.waitUntil(caches.keys().then(keys=>Promise.all(keys.filter(k=>k.startsWith('dzzzr-browser-')&&k!==CACHE).map(k=>caches.delete(k)))).then(()=>self.clients.claim())));self.addEventListener('fetch',e=>{if(e.request.method!=='GET'||new URL(e.request.url).origin!==location.origin)return;e.respondWith(fetch(e.request).catch(()=>caches.match(e.request).then(r=>r||(e.request.mode==='navigate'?caches.match('index.html'):Response.error()))))});`
	return os.WriteFile(filepath.Join(out, "sw.js"), []byte(sw), 0644)
}
func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0644)
}
