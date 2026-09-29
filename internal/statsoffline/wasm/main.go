//go:build js && wasm

// The offline report runs the same Go package as the HTTP backend.
package main

import (
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"syscall/js"

	"github.com/skrashevich/dzzzr-cli/gamestats"
)

func calculate(_ js.Value, args []js.Value) any {
	result, err := report(args)
	if err != nil {
		out, _ := json.Marshal(map[string]string{"error": err.Error()})
		return string(out)
	}
	return string(result)
}

func report(args []js.Value) ([]byte, error) {
	if len(args) != 4 {
		return nil, fmt.Errorf("неверный запрос расчёта")
	}
	data, err := base64.StdEncoding.DecodeString(args[1].String())
	if err != nil {
		return nil, err
	}
	var cfg *gamestats.Config
	if err := json.Unmarshal([]byte(args[2].String()), &cfg); err != nil {
		return nil, err
	}
	r, err := gamestats.Build(args[0].String(), data, cfg)
	if err != nil {
		return nil, err
	}
	if args[3].String() == "html" {
		html, err := gamestats.HTMLSnapshot(r)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]string{"html": html})
	}
	if args[3].String() == "xlsx" {
		xlsx, err := gamestats.ExportXLSX(r)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string][]byte{"xlsx": xlsx})
	}
	return json.Marshal(r)
}

func main() {
	js.Global().Set("dzzzrOfflineCalculate", js.FuncOf(calculate))
	select {}
}
