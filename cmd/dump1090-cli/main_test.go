package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/technothatch/dump1090-cli/internal/render"
)

// --- fixtures -------------------------------------------------------------

const (
	fixtureAircraft = `{"now":1759470000.5,"messages":1234567,"aircraft":[{"hex":"3c673a","flight":"BAW117  ","type":"A320","alt_baro":34000,"gs":452,"track":271.4,"squawk":"7700","lat":48.25,"lon":11.6,"dst":3.2,"dir":20,"some_future_field":[1,2]},{"hex":"4ca1b2","alt_baro":"ground","squawk":"1200"},{"hex":"abc123","flight":"EZY42   ","type":"A21N","alt_baro":12000,"gs":280,"squawk":"1200","lat":48.5,"lon":11.9}]}`
	fixtureReceiver = `{"version":"readsb 4.0.0","lat":48.208,"lon":11.512}`
	fixtureStats    = `{"latest":{"messages":5678,"local":{"blocks_processed":1200,"modes":3456,"signal":-12.4},"cpu":{"demod":1234.5},"cpr":{"global_ok":10},"tracks":{"all":12}},"last1min":{"messages":3400},"total":{"messages":1234567}}`

	emptyAircraft   = `{"now":1759470000.5,"messages":0,"aircraft":[]}`
	groundOnlyDoc   = `{"now":1,"messages":0,"aircraft":[{"hex":"4ca1b2","alt_baro":"ground"}]}`
	noSquawkDoc     = `{"now":1,"messages":0,"aircraft":[{"hex":"aaaaaa","alt_baro":30000}]}`
	noEmergencyDoc  = `{"now":1,"messages":0,"aircraft":[{"hex":"aaaaaa","squawk":"1200"}]}`
	deadBase        = "http://127.0.0.1:9"
	emptyStatsPatch = `{}`
)

func jsonHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}
}

func newServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// serveDocs builds a decoder serving only the documents it was given
// (empty strings are simply not served).
func serveDocs(t *testing.T, aircraft, receiver, stats string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	if aircraft != "" {
		mux.HandleFunc("/data/aircraft.json", jsonHandler(aircraft))
	}
	if receiver != "" {
		mux.HandleFunc("/data/receiver.json", jsonHandler(receiver))
	}
	if stats != "" {
		mux.HandleFunc("/data/stats.json", jsonHandler(stats))
	}
	return newServer(t, mux)
}

func decoderServer(t *testing.T) *httptest.Server {
	t.Helper()
	return serveDocs(t, fixtureAircraft, fixtureReceiver, fixtureStats)
}

// --- test plumbing --------------------------------------------------------

// isolate pins the environment and swaps the hook variables for
// deterministic stubs. Tests must not run in parallel and must not call it
// twice; cleanup restores everything.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("DUMP1090_URL", "")
	oldProbe, oldBases, oldNow, oldEnv := probeFn, basesFn, nowFn, outputEnv
	probeFn = func(context.Context, string) bool { return false }
	basesFn = func() []string { return []string{deadBase} }
	t.Cleanup(func() {
		probeFn, basesFn, nowFn, outputEnv = oldProbe, oldBases, oldNow, oldEnv
	})
}

// exec runs the CLI without touching hooks (for tests that set their own).
func exec(args ...string) (int, string, string) {
	var out, errb strings.Builder
	code := run(context.Background(), args, &out, &errb)
	return code, out.String(), errb.String()
}

// runOut isolates and runs the CLI with plain buffers.
func runOut(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	isolate(t)
	return exec(args...)
}

func containsAll(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q:\n%s", w, out)
		}
	}
}

func indexOf(t *testing.T, s, sub string) int {
	t.Helper()
	i := strings.Index(s, sub)
	if i < 0 {
		t.Fatalf("%q not found in output:\n%s", sub, s)
	}
	return i
}

