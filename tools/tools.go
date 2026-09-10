//go:build tools

// Package tools pins the gomobile toolchain as a module dependency, so the
// bind scripts and CI build the bindings with the version this repository was
// tested against.
package tools

import (
	_ "golang.org/x/mobile/cmd/gobind"
	_ "golang.org/x/mobile/cmd/gomobile"
)
