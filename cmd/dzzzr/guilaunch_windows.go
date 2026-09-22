//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
)

// launchedFromGUI reports whether dzzzr.exe was started outside an existing
// terminal — a double-click in Explorer, a shortcut, or any parent that is not
// a shell.
func launchedFromGUI() bool {
	return guiFromLaunchContext(parentProcessName(), consoleProcessCount())
}

// consoleProcessCount returns how many processes share this process's console,
// or 0 when there is no console. Two slots are enough: GetConsoleProcessList
// returns the total count even when the buffer is too small to hold every
// identifier.
func consoleProcessCount() int {
	if err := procGetConsoleProcessList.Find(); err != nil {
		// LazyProc.Call panics on a missing export; a Windows without this
		// call simply keeps the usage text.
		return 0
	}
	var pids [2]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	return int(n)
}

// parentProcessName reads one process snapshot, so Explorer and shells can be
// distinguished even when GetConsoleProcessList gives an ambiguous result.
func parentProcessName() string {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	var parentID uint32
	names := make(map[uint32]string)
	for err := windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		names[entry.ProcessID] = windows.UTF16ToString(entry.ExeFile[:])
		if entry.ProcessID == uint32(os.Getpid()) {
			parentID = entry.ParentProcessID
		}
	}
	return names[parentID]
}