func dataLines(s string) []string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// syncBuf is a goroutine-safe buffer for watch tests.
type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// runWatch starts watch on its own goroutine, waits until the produced
// output satisfies cond, cancels the context and returns the results.
func runWatch(t *testing.T, args []string, cond func(string) bool) (string, string, int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, errb := &syncBuf{}, &syncBuf{}
	done := make(chan int, 1)
	go func() { done <- run(ctx, args, out, errb) }()
	waitFor(t, "watch output", func() bool { return cond(out.String()) })
	cancel()
	select {
	case code := <-done:
		return out.String(), errb.String(), code
	case <-time.After(10 * time.Second):
		t.Fatal("watch did not exit after cancellation")
		return "", "", 0
	}
}

// --- argument parsing -----------------------------------------------------

func TestSplitArgs(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		flags []string
		pos   []string
		err   bool
	}{
		{name: "flags before command", args: []string{"--json", "ls"}, flags: []string{"--json"}, pos: []string{"ls"}},
		{name: "flags after command", args: []string{"ls", "--json"}, flags: []string{"--json"}, pos: []string{"ls"}},
		{name: "value flag with equals", args: []string{"--url=http://x", "ls"}, flags: []string{"--url=http://x"}, pos: []string{"ls"}},
		{name: "value flag with space", args: []string{"ls", "--url", "http://x"}, flags: []string{"--url", "http://x"}, pos: []string{"ls"}},
		{name: "dash dash stops flag parsing", args: []string{"nearest", "--", "-1.5", "-2.5"}, pos: []string{"nearest", "-1.5", "-2.5"}},
		{name: "negative coordinates are positionals", args: []string{"box", "47", "49", "-1", "1"}, pos: []string{"box", "47", "49", "-1", "1"}},
		{name: "single dash is positional", args: []string{"ls", "-"}, pos: []string{"ls", "-"}},
		{name: "repeated flags keep order", args: []string{"--json", "--metric", "ls"}, flags: []string{"--json", "--metric"}, pos: []string{"ls"}},
		{name: "unknown flag", args: []string{"--bogus", "ls"}, err: true},
		{name: "value flag without value", args: []string{"--url"}, err: true},
		{name: "value flag followed by flag", args: []string{"--url", "--json", "ls"}, err: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flags, pos, err := splitArgs(tt.args)
			if tt.err {
				if err == nil {
					t.Fatalf("want error, got flags=%v pos=%v", flags, pos)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slices.Equal(flags, tt.flags) {
				t.Errorf("flags = %v, want %v", flags, tt.flags)
			}
			if !slices.Equal(pos, tt.pos) {
				t.Errorf("pos = %v, want %v", pos, tt.pos)
			}
		})
	}
}

func TestParseArgsDefaults(t *testing.T) {
	o, err := parseArgs([]string{"ls"}, io.Discard)
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if o.command != "ls" || o.interval != time.Second || o.timeout != 5*time.Second || o.limit != -1 {
		t.Errorf("defaults wrong: %+v", o)
	}
	if o.jsonOut || o.metric || o.help {
		t.Errorf("boolean defaults wrong: %+v", o)
	}

	t.Run("flags anywhere", func(t *testing.T) {
		o, err := parseArgs([]string{"ls", "--json", "--limit", "5", "--metric"}, io.Discard)
		if err != nil {
			t.Fatalf("parseArgs: %v", err)
		}
		if o.command != "ls" || !o.jsonOut || !o.metric || o.limit != 5 {
			t.Errorf("got %+v", o)
		}
	})

	t.Run("invalid duration is an error", func(t *testing.T) {
		if _, err := parseArgs([]string{"--interval", "bogus"}, io.Discard); err == nil {
			t.Fatal("want error for invalid --interval")
		}
	})
}

// --- usage and exit codes -------------------------------------------------

func TestRunHelp(t *testing.T) {
	for _, flagName := range []string{"--help", "-h"} {
		code, out, _ := runOut(t, flagName)
		if code != 0 {
			t.Errorf("%s exit = %d, want 0", flagName, code)
		}
		containsAll(t, out, "usage:", "Commands:", "--json")
	}
}

func TestRunUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "no command", args: []string{"--json"}, want: "usage:"},
		{name: "unknown command", args: []string{"frobnicate"}, want: "unknown command"},
		{name: "ls with argument", args: []string{"ls", "x"}, want: "takes no arguments"},
		{name: "info without hex", args: []string{"info"}, want: "usage: dump1090-cli info HEX"},
		{name: "info invalid hex", args: []string{"info", "xyz"}, want: "invalid hex address"},
		{name: "nearest without coords", args: []string{"nearest", "48"}, want: "usage: dump1090-cli nearest LAT LON"},
		{name: "nearest bad latitude", args: []string{"nearest", "95", "0"}, want: "between -90 and 90"},
		{name: "nearest unparseable longitude", args: []string{"nearest", "48", "abc"}, want: "invalid longitude"},
		{name: "box wrong arity", args: []string{"box", "47", "49", "11"}, want: "usage: dump1090-cli box"},
		{name: "box unparseable coordinate", args: []string{"box", "47", "49", "11", "x"}, want: "invalid coordinate"},
		{name: "box bad latitude", args: []string{"box", "95", "49", "11", "12"}, want: "LAT1"},
		{name: "zero interval", args: []string{"--interval", "0", "ls"}, want: "--interval must be positive"},
		{name: "negative timeout", args: []string{"--timeout", "-1s", "ls"}, want: "--timeout must be positive"},
		{name: "negative limit", args: []string{"--limit", "-5", "ls"}, want: "--limit must be >= 0"},
		{name: "unknown flag", args: []string{"--bogus"}, want: "unknown flag"},
		{name: "flag missing value", args: []string{"--url"}, want: "flag needs an argument"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, stderr := runOut(t, tt.args...)
			if code != 2 {
				t.Errorf("exit = %d, want 2", code)
			}
			containsAll(t, stderr, tt.want)
		})
	}
}

// --- command output -------------------------------------------------------

func TestRunLsTable(t *testing.T) {
	srv := decoderServer(t)
	code, out, stderr := runOut(t, "--url", srv.URL, "ls")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	containsAll(t, out, "HEX", "CALLSIGN", "DST(nm)", "3c673a", "BAW117", "GND")
	// Sorted by distance from the receiver: dst field first, computed
	// second, the aircraft without any distance last.
	i1 := indexOf(t, out, "3c673a")
	i2 := indexOf(t, out, "abc123")
	i3 := indexOf(t, out, "4ca1b2")
	if i1 >= i2 || i2 >= i3 {
		t.Errorf("rows out of order: %d, %d, %d\n%s", i1, i2, i3, out)
	}
}

