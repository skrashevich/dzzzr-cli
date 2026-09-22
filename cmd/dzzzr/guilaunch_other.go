//go:build !windows

package main

// launchedFromGUI is a Windows-only concern: elsewhere dzzzr is started from a
// shell, so a run without a command keeps printing usage.
func launchedFromGUI() bool { return false }
