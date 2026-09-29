package statsoffline

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/skrashevich/dzzzr-cli/gamestats"
)

func TestCalculatorMatchesSources(t *testing.T) {
	paths, err := filepath.Glob("../../gamestats/*")
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, "../../go.mod", "../../go.sum", "wasm/main.go")
	for i := range paths {
		paths[i] = filepath.ToSlash(paths[i])
	}
	slices.Sort(paths)
	h := sha256.New()
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		h.Write([]byte(filepath.ToSlash(path)))
		h.Write([]byte{0})
		h.Write(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))
	}
	stamp, err := os.ReadFile("sources.sha256")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(stamp)) != hex.EncodeToString(h.Sum(nil)) {
		t.Fatal("offline calculator is stale: run go generate ./internal/statsoffline")
	}
}

// Exercise the checked-in binary, not just the native implementation.
func TestWASMMatchesNative(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required to exercise WebAssembly")
	}
	name := "log.csv"
	data := []byte("Время;Действие;Команда;Уровень;Данные\n2026-09-26 22:00:00;выдан уровень;A;1.0;\n2026-09-26 22:01:00;завершена игра;;;\n")
	if file := os.Getenv("DZZZR_STATS_TEST_JSON"); file != "" {
		name = filepath.Base(file)
		data, err = os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, cfg := range []*gamestats.Config{nil, {Game: map[string]any{"defaultDur": "45"}, Teams: map[string]map[string]any{"A": {"penalty": "2"}, "Rising": {"penalty": "10"}}}} {
		want, err := gamestats.Build(name, data, cfg)
		if err != nil {
			t.Fatal(err)
		}
		cfgJSON, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		input, err := json.Marshal([]string{name, base64.StdEncoding.EncodeToString(data), string(cfgJSON), ""})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		cmd := exec.CommandContext(ctx, node, "-e", `
const fs=require('node:fs'),zlib=require('node:zlib');
globalThis.crypto ??= require('node:crypto').webcrypto;
require('./wasm_exec.js');
(async()=>{
 const go=new Go();
 const {instance}=await WebAssembly.instantiate(zlib.gunzipSync(fs.readFileSync('calculator.wasm.gz')),go.importObject);
 go.run(instance);
 const result=globalThis.dzzzrOfflineCalculate(...JSON.parse(fs.readFileSync(0,'utf8')));
 process.stdout.write(result,()=>process.exit(0));
})().catch(e=>{console.error(e);process.exit(1)});`)
		cmd.Stdin = bytes.NewReader(input)
		output, err := cmd.Output()
		cancel()
		if err != nil {
			t.Fatalf("WASM: %v", err)
		}
		wantJSON, err := json.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		var a, b any
		if err := json.Unmarshal(wantJSON, &a); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(output, &b); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a, b) {
			t.Fatal("WASM and native Go reports differ")
		}
	}
}
