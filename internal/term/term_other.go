//go:build !windows

package term

import "io"

// EnableANSI reports whether ANSI escape sequences written to w will be
// honoured. POSIX terminals render them by default; pipes and files store
// them as literal bytes, so they are not.
func EnableANSI(w io.Writer) bool {
	return IsTerminal(w)
}
