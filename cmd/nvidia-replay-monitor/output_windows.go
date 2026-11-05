package main

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Use an existing terminal or redirected stdout when available. A GUI build
// never creates a console; interactive commands otherwise use a message box.
func showMessage(message string, failure bool) {
	if info, err := os.Stdout.Stat(); err == nil && info.Mode()&os.ModeCharDevice == 0 {
		fmt.Fprintln(os.Stdout, message)
		return
	}
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	attached, _, _ := kernel.NewProc("AttachConsole").Call(uintptr(^uint32(0)))
	if attached != 0 || consoleAvailable() {
		if handle, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE); err == nil {
			_, _ = fmt.Fprintln(os.NewFile(uintptr(handle), "stdout"), message)
			return
		}
	}
	text, _ := windows.UTF16PtrFromString(message)
	title, _ := windows.UTF16PtrFromString("NVIDIAReplayMonitor")
	style := uintptr(0x40)
	if failure {
		style = 0x10
	}
	_, _, _ = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), style)
}

func consoleAvailable() bool {
	handle, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		return false
	}
	var mode uint32
	return windows.GetConsoleMode(handle, &mode) == nil
}
