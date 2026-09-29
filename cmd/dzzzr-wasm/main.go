//go:build js && wasm

package main

import (
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"sync"
	"syscall/js"
	"time"

	"github.com/skrashevich/dzzzr-cli/browserapp"
	"github.com/skrashevich/dzzzr-cli/gamestats"
)

var app browserapp.App
var cancelMu sync.Mutex
var chatCancel context.CancelFunc

func main() {
	js.Global().Set("dzzzrBrowserCall", js.FuncOf(func(_ js.Value, args []js.Value) any {
		raw := args[0].String()
		executor := js.FuncOf(func(_ js.Value, p []js.Value) any {
			resolve := p[0]
			go func() {
				value, err := dispatch(raw)
				out := map[string]any{"result": value}
				if err != nil {
					out = map[string]any{"error": err.Error()}
				}
				b, e := json.Marshal(out)
				if e != nil {
					b = []byte(`{"error":"не удалось сериализовать ответ"}`)
				}
				resolve.Invoke(string(b))
			}()
			return nil
		})
		promise := js.Global().Get("Promise").New(executor)
		executor.Release()
		return promise
	}))
	js.Global().Set("dzzzrBrowserCancel", js.FuncOf(func(_ js.Value, _ []js.Value) any {
		cancelMu.Lock()
		defer cancelMu.Unlock()
		if chatCancel != nil {
			chatCancel()
		}
		return nil
	}))
	// The statistics view shares the same rendering-only JS with offline exports.
	js.Global().Set("dzzzrOfflineCalculate", js.FuncOf(func(_ js.Value, args []js.Value) any {
		var out any
		data, err := base64.StdEncoding.DecodeString(args[1].String())
		var cfg *gamestats.Config
		if err == nil {
			err = json.Unmarshal([]byte(args[2].String()), &cfg)
		}
		if err == nil {
			var r *gamestats.Report
			r, err = gamestats.Build(args[0].String(), data, cfg)
			if err == nil {
				switch args[3].String() {
				case "xlsx":
					var b []byte
					b, err = gamestats.ExportXLSX(r)
					out = map[string]any{"xlsx": b}
				case "html":
					var h string
					h, err = gamestats.HTMLSnapshot(r)
					out = map[string]any{"html": h}
				default:
					out = r
				}
			}
		}
		if err != nil {
			out = map[string]string{"error": err.Error()}
		}
		b, _ := json.Marshal(out)
		return string(b)
	}))
	js.Global().Set("dzzzrBrowserReady", true)
	select {}
}
func dispatch(raw string) (value any, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("операция не выполнена: %v", p)
		}
	}()
	var r struct {
		browserapp.Request
		LLM      browserapp.ChatConfig    `json:"llm"`
		Messages []browserapp.ChatMessage `json:"messages"`
	}
	if err = json.Unmarshal([]byte(raw), &r); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if r.Action == "chat" {
		cancelMu.Lock()
		if chatCancel != nil {
			cancelMu.Unlock()
			return nil, fmt.Errorf("агент уже работает")
		}
		chatCancel = cancel
		cancelMu.Unlock()
		defer func() { cancelMu.Lock(); chatCancel = nil; cancelMu.Unlock() }()
		return app.Chat(ctx, r.LLM, r.Messages, func(s string) {
			if fn := js.Global().Get("dzzzrBrowserStatus"); fn.Type() == js.TypeFunction {
				fn.Invoke(s)
			}
		})
	}
	return app.Dispatch(ctx, r.Request)
}
