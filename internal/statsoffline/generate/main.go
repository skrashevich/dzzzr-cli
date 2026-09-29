// Generates the embedded calculator using the current Go toolchain.
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

func main() {
	if err := generate(); err != nil {
		panic(err)
	}
}

func generate() error {
	dir, err := os.MkdirTemp("", "dzzzr-stats-wasm-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "calculator.wasm")
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", out, "./wasm")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	wasm, err := os.ReadFile(out)
	if err != nil {
		return err
	}
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	if _, err := z.Write(wasm); err != nil {
		return err
	}
	if err := z.Close(); err != nil {
		return err
	}
	if err := os.WriteFile("calculator.wasm.gz", b.Bytes(), 0644); err != nil {
		return err
	}
	for src, dst := range map[string]string{"lib/wasm/wasm_exec.js": "wasm_exec.js", "LICENSE": "GO-LICENSE"} {
		data, err := os.ReadFile(filepath.Join(runtime.GOROOT(), src))
		if os.IsNotExist(err) && src == "LICENSE" {
			data, err = os.ReadFile(filepath.Join(runtime.GOROOT(), "..", src))
		}
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0644); err != nil {
			return err
		}
	}
	// Fingerprint inputs to make stale checked-in binaries fail tests.
	paths, err := filepath.Glob("../../gamestats/*")
	if err != nil {
		return err
	}
	paths = append(paths, "../../go.mod", "../../go.sum", "wasm/main.go")
	slices.Sort(paths)
	h := sha256.New()
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		h.Write([]byte(filepath.ToSlash(path)))
		h.Write([]byte{0})
		h.Write(data)
	}
	return os.WriteFile("sources.sha256", []byte(hex.EncodeToString(h.Sum(nil))+"\n"), 0644)
}