func TestRunLsJSON(t *testing.T) {
	srv := decoderServer(t)
	code, out, stderr := runOut(t, "--url", srv.URL, "--json", "ls")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Error("JSON output must end with a newline")
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("--json must never emit ANSI escapes")
	}
	var doc struct {
		Now      float64          `json:"now"`
		Messages int64            `json:"messages"`
		Aircraft []map[string]any `json:"aircraft"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if doc.Now != 1759470000.5 || doc.Messages != 1234567 {
		t.Errorf("header fields wrong: now=%v messages=%d", doc.Now, doc.Messages)
	}
	if len(doc.Aircraft) != 3 {
		t.Fatalf("len(aircraft) = %d, want 3", len(doc.Aircraft))
	}
	if doc.Aircraft[0]["hex"] != "3c673a" {
		t.Errorf("first aircraft = %v, want 3c673a", doc.Aircraft[0]["hex"])
	}
	if _, ok := doc.Aircraft[0]["some_future_field"]; !ok {
		t.Error("raw element lost an unknown key")
	}
}

func TestRunLsEmpty(t *testing.T) {
	srv := serveDocs(t, emptyAircraft, "", "")
	code, out, stderr := runOut(t, "--url", srv.URL, "ls")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if out != "no aircraft\n" {
		t.Errorf("out = %q, want %q", out, "no aircraft\n")
	}
}

func TestRunLsLimitAndMetric(t *testing.T) {
	srv := decoderServer(t)
	code, out, stderr := runOut(t, "--url", srv.URL, "--limit", "1", "ls")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if lines := dataLines(out); len(lines) != 2 {
		t.Errorf("got %d lines (header + rows), want 2:\n%s", len(lines), out)
	}

	code, out, stderr = runOut(t, "--url", srv.URL, "--metric", "ls")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	containsAll(t, out, "ALT(m)", "GS(km/h)", "DST(km)")
}

func TestRunInfo(t *testing.T) {
	srv := decoderServer(t)
	code, out, stderr := runOut(t, "--url", srv.URL, "info", "3c673a")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	containsAll(t, out, "3c673a", "BAW117", "A320", "7700", "34000 ft")
}

func TestRunInfoNotFound(t *testing.T) {
	srv := decoderServer(t)
	code, _, stderr := runOut(t, "--url", srv.URL, "info", "ffffff")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	containsAll(t, stderr, `no aircraft with hex "ffffff"`)
}

func TestRunInfoJSON(t *testing.T) {
	srv := decoderServer(t)
	code, out, stderr := runOut(t, "--url", srv.URL, "--json", "info", "3c673a")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	var ac map[string]any
	if err := json.Unmarshal([]byte(out), &ac); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if ac["hex"] != "3c673a" {
		t.Errorf("hex = %v", ac["hex"])
	}
	if _, ok := ac["some_future_field"]; !ok {
		t.Error("info --json lost the raw element")
	}
}

func TestRunNearest(t *testing.T) {
	srv := decoderServer(t)
	code, out, stderr := runOut(t, "--url", srv.URL, "nearest", "0", "0")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	lines := dataLines(out)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want header + 2 positioned rows:\n%s", len(lines), out)
	}
	if i, j := indexOf(t, out, "3c673a"), indexOf(t, out, "abc123"); i > j {
		t.Errorf("nearest order wrong: %d > %d\n%s", i, j, out)
	}
}

func TestRunNearestMessages(t *testing.T) {
	srv := serveDocs(t, groundOnlyDoc, "", "")
	code, out, stderr := runOut(t, "--url", srv.URL, "nearest", "48", "11")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if out != "no aircraft with a known position\n" {
		t.Errorf("out = %q", out)
	}

	srv = serveDocs(t, emptyAircraft, "", "")
	code, out, stderr = runOut(t, "--url", srv.URL, "nearest", "48", "11")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if out != "no aircraft\n" {
		t.Errorf("out = %q", out)
	}
}

func TestRunNearestJSON(t *testing.T) {
	srv := decoderServer(t)
	code, out, stderr := runOut(t, "--url", srv.URL, "--json", "nearest", "48.2", "11.5")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	var nd struct {
		Point struct {
			Lat float64 `json:"lat"`
			Lon float64 `json:"lon"`
		} `json:"point"`
		Aircraft []map[string]any `json:"aircraft"`
	}
	if err := json.Unmarshal([]byte(out), &nd); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if nd.Point.Lat != 48.2 || nd.Point.Lon != 11.5 {
		t.Errorf("point = %+v", nd.Point)
	}
	if len(nd.Aircraft) != 2 {
		t.Fatalf("len(aircraft) = %d, want 2 (ground aircraft excluded)", len(nd.Aircraft))
	}
	d0, ok0 := nd.Aircraft[0]["dst"].(float64)
	d1, ok1 := nd.Aircraft[1]["dst"].(float64)
	if !ok0 || !ok1 {
		t.Fatalf("dst missing: %v %v", nd.Aircraft[0], nd.Aircraft[1])
	}
	if d0 >= d1 {
		t.Errorf("distances not ascending: %v, %v", d0, d1)
	}
}

func TestRunBox(t *testing.T) {
	srv := decoderServer(t)
	code, out, stderr := runOut(t, "--url", srv.URL, "box", "48", "49", "11", "12")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	containsAll(t, out, "3c673a", "abc123")
	if strings.Contains(out, "4ca1b2") {
		t.Errorf("ground aircraft leaked into the box:\n%s", out)
	}
	if lines := dataLines(out); len(lines) != 3 {
		t.Errorf("got %d lines, want header + 2 rows:\n%s", len(lines), out)
	}

	t.Run("empty box", func(t *testing.T) {
		code, out, stderr := runOut(t, "--url", srv.URL, "box", "-10", "-5", "-10", "-5")
		if code != 0 {
			t.Fatalf("exit = %d, stderr: %s", code, stderr)
		}
		if out != "no aircraft in box\n" {
			t.Errorf("out = %q", out)
		}
	})

	t.Run("json", func(t *testing.T) {
		code, out, stderr := runOut(t, "--url", srv.URL, "--json", "box", "48", "49", "11", "12")
		if code != 0 {
			t.Fatalf("exit = %d, stderr: %s", code, stderr)
		}
		var doc struct {
			Aircraft []map[string]any `json:"aircraft"`
		}
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(doc.Aircraft) != 2 {
			t.Fatalf("len = %d, want 2", len(doc.Aircraft))
		}
		if doc.Aircraft[0]["hex"] != "3c673a" {
			t.Errorf("first = %v, want 3c673a", doc.Aircraft[0]["hex"])
		}
	})
}

func TestRunStats(t *testing.T) {
	srv := decoderServer(t)
	code, out, stderr := runOut(t, "--url", srv.URL, "stats")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	containsAll(t, out, "METRIC", "latest", "cpu.demod_ms", "1234.5", "1234567")
}

func TestRunStatsJSONIsVerbatim(t *testing.T) {
	srv := decoderServer(t)
	code, out, stderr := runOut(t, "--url", srv.URL, "--json", "stats")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if strings.TrimSpace(out) != strings.TrimSpace(fixtureStats) {
		t.Errorf("stats --json changed the document:\n got %q\nwant %q", strings.TrimSpace(out), strings.TrimSpace(fixtureStats))
	}
}

func TestRunStatsEmpty(t *testing.T) {
	srv := serveDocs(t, "", "", emptyStatsPatch)
	code, out, stderr := runOut(t, "--url", srv.URL, "stats")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if out != "no stats available\n" {
		t.Errorf("out = %q", out)
	}
}

func TestRunSquawks(t *testing.T) {
	srv := decoderServer(t)
	code, out, stderr := runOut(t, "--url", srv.URL, "squawks")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	containsAll(t, out, "SQUAWK", "3 aircraft, 3 with squawk, 2 distinct squawks")
	if i, j := indexOf(t, out, "1200"), indexOf(t, out, "7700"); i > j {
		t.Errorf("busiest squawk should come first:\n%s", out)
	}

	t.Run("json", func(t *testing.T) {
		code, out, stderr := runOut(t, "--url", srv.URL, "--json", "squawks")
		if code != 0 {
			t.Fatalf("exit = %d, stderr: %s", code, stderr)
		}
		var sd struct {
			Total      int `json:"total"`
			WithSquawk int `json:"with_squawk"`
			Squawks    []struct {
				Squawk string   `json:"squawk"`
				Count  int      `json:"count"`
				Hexes  []string `json:"hexes"`
			} `json:"squawks"`
		}
		if err := json.Unmarshal([]byte(out), &sd); err != nil {
			t.Fatalf("decode: %v\n%s", err, out)
		}
		if sd.Total != 3 || sd.WithSquawk != 3 || len(sd.Squawks) != 2 {
			t.Fatalf("got total=%d with=%d groups=%d", sd.Total, sd.WithSquawk, len(sd.Squawks))
		}
		g := sd.Squawks[0]
		if g.Squawk != "1200" || g.Count != 2 || !slices.Equal(g.Hexes, []string{"4ca1b2", "abc123"}) {
			t.Errorf("groups[0] = %+v", g)
		}
	})

	t.Run("no squawks at all", func(t *testing.T) {
		srv := serveDocs(t, noSquawkDoc, "", "")
		code, out, stderr := runOut(t, "--url", srv.URL, "squawks")
		if code != 0 {
			t.Fatalf("exit = %d, stderr: %s", code, stderr)
		}
		if out != "no aircraft reporting a squawk\n" {
			t.Errorf("out = %q", out)
		}
	})
}

func TestRunEmergency(t *testing.T) {
	srv := decoderServer(t)
	code, out, stderr := runOut(t, "--url", srv.URL, "emergency")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	containsAll(t, out, "EMERGENCY", "3c673a", "7700")
	if strings.Contains(out, "abc123") || strings.Contains(out, "4ca1b2") {
		t.Errorf("non-emergency aircraft leaked in:\n%s", out)
	}

	t.Run("json preserves raw elements", func(t *testing.T) {
		code, out, stderr := runOut(t, "--url", srv.URL, "--json", "emergency")
		if code != 0 {
			t.Fatalf("exit = %d, stderr: %s", code, stderr)
		}
		var doc struct {
			Aircraft []map[string]any `json:"aircraft"`
		}
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(doc.Aircraft) != 1 || doc.Aircraft[0]["hex"] != "3c673a" {
			t.Fatalf("aircraft = %v", doc.Aircraft)
		}
		if _, ok := doc.Aircraft[0]["some_future_field"]; !ok {
			t.Error("raw element lost an unknown key")
		}
	})

	t.Run("none", func(t *testing.T) {
		srv := serveDocs(t, noEmergencyDoc, "", "")
		code, out, stderr := runOut(t, "--url", srv.URL, "emergency")
		if code != 0 {
			t.Fatalf("exit = %d, stderr: %s", code, stderr)
		}
		if out != "no aircraft in emergency\n" {
			t.Errorf("out = %q", out)
		}
	})
}

// --- decoder failures and discovery ---------------------------------------

func TestRunDecoderDown(t *testing.T) {
	srv := newServer(t, jsonHandler(fixtureAircraft))
	srv.Close()
	code, _, stderr := runOut(t, "--url", srv.URL, "ls")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	containsAll(t, stderr, "ls:")
	if strings.Contains(stderr, "auto-discovery") {
		t.Errorf("explicit --url must not print the auto-discovery hint:\n%s", stderr)
	}
}

func TestRunHTMLPageIsNotData(t *testing.T) {
	srv := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><body>proxy error</body></html>")
	}))
	code, _, stderr := runOut(t, "--url", srv.URL, "ls")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	containsAll(t, stderr, "no usable response")
}

func TestRunAutoDiscoveryHint(t *testing.T) {
	isolate(t)
	code, _, stderr := exec("ls")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	containsAll(t, stderr, "auto-discovery checked "+deadBase, "Try --url")
}

func TestRunDiscoveryPicksAliveBase(t *testing.T) {
	isolate(t)
	srv := decoderServer(t)
	var probed []string
	probeFn = func(_ context.Context, base string) bool {
		probed = append(probed, base)
		return base == srv.URL
	}
	basesFn = func() []string { return []string{deadBase, srv.URL} }

	code, out, stderr := exec("ls")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if !slices.Equal(probed, []string{deadBase, srv.URL}) {
		t.Errorf("probe order = %v", probed)
	}
	containsAll(t, out, "3c673a")
}

func TestRunFlagURLSkipsProbe(t *testing.T) {
	isolate(t)
	srv := decoderServer(t)
	calls := 0
	probeFn = func(context.Context, string) bool { calls++; return false }

	code, _, stderr := exec("--url", srv.URL, "ls")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if calls != 0 {
		t.Errorf("probed %d times, want 0", calls)
	}
}

func TestRunEnvURLSkipsProbe(t *testing.T) {
	isolate(t)
	srv := decoderServer(t)
	t.Setenv("DUMP1090_URL", srv.URL)
	calls := 0
	probeFn = func(context.Context, string) bool { calls++; return false }

	code, _, stderr := exec("ls")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if calls != 0 {
		t.Errorf("probed %d times, want 0", calls)
	}
}

func TestRunPathFallbacks(t *testing.T) {
	t.Run("aircraft.json at the root", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/aircraft.json", jsonHandler(fixtureAircraft))
		srv := newServer(t, mux)
		code, out, stderr := runOut(t, "--url", srv.URL, "ls")
		if code != 0 {
			t.Fatalf("exit = %d, stderr: %s", code, stderr)
		}
		containsAll(t, out, "3c673a")
	})

	t.Run("readsb API root", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, fixtureAircraft)
		})
		srv := newServer(t, mux)
		code, out, stderr := runOut(t, "--url", srv.URL, "ls")
		if code != 0 {
			t.Fatalf("exit = %d, stderr: %s", code, stderr)
		}
		containsAll(t, out, "3c673a")
	})

	t.Run("missing receiver is not fatal", func(t *testing.T) {
		srv := serveDocs(t, fixtureAircraft, "", "")
		code, out, stderr := runOut(t, "--url", srv.URL, "ls")
		if code != 0 {
			t.Fatalf("exit = %d, stderr: %s", code, stderr)
		}
		containsAll(t, out, "3c673a")
	})
}

// --- colors ---------------------------------------------------------------

func TestRunColorDecision(t *testing.T) {
	srv := decoderServer(t)

	t.Run("terminal without NO_COLOR is colored", func(t *testing.T) {
		isolate(t)
		outputEnv = render.Env{
			Getenv:     func(string) string { return "" },
			EnableANSI: func(io.Writer) bool { return true },
		}
		code, out, stderr := exec("--url", srv.URL, "ls")
		if code != 0 {
			t.Fatalf("exit = %d, stderr: %s", code, stderr)
		}
		containsAll(t, out, "\x1b[36m3c673a\x1b[0m", "\x1b[1m")
	})

	t.Run("NO_COLOR disables color even on a terminal", func(t *testing.T) {
		isolate(t)
		outputEnv = render.Env{
			Getenv: func(k string) string {
				if k == "NO_COLOR" {
					return "1"
				}
				return ""
			},
			EnableANSI: func(io.Writer) bool { return true },
		}
		code, out, stderr := exec("--url", srv.URL, "ls")
		if code != 0 {
			t.Fatalf("exit = %d, stderr: %s", code, stderr)
		}
		if strings.Contains(out, "\x1b[") {
			t.Errorf("NO_COLOR must suppress escapes:\n%s", out)
		}
		containsAll(t, out, "3c673a")
	})

	t.Run("redirected output is plain", func(t *testing.T) {
		code, out, _ := runOut(t, "--url", srv.URL, "ls")
		if code != 0 {
			t.Fatalf("exit = %d", code)
		}
		if strings.Contains(out, "\x1b[") {
			t.Errorf("a buffer must not be colored:\n%s", out)
		}
	})
}

// --- watch ----------------------------------------------------------------

func TestRunWatchPlainFrame(t *testing.T) {
	isolate(t)
	srv := decoderServer(t)
	nowFn = func() time.Time { return time.Date(2026, 10, 7, 15, 4, 5, 0, time.UTC) }

	out, stderr, code := runWatch(t,
		[]string{"watch", "--interval", "20ms", "--url", srv.URL},
		func(s string) bool { return strings.Contains(s, "HEX") })
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	containsAll(t, out, "15:04:05", "3 aircraft", "1234567 messages", "3c673a")
	if strings.Contains(out, "\x1b[") {
		t.Errorf("redirected watch must stay plain:\n%s", out)
	}
}

func TestRunWatchANSIFrame(t *testing.T) {
	isolate(t)
	srv := decoderServer(t)
	outputEnv = render.Env{
		Getenv:     func(string) string { return "" },
		EnableANSI: func(io.Writer) bool { return true },
	}

	out, stderr, code := runWatch(t,
		[]string{"watch", "--interval", "20ms", "--url", srv.URL},
		func(s string) bool { return strings.Contains(s, "HEX") })
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	containsAll(t, out, "\x1b[?25l", "\x1b[2J\x1b[H", "3c673a", "\x1b[?25h")
}

func TestRunWatchJSONIsNDJSON(t *testing.T) {
	isolate(t)
	srv := decoderServer(t)

	out, stderr, code := runWatch(t,
		[]string{"watch", "--interval", "20ms", "--json", "--url", srv.URL},
		func(s string) bool { return strings.Count(s, "\n") >= 2 })
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	first, _, _ := strings.Cut(out, "\n")
	var doc struct {
		Aircraft []map[string]any `json:"aircraft"`
	}
	if err := json.Unmarshal([]byte(first), &doc); err != nil {
		t.Fatalf("first frame is not JSON: %v\n%s", err, first)
	}
	if len(doc.Aircraft) != 3 {
		t.Errorf("first frame has %d aircraft, want 3", len(doc.Aircraft))
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("--json watch must not emit ANSI")
	}
}
