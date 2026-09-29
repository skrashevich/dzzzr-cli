// Package statsoffline embeds the Go calculator for self-contained HTML reports.
package statsoffline

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"io"
)

// Regenerate after changing gamestats, the bridge, dependencies or Go version.
//go:generate go run ./generate

//go:embed calculator.wasm.gz
var calculator []byte

// Runtime is distributed under the Go BSD license (see GO-LICENSE).
//
//go:embed wasm_exec.js
var Runtime string

//go:embed GO-LICENSE
var License string

func WASM() ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(calculator))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}
