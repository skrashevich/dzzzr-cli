package main

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"strings"

	"github.com/skrashevich/dzzzr-cli/gamestats"
	"github.com/skrashevich/dzzzr-cli/internal/statsoffline"
)

// Export a single file: no CDN, fetches or local server are needed on opening.
func exportStatsHTML(name string, data []byte, cfg gamestats.Config) ([]byte, error) {
	report, err := gamestats.Build(name, data, &cfg)
	if err != nil {
		return nil, err
	}
	snapshot, err := gamestats.HTMLSnapshot(report)
	if err != nil {
		return nil, err
	}
	wasm, err := statsoffline.WASM()
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(struct {
		Name   string           `json:"name"`
		Data   []byte           `json:"data"`
		Config gamestats.Config `json:"cfg"`
	}{name, data, cfg}, jsontext.EscapeForHTML(true))
	if err != nil {
		return nil, err
	}
	page, err := webUIFiles.ReadFile("webui/stats.html")
	if err != nil {
		return nil, err
	}
	css, err := webUIFiles.ReadFile("webui/stats.css")
	if err != nil {
		return nil, err
	}
	js, err := webUIFiles.ReadFile("webui/stats.js")
	if err != nil {
		return nil, err
	}
	offline, err := webUIFiles.ReadFile("webui/stats-offline.js")
	if err != nil {
		return nil, err
	}
	scripts := `<script type="application/json" id="stats-offline-data">` + string(payload) + `</script>` +
		`<script type="application/octet-stream" id="stats-offline-wasm">` + base64.StdEncoding.EncodeToString(wasm) + `</script>` +
		`<script>` + "/*\n" + statsoffline.License + "*/\n" + statsoffline.Runtime + `</script><script>` + string(offline) + `</script><script>` + string(js) + `</script>`
	html := strings.Replace(string(page), `<script src="stats.js" defer></script>`, scripts, 1)
	html = strings.Replace(html, `<link rel="stylesheet" href="stats.css">`, "<style>"+string(css)+"</style>", 1)
	html = strings.Replace(html, `<body class="stats-app">`, `<body class="stats-app stats-offline">`+snapshot, 1)
	return []byte(html), nil
}
