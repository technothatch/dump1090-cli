package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/technothatch/dump1090-cli/internal/dump1090"
	"github.com/technothatch/dump1090-cli/internal/query"
)

func fp(v float64) *float64 { return &v }

// mustAircraft decodes one aircraft.json element for fixtures.
func mustAircraft(t *testing.T, raw string) dump1090.Aircraft {
	t.Helper()
	var a dump1090.Aircraft
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	return a
}

// fleetFixture is the two-aircraft fleet every table golden uses: one
// airborne BAW117 with a decoder-computed distance, one on the ground
// without a position.
func fleetFixture(t *testing.T) []dump1090.Aircraft {
	return []dump1090.Aircraft{
		mustAircraft(t, `{"hex":"3c673a","flight":"BAW117  ","type":"A320","alt_baro":34000,"gs":452,"track":271.4,"squawk":"7700","dst":12.44,"dir":271}`),
		mustAircraft(t, `{"hex":"4ca1b2","alt_baro":"ground","squawk":"1200"}`),
	}
}

func TestAircraftTableGolden(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		acs  func(t *testing.T) []dump1090.Aircraft
		want string
	}{
		{
			name: "imperial",
			opts: Options{},
			acs:  fleetFixture,
			want: "HEX     CALLSIGN  TYPE  ALT(ft)  GS(kt)  TRACK  SQUAWK  DST(nm)\n" +
				"3c673a  BAW117    A320    34000     452    271    7700     12.4\n" +
				"4ca1b2  -         -         GND       -      -    1200        -\n",
		},
		{
			name: "metric",
			opts: Options{Metric: true},
			acs:  fleetFixture,
			want: "HEX     CALLSIGN  TYPE  ALT(m)  GS(km/h)  TRACK  SQUAWK  DST(km)\n" +
				"3c673a  BAW117    A320   10363       837    271    7700     23.0\n" +
				"4ca1b2  -         -        GND         -      -    1200        -\n",
		},
		{
			name: "empty fleet renders the header only",
			opts: Options{},
			acs:  func(t *testing.T) []dump1090.Aircraft { return nil },
			want: "HEX  CALLSIGN  TYPE  ALT(ft)  GS(kt)  TRACK  SQUAWK  DST(nm)\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AircraftTable(tt.acs(t), tt.opts)
			if got != tt.want {
				t.Fatalf("AircraftTable:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

// TestAircraftTableColor checks the colored rendition: codes wrap the
// text only, so every line keeps the exact width of the plain table.
func TestAircraftTableColor(t *testing.T) {
	opts := Options{Style: NewStyle(true)}
	got := AircraftTable(fleetFixture(t), opts)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	// Header is bold; addresses cyan; callsign bold; GND yellow; the
	// emergency squawk red; distances green; missing values dim.
	if !strings.HasPrefix(lines[0], "\x1b[1mHEX\x1b[0m") {
		t.Errorf("header not bold: %q", lines[0])
	}
	if !strings.Contains(lines[1], "\x1b[36m3c673a\x1b[0m") {
		t.Errorf("hex not cyan: %q", lines[1])
	}
	if !strings.Contains(lines[1], "\x1b[1mBAW117\x1b[0m") {
		t.Errorf("callsign not bold: %q", lines[1])
	}
	if !strings.Contains(lines[1], "\x1b[31m7700\x1b[0m") {
		t.Errorf("emergency squawk not red: %q", lines[1])
	}
	if !strings.Contains(lines[1], "\x1b[32m12.4\x1b[0m") {
		t.Errorf("distance not green: %q", lines[1])
	}
	if !strings.Contains(lines[2], "\x1b[33mGND\x1b[0m") {
		t.Errorf("ground altitude not yellow: %q", lines[2])
	}
	if !strings.Contains(lines[2], "\x1b[2m-\x1b[0m") {
		t.Errorf("missing values not dim: %q", lines[2])
	}
	// Visible widths must equal the plain table's.
	plain := AircraftTable(fleetFixture(t), Options{})
	plainLines := strings.Split(strings.TrimRight(plain, "\n"), "\n")
	for i := range lines {
		if visible := visibleLen(lines[i]); visible != len(plainLines[i]) {
			t.Errorf("line %d width %d, plain %d\n colored: %q\n plain:   %q", i, visible, len(plainLines[i]), lines[i], plainLines[i])
		}
	}
}

// visibleLen measures a line as a terminal renders it: escape sequences
// take no space.
func visibleLen(s string) int {
	n, inEscape := 0, false
	for _, r := range s {
		switch {
		case inEscape:
			if r == 'm' {
				inEscape = false
			}
		case r == '\x1b':
			inEscape = true
		default:
			n++
		}
	}
	return n
}

func TestInfoGolden(t *testing.T) {
	rich := mustAircraft(t, `{"hex":"3c673a","flight":"BAW117  ","type":"A320","r":"G-EUUU","category":"A3",`+
		`"squawk":"7700","emergency":"unlawful","alt_baro":34000,"alt_geom":34500,"gs":452,"ias":430,"tas":480,`+
		`"mach":0.78,"track":271.4,"baro_rate":1200,"lat":51.47,"lon":-0.45,"dst":12.44,"dir":271.5,`+
		`"rssi":-3.2,"messages":1234,"seen":2.1,"seen_pos":3,"mlat":["lat","lon"]}`)
	want := "hex                3c673a\n" +
		"callsign           BAW117\n" +
		"type               A320\n" +
		"registration       G-EUUU\n" +
		"category           A3\n" +
		"squawk             7700\n" +
		"emergency          unlawful\n" +
		"altitude           34000 ft\n" +
		"geom altitude      34500 ft\n" +
		"ground speed       452 kt\n" +
		"indicated airspeed 430 kt\n" +
		"true airspeed      480 kt\n" +
		"mach               0.780\n" +
		"track              271.4 deg\n" +
		"baro rate          +1200 ft/min\n" +
		"position           51.4700, -0.4500\n" +
		"dst                12.4 nm from receiver\n" +
		"dir                272 deg from receiver\n" +
		"rssi               -3.2 dBFS\n" +
		"messages           1234\n" +
		"seen               2.1 s\n" +
		"seen pos           3.0 s\n" +
		"mlat               lat, lon\n"
	if got := Info(&rich, Options{}); got != want {
		t.Fatalf("Info(rich):\n got %q\nwant %q", got, want)
	}

	t.Run("sparse aircraft shows only what exists", func(t *testing.T) {
		sparse := mustAircraft(t, `{"hex":"abc123"}`)
		want := "hex abc123\n"
		if got := Info(&sparse, Options{}); got != want {
			t.Fatalf("Info(sparse) = %q, want %q", got, want)
		}
	})

	t.Run("ground aircraft reports ground altitude", func(t *testing.T) {
		ground := mustAircraft(t, `{"hex":"4ca1b2","alt_baro":"ground","lat":48.35,"lon":11.78}`)
		got := Info(&ground, Options{Receiver: &dump1090.Receiver{Lat: fp(48.208), Lon: fp(11.512)}})
		if !strings.Contains(got, "altitude ground\n") {
			t.Errorf("ground altitude line missing:\n%s", got)
		}
		if !strings.Contains(got, "nm from receiver") {
			t.Errorf("receiver-relative dst missing:\n%s", got)
		}
	})
}

func TestInfoMetric(t *testing.T) {
	ac := mustAircraft(t, `{"hex":"3c673a","alt_baro":34000,"gs":452,"baro_rate":-640,"dst":12.44,"dir":90}`)
	got := Info(&ac, Options{Metric: true})
	for _, want := range []string{
		"altitude     10363 m\n",
		"ground speed 837 km/h\n",
		"baro rate    -195 m/min\n",
		"dst          23.0 km from receiver\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestStatsTableGolden(t *testing.T) {
	stats := dump1090.Stats{
		Latest: &dump1090.StatsWindow{
			Messages: fp(5678),
			Local: &dump1090.LocalStats{
				BlocksProcessed: fp(1200), BlocksDropped: fp(7), Modes: fp(3456),
				ModeAC: fp(12), Bad: fp(3), UnknownICAO: fp(21),
				Signal: fp(-12.4), PeakSignal: fp(-3.2), StrongSignals: fp(42),
			},
			CPU:    &dump1090.CPUStats{Demod: fp(1234.5), Reader: fp(234.5), Background: fp(34.5)},
			CPR:    &dump1090.CPRStats{GlobalOK: fp(10), GlobalBad: fp(1), GlobalSkipped: fp(2), LocalOK: fp(20), LocalBad: fp(0), LocalSkipped: fp(1)},
			Tracks: &dump1090.TracksStats{All: fp(12), SingleMessage: fp(3)},
		},
		Last1Min: &dump1090.StatsWindow{Messages: fp(3400)},
		Total:    &dump1090.StatsWindow{Messages: fp(1234567)},
	}
	got := StatsTable(stats, Options{})
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")

	wantHeader := "METRIC                 latest  last1min  last5min  last15min    total"
	if lines[0] != wantHeader {
		t.Fatalf("header:\n got %q\nwant %q", lines[0], wantHeader)
	}
	wantRows := []string{
		"messages                 5678      3400         -          -  1234567",
		"blocks_processed         1200         -         -          -        -",
		"blocks_dropped              7         -         -          -        -",
		"modes                    3456         -         -          -        -",
		"modeac                     12         -         -          -        -",
		"bad                         3         -         -          -        -",
		"unknown_icao               21         -         -          -        -",
		"signal                  -12.4         -         -          -        -",
		"peak_signal              -3.2         -         -          -        -",
		"strong_signals             42         -         -          -        -",
		"cpu.demod_ms           1234.5         -         -          -        -",
		"cpu.reader_ms           234.5         -         -          -        -",
		"cpu.background_ms        34.5         -         -          -        -",
		"cpr.global_ok              10         -         -          -        -",
		"cpr.global_bad              1         -         -          -        -",
		"cpr.global_skipped          2         -         -          -        -",
		"cpr.local_ok               20         -         -          -        -",
		"cpr.local_bad               0         -         -          -        -",
		"cpr.local_skipped           1         -         -          -        -",
		"tracks.all                 12         -         -          -        -",
		"tracks.single_message       3         -         -          -        -",
	}
	if len(lines) != 1+len(wantRows) {
		t.Fatalf("got %d lines, want %d", len(lines), 1+len(wantRows))
	}
	for i, want := range wantRows {
		if lines[i+1] != want {
			t.Errorf("row %d:\n got %q\nwant %q", i, lines[i+1], want)
		}
	}
}

// TestStatsTableEmptyAndPartial: a document without windows renders
// nothing (the command prints a message instead), and missing
// sub-objects become dashes rather than panics.
func TestStatsTableEmptyAndPartial(t *testing.T) {
	if got := StatsTable(dump1090.Stats{}, Options{}); got != "" {
		t.Fatalf("StatsTable(zero) = %q, want empty", got)
	}
	partial := dump1090.Stats{Last5Min: &dump1090.StatsWindow{}}
	got := StatsTable(partial, Options{})
	if !strings.Contains(got, "messages") {
		t.Fatalf("partial stats should still render rows:\n%s", got)
	}
	for _, line := range strings.Split(strings.TrimRight(got, "\n"), "\n")[1:] {
		if !strings.HasSuffix(line, "-") && !strings.Contains(line, "  -") {
			t.Errorf("row with no data should end in dashes: %q", line)
		}
	}
}

func TestSquawksTableGolden(t *testing.T) {
	groups := []query.SquawkGroup{
		{Squawk: "1200", Hexes: []string{"aaaaaa", "bbbbbb", "cccccc"}, Count: 3},
		{Squawk: "7700", Hexes: []string{"dddddd"}, Count: 1},
	}
	got := SquawksTable(groups, 5, 4, Options{})
	want := "SQUAWK  COUNT  AIRCRAFT\n" +
		"1200        3  aaaaaa, bbbbbb, cccccc\n" +
		"7700        1  dddddd\n" +
		"5 aircraft, 4 with squawk, 2 distinct squawks\n"
	if got != want {
		t.Fatalf("SquawksTable:\n got %q\nwant %q", got, want)
	}

	t.Run("no groups", func(t *testing.T) {
		got := SquawksTable(nil, 3, 0, Options{})
		if got != "no aircraft reporting a squawk\n" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("emergency squawk is red", func(t *testing.T) {
		got := SquawksTable(groups, 5, 4, Options{Style: NewStyle(true)})
		if !strings.Contains(got, "\x1b[31m7700\x1b[0m") {
			t.Errorf("7700 not red: %q", got)
		}
		if strings.Contains(got, "\x1b[31m1200\x1b[0m") {
			t.Errorf("1200 must not be red: %q", got)
		}
	})
}

func TestEmergencyTableGolden(t *testing.T) {
	acs := []dump1090.Aircraft{
		mustAircraft(t, `{"hex":"3c673a","flight":"BAW117  ","squawk":"7700","emergency":"unlawful","alt_baro":34000,"gs":452,"dst":12.44}`),
		mustAircraft(t, `{"hex":"abc123","squawk":"1200","emergency":"lifeguard"}`),
	}
	got := EmergencyTable(acs, Options{})
	want := "HEX     CALLSIGN  SQUAWK  EMERGENCY  ALT(ft)  GS(kt)  DST(nm)\n" +
		"3c673a  BAW117      7700  unlawful     34000     452     12.4\n" +
		"abc123  -           1200  lifeguard        -       -        -\n"
	if got != want {
		t.Fatalf("EmergencyTable:\n got %q\nwant %q", got, want)
	}

	t.Run("colors", func(t *testing.T) {
		got := EmergencyTable(acs, Options{Style: NewStyle(true)})
		for _, want := range []string{"\x1b[31m7700\x1b[0m", "\x1b[31munlawful\x1b[0m", "\x1b[36m3c673a\x1b[0m", "\x1b[32m12.4\x1b[0m"} {
			if !strings.Contains(got, want) {
				t.Errorf("missing %q in %q", want, got)
			}
		}
	})
}

func TestUnitConversions(t *testing.T) {
	tests := []struct {
		name string
		m    bool // metric?
		fn   func(Options) string
		want string
	}{
		{name: "feet", fn: func(o Options) string { return o.formatAlt(34000) }, want: "34000"},
		{name: "metres", m: true, fn: func(o Options) string { return o.formatAlt(34000) }, want: "10363"},
		{name: "negative altitude metres", m: true, fn: func(o Options) string { return o.formatAlt(-1000) }, want: "-305"},
		{name: "knots", fn: func(o Options) string { return o.formatSpeed(452) }, want: "452"},
		{name: "km per hour", m: true, fn: func(o Options) string { return o.formatSpeed(452) }, want: "837"},
		{name: "nautical miles", fn: func(o Options) string { return o.formatDist(12.44) }, want: "12.4"},
		{name: "kilometres", m: true, fn: func(o Options) string { return o.formatDist(12.44) }, want: "23.0"},
		{name: "climb rate", fn: func(o Options) string { return o.formatRate(1200) }, want: "+1200 ft/min"},
		{name: "descent rate", fn: func(o Options) string { return o.formatRate(-640) }, want: "-640 ft/min"},
		{name: "climb rate metric", m: true, fn: func(o Options) string { return o.formatRate(1200) }, want: "+366 m/min"},
		{name: "descent rate metric", m: true, fn: func(o Options) string { return o.formatRate(-640) }, want: "-195 m/min"},
		{name: "alt header", fn: func(o Options) string { return o.altHeader() }, want: "ALT(ft)"},
		{name: "alt header metric", m: true, fn: func(o Options) string { return o.altHeader() }, want: "ALT(m)"},
		{name: "gs header metric", m: true, fn: func(o Options) string { return o.gsHeader() }, want: "GS(km/h)"},
		{name: "dst header metric", m: true, fn: func(o Options) string { return o.dstHeader() }, want: "DST(km)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fn(Options{Metric: tt.m}); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
