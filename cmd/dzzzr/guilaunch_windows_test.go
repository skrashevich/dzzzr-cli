//go:build windows

package main

import "testing"

// The console process count is the whole signal, so its mapping is pinned
// rather than left to the one machine that can run it.
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
			if got := guiFromConsoleProcessCount(tc.n); got != tc.want {
				t.Errorf("guiFromConsoleProcessCount(%d) = %v, ожидалось %v", tc.n, got, tc.want)
			}
		})
	}
}
