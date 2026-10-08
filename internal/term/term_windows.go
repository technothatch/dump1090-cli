//go:build windows

package term

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

// enableVirtualTerminalProcessing is the Win32 console mode flag that makes
// the legacy conhost render ANSI sequences instead of printing them raw.
const enableVirtualTerminalProcessing = 0x0004

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode = kernel32.NewProc("SetConsoleMode")
)

// EnableANSI reports whether ANSI escape sequences written to w will be
// honoured. For a Windows console it switches on virtual terminal
// processing first (PowerShell's legacy conhost otherwise prints the
// sequences verbatim); for pipes and files — which store bytes literally
// and so must never receive framing escapes — it reports false.
func EnableANSI(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	handle := syscall.Handle(f.Fd())
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(uintptr(handle), uintptr(unsafe.Pointer(&mode)))
	if r == 0 {
		// Not a console handle: redirected output.
		return false
	}
	if mode&enableVirtualTerminalProcessing != 0 {
		return true
	}
	r, _, _ = procSetConsoleMode.Call(uintptr(handle), uintptr(mode|enableVirtualTerminalProcessing))
	return r != 0
}
