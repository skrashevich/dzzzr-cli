package main

import (
	"os"
	"strings"
)

// guiDetector is an indirection so the auto-start decision can be tested off
// Windows, where the real detector always says no.
var guiDetector = launchedFromGUI

// autoWebEnv turns the auto-start off. An automation whose parent is itself a
// GUI process looks exactly like a double-click, and without an escape hatch a
// run that used to exit immediately would block on a listening socket.
const autoWebEnv = "DZZZR_NO_AUTO_WEB"

// autoStartWebIfGUI covers the Windows double-click: no command was given and
// there is no terminal to read the usage text from, so the browser editor is the
// only thing that makes the run useful. It reports whether the invocation
// should be turned into «dzzzr editor»; a run from a shell says no and keeps the
// usage text and the exit code it has always had.
func autoStartWebIfGUI() bool {
	if os.Getenv(autoWebEnv) != "" {
		return false
	}
	return guiDetector()
}

// guiFromLaunchContext prefers a known launcher over console allocation:
// Explorer may share its new console with a host process or have none at all.
func guiFromLaunchContext(parent string, count int) bool {
	switch strings.ToLower(parent) {
	case "explorer.exe":
		return true
	case "cmd.exe", "powershell.exe", "pwsh.exe", "bash.exe", "sh.exe", "zsh.exe", "fish.exe", "wsl.exe", "mintty.exe", "windowsterminal.exe", "wt.exe":
		return false
	default:
		return count == 1
	}
}
