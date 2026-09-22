//go:build !windows

package main

import "testing"

// Outside Windows the auto-start must never trigger: «dzzzr» without a command
// still prints usage and exits 2.
func TestLaunchedFromGUIIsFalseOffWindows(t *testing.T) {
	if launchedFromGUI() {
		t.Error("launchedFromGUI() = true, ожидалось false вне Windows")
	}
}
