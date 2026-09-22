package main

import "os"

// guiDetector is an indirection so the auto-start decision can be tested off
// Windows, where the real detector always says no.
var guiDetector = launchedFromGUI

// autoWebEnv turns the auto-start off. An automation whose parent is itself a
// GUI process looks exactly like a double-click, and without an escape hatch a
// run that used to exit immediately would block on a listening socket.
const autoWebEnv = "DZZZR_NO_AUTO_WEB"

// autoStartWebIfGUI covers the Windows double-click: no command was given and
// there is no terminal to read the usage text from, so the browser chat is the
// only thing that makes the run useful. It reports whether the invocation
// should be turned into «dzzzr web»; a run from a shell says no and keeps the
// usage text and the exit code it has always had.
func autoStartWebIfGUI() bool {
	if os.Getenv(autoWebEnv) != "" {
		return false
	}
	return guiDetector()
}
