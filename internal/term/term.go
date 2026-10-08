// Package term provides the two pieces of terminal awareness dump1090-cli
// needs — "is this writer a terminal?" and "can it render ANSI?" — using
// only the standard library.
package term

import (
	"io"
	"os"
)

// IsTerminal reports whether w is a character device (console, pty), which
// is the stdlib-only approximation of isatty. Pipes, files and test
// buffers are not terminals, so redirected output never gets escape
// sequences or colors.
func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
