// Package render turns aircraft, decoder statistics and squawk groupings
// into the CLI's human-facing output: aligned plain-text tables, the info
// view, ANSI colors (only on a terminal, with NO_COLOR respected) and the
// unit conversions behind --metric.
package render

import (
	"io"
	"os"

	"github.com/technothatch/dump1090-cli/internal/term"
)

// Color names a text style applied when color output is enabled.
type Color int

// The palette is deliberately small: emphasis (bold), de-emphasis (dim)
// and the three meaning-carrying colors — red for emergencies, yellow for
// on-ground, cyan for addresses.
const (
	ColorNone Color = iota
	ColorBold
	ColorDim
	ColorRed
	ColorGreen
	ColorYellow
	ColorCyan
)

// colorCodes are the ANSI SGR sequences for each Color.
var colorCodes = map[Color]string{
	ColorBold:   "\x1b[1m",
	ColorDim:    "\x1b[2m",
	ColorRed:    "\x1b[31m",
	ColorGreen:  "\x1b[32m",
	ColorYellow: "\x1b[33m",
	ColorCyan:   "\x1b[36m",
}

// resetCode restores the default rendition.
const resetCode = "\x1b[0m"

// Style wraps text in ANSI colors, or returns it untouched when color is
// disabled. The zero Style is a working colorless style.
type Style struct {
	enabled bool
}

// NewStyle returns a Style that colours when enabled.
func NewStyle(enabled bool) Style { return Style{enabled: enabled} }

// Enabled reports whether this style colours text.
func (s Style) Enabled() bool { return s.enabled }

// Wrap surrounds text with the color's escape sequence and a reset. Empty
// text and ColorNone pass through unchanged so padding never gains width.
func (s Style) Wrap(c Color, text string) string {
	if !s.enabled || c == ColorNone || text == "" {
		return text
	}
	code, ok := colorCodes[c]
	if !ok {
		return text
	}
	return code + text + resetCode
}

// Env captures everything that decides how output is decorated: the
// environment variables consulted and the terminal capability probe.
// Tests inject stubs to cover TTY and NO_COLOR behaviour without owning a
// terminal.
type Env struct {
	// Getenv reads an environment variable.
	Getenv func(string) string
	// EnableANSI reports whether ANSI escapes written to w are honoured
	// (a terminal; on Windows, after enabling VT processing).
	EnableANSI func(io.Writer) bool
}

// DefaultEnv is the real process environment.
var DefaultEnv = Env{Getenv: os.Getenv, EnableANSI: term.EnableANSI}

// ANSI reports whether w can take ANSI escape sequences.
func (e Env) ANSI(w io.Writer) bool {
	enable := e.EnableANSI
	if enable == nil {
		enable = term.EnableANSI
	}
	return enable(w)
}

// Color reports whether ANSI colors may be written to w: the terminal must
// support them, and NO_COLOR (https://no-color.org) must be unset or empty.
// A non-empty value of any length disables color.
func (e Env) Color(w io.Writer) bool {
	if !e.ANSI(w) {
		return false
	}
	getenv := e.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	return getenv("NO_COLOR") == ""
}

// Style returns the Style for writing to w.
func (e Env) Style(w io.Writer) Style { return NewStyle(e.Color(w)) }
