//go:build windows

package main

import "testing"

// Unknown launchers retain the console-count fallback.
func TestGUIFromConsoleProcessCount(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    int
		want bool
	}{
		{name: "консоль выделена под нас одних — двойной клик", n: 1, want: true},
		{name: "консоль разделена с оболочкой", n: 2, want: false},
		{name: "консоли нет: mintty, Git Bash, Wine", n: 0, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := guiFromLaunchContext("", tc.n); got != tc.want {
				t.Errorf("guiFromConsoleProcessCount(%d) = %v, ожидалось %v", tc.n, got, tc.want)
			}
		})
	}
}
