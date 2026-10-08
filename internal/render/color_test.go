package render

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestEnvColor(t *testing.T) {
	tests := []struct {
		name    string
		ansi    bool
		noColor string
		want    bool
	}{
		{name: "terminal with NO_COLOR unset", ansi: true, noColor: "", want: true},
		{name: "terminal with NO_COLOR=1", ansi: true, noColor: "1", want: false},
		{name: "terminal with NO_COLOR=0 still disables", ansi: true, noColor: "0", want: false},
		{name: "terminal with a long NO_COLOR value", ansi: true, noColor: "any non-empty value", want: false},
		{name: "no terminal, no NO_COLOR", ansi: false, noColor: "", want: false},
		{name: "no terminal and NO_COLOR set", ansi: false, noColor: "1", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := Env{
				Getenv: func(key string) string {
					if key == "NO_COLOR" {
						return tt.noColor
					}
					return ""
				},
				EnableANSI: func(io.Writer) bool { return tt.ansi },
			}
			var buf bytes.Buffer
			if got := env.Color(&buf); got != tt.want {
				t.Fatalf("Color = %v, want %v", got, tt.want)
			}
			if got := env.ANSI(&buf); got != tt.ansi {
				t.Fatalf("ANSI = %v, want %v", got, tt.ansi)
			}
			style := env.Style(&buf)
			if style.Enabled() != tt.want {
				t.Fatalf("Style().Enabled() = %v, want %v", style.Enabled(), tt.want)
			}
		})
	}
}

// TestEnvZeroValue checks the documented fallbacks: a zero Env uses the
// real environment and the real terminal probe, and a buffer is never a
// terminal.
func TestEnvZeroValue(t *testing.T) {
	var env Env
	var buf bytes.Buffer
	if env.ANSI(&buf) {
		t.Fatal("a bytes.Buffer must never count as a terminal")
	}
	if env.Style(&buf).Enabled() {
		t.Fatal("zero Env must not colour a buffer")
	}
	// With the terminal probe forced on, a nil Getenv must fall back to
	// the real environment.
	forced := Env{EnableANSI: func(io.Writer) bool { return true }}
	t.Setenv("NO_COLOR", "")
	if !forced.Color(&buf) {
		t.Fatal("empty NO_COLOR must allow color")
	}
	t.Setenv("NO_COLOR", "1")
	if forced.Color(&buf) {
		t.Fatal("NO_COLOR=1 must disable color even with a nil Getenv")
	}
}

// TestDefaultEnvBufferIsPlain pins the end-to-end rule for redirected
// output: no color without a terminal, regardless of the environment.
func TestDefaultEnvBufferIsPlain(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	var buf bytes.Buffer
	if DefaultEnv.Style(&buf).Enabled() {
		t.Fatal("DefaultEnv must not colour a buffer")
	}
	if DefaultEnv.ANSI(&buf) {
		t.Fatal("DefaultEnv must not report ANSI for a buffer")
	}
}

func TestStyleWrap(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		color   Color
		text    string
		want    string
	}{
		{name: "disabled passes through", enabled: false, color: ColorRed, text: "7700", want: "7700"},
		{name: "enabled bold wraps", enabled: true, color: ColorBold, text: "HEX", want: "\x1b[1mHEX\x1b[0m"},
		{name: "enabled red wraps", enabled: true, color: ColorRed, text: "7700", want: "\x1b[31m7700\x1b[0m"},
		{name: "enabled green wraps", enabled: true, color: ColorGreen, text: "12.4", want: "\x1b[32m12.4\x1b[0m"},
		{name: "enabled yellow wraps", enabled: true, color: ColorYellow, text: "GND", want: "\x1b[33mGND\x1b[0m"},
		{name: "enabled cyan wraps", enabled: true, color: ColorCyan, text: "3c673a", want: "\x1b[36m3c673a\x1b[0m"},
		{name: "enabled dim wraps", enabled: true, color: ColorDim, text: "-", want: "\x1b[2m-\x1b[0m"},
		{name: "ColorNone never wraps", enabled: true, color: ColorNone, text: "452", want: "452"},
		{name: "empty text never wraps", enabled: true, color: ColorRed, text: "", want: ""},
		{name: "unknown color passes through", enabled: true, color: Color(99), text: "x", want: "x"},
		{name: "disabled empty text", enabled: false, color: ColorBold, text: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStyle(tt.enabled)
			if got := s.Wrap(tt.color, tt.text); got != tt.want {
				t.Fatalf("Wrap = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestWrapKeepsAlignment is the property the tables rely on: wrapping
// never changes the visible length of the text it decorates.
func TestWrapKeepsAlignment(t *testing.T) {
	on := NewStyle(true)
	for _, c := range []Color{ColorBold, ColorDim, ColorRed, ColorGreen, ColorYellow, ColorCyan} {
		wrapped := on.Wrap(c, "ABC")
		if strings.Count(wrapped, "ABC") != 1 {
			t.Fatalf("color %v corrupted the text: %q", c, wrapped)
		}
		if !strings.HasSuffix(wrapped, "\x1b[0m") {
			t.Fatalf("color %v missing reset: %q", c, wrapped)
		}
	}
}
