package render

import (
	"fmt"
	"strings"

	"github.com/technothatch/dump1090-cli/internal/dump1090"
	"github.com/technothatch/dump1090-cli/internal/query"
)

// Options control unit choice, styling and receiver-relative distances for
// every view. The zero value renders imperial units with no color.
type Options struct {
	// Metric switches altitude feet→metres, speed knots→km/h and distance
	// nautical miles→kilometres.
	Metric bool
	// Style carries the color decision (disabled for pipes and tests).
	Style Style
	// Receiver is the receiver.json position, used to resolve distances
	// the decoder did not compute itself. It may be nil.
	Receiver *dump1090.Receiver
}

// unknownCell is what every absent value renders as.
func unknownCell() Cell { return Cell{Text: "-", Color: ColorDim} }

// header makes a bold, right- or left-aligned column header.
func header(text string, align Align) Column {
	return Column{Header: text, Align: align, HeaderColor: ColorBold}
}

// --- unit helpers ---------------------------------------------------------

// Alt header plus value: feet or metres.
func (o Options) altHeader() string {
	if o.Metric {
		return "ALT(m)"
	}
	return "ALT(ft)"
}

func (o Options) gsHeader() string {
	if o.Metric {
		return "GS(km/h)"
	}
	return "GS(kt)"
}

func (o Options) dstHeader() string {
	if o.Metric {
		return "DST(km)"
	}
	return "DST(nm)"
}

func (o Options) altUnit() string {
	if o.Metric {
		return "m"
	}
	return "ft"
}

func (o Options) speedUnit() string {
	if o.Metric {
		return "km/h"
	}
	return "kt"
}

func (o Options) distUnit() string {
	if o.Metric {
		return "km"
	}
	return "nm"
}

// formatAlt renders an altitude in whole feet, or metres with --metric.
func (o Options) formatAlt(feet float64) string {
	if o.Metric {
		feet *= 0.3048
	}
	return fmt.Sprintf("%.0f", feet)
}

// formatSpeed renders a speed in whole knots, or km/h with --metric.
func (o Options) formatSpeed(kt float64) string {
	if o.Metric {
		kt *= 1.852
	}
	return fmt.Sprintf("%.0f", kt)
}

// formatDist renders a distance with one decimal, in nautical miles or
// kilometres with --metric.
func (o Options) formatDist(nm float64) string {
	if o.Metric {
		nm *= 1.852
	}
	return fmt.Sprintf("%.1f", nm)
}

// formatRate renders a vertical rate with an explicit sign, in ft/min or
// m/min with --metric.
func (o Options) formatRate(fpm float64) string {
	unit := "ft/min"
	if o.Metric {
		fpm *= 0.3048
		unit = "m/min"
	}
	sign := "+"
	if fpm < 0 {
		sign = "-"
		fpm = -fpm
	}
	return fmt.Sprintf("%s%.0f %s", sign, fpm, unit)
}

// --- aircraft table -------------------------------------------------------

// AircraftTable renders the shared aircraft listing (ls, watch, nearest,
// box and emergency all use these columns). Distances come from the
// decoder's dst field, falling back to the receiver position; callers sort
// the slice beforehand.
func AircraftTable(acs []dump1090.Aircraft, o Options) string {
	t := NewTable(
		header("HEX", AlignLeft),
		header("CALLSIGN", AlignLeft),
		header("TYPE", AlignLeft),
		header(o.altHeader(), AlignRight),
		header(o.gsHeader(), AlignRight),
		header("TRACK", AlignRight),
		header("SQUAWK", AlignRight),
		header(o.dstHeader(), AlignRight),
	)
	for i := range acs {
		t.Add(o.aircraftCells(&acs[i])...)
	}
	return t.Render(o.Style)
}

// aircraftCells builds the shared eight columns for one aircraft.
func (o Options) aircraftCells(a *dump1090.Aircraft) []Cell {
	callsign := Cell{Text: a.Callsign(), Color: ColorBold}
	if callsign.Text == "" {
		callsign = unknownCell()
	}
	kind := Cell{Text: a.TypeName()}
	if kind.Text == "" {
		kind = unknownCell()
	}
	gs := unknownCell()
	if a.GS != nil {
		gs = Cell{Text: o.formatSpeed(*a.GS)}
	}
	track := unknownCell()
	if a.Track != nil {
		track = Cell{Text: fmt.Sprintf("%.0f", *a.Track)}
	}
	squawk := unknownCell()
	if a.Squawk != "" {
		squawk = Cell{Text: a.Squawk}
		if query.IsEmergencySquawk(a.Squawk) {
			squawk.Color = ColorRed
		}
	}
	dst := unknownCell()
	if nm, _, ok := query.ResolveDst(a, o.Receiver); ok {
		dst = Cell{Text: o.formatDist(nm), Color: ColorGreen}
	}
	return []Cell{
		{Text: a.Hex, Color: ColorCyan},
		callsign,
		kind,
		o.altCell(a),
		gs,
		track,
		squawk,
		dst,
	}
}

// altCell renders alt_baro: the literal "ground" becomes GND (yellow), a
// number becomes feet or metres, anything else a dim dash.
func (o Options) altCell(a *dump1090.Aircraft) Cell {
	if a.OnGround() {
		return Cell{Text: "GND", Color: ColorYellow}
	}
	feet, ok := a.AltFeet()
	if !ok {
		return unknownCell()
	}
	return Cell{Text: o.formatAlt(feet)}
}

// --- info view ------------------------------------------------------------

// infoRow is one label/value line of the info view.
type infoRow struct {
	label string
	value string
	color Color
}

// Info renders the detail view for one aircraft: every field the decoder
// reported, one label/value per line, absent fields omitted entirely.
func Info(a *dump1090.Aircraft, o Options) string {
	var rows []infoRow
	add := func(label, value string, color Color) {
		if value != "" {
			rows = append(rows, infoRow{label: label, value: value, color: color})
		}
	}

	add("hex", a.Hex, ColorCyan)
	add("callsign", a.Callsign(), ColorBold)
	add("type", a.TypeName(), ColorNone)
	add("registration", a.R, ColorNone)
	add("category", a.Category, ColorNone)
	squawkColor := ColorNone
	if query.IsEmergencySquawk(a.Squawk) {
		squawkColor = ColorRed
	}
	add("squawk", a.Squawk, squawkColor)
	if em := strings.ToLower(strings.TrimSpace(a.Emergency)); em != "" && em != "none" {
		add("emergency", a.Emergency, ColorRed)
	}

	if a.OnGround() {
		add("altitude", "ground", ColorYellow)
	} else if feet, ok := a.AltFeet(); ok {
		add("altitude", o.formatAlt(feet)+" "+o.altUnit(), ColorNone)
	}
	if a.AltGeom != nil {
		add("geom altitude", o.formatAlt(*a.AltGeom)+" "+o.altUnit(), ColorNone)
	}
	if a.GS != nil {
		add("ground speed", o.formatSpeed(*a.GS)+" "+o.speedUnit(), ColorNone)
	}
	if a.IAS != nil {
		add("indicated airspeed", o.formatSpeed(*a.IAS)+" "+o.speedUnit(), ColorNone)
	}
	if a.TAS != nil {
		add("true airspeed", o.formatSpeed(*a.TAS)+" "+o.speedUnit(), ColorNone)
	}
	if a.Mach != nil {
		add("mach", fmt.Sprintf("%.3f", *a.Mach), ColorNone)
	}
	if a.Track != nil {
		add("track", fmt.Sprintf("%.1f deg", *a.Track), ColorNone)
	}
	if a.BaroRate != nil {
		add("baro rate", o.formatRate(*a.BaroRate), ColorNone)
	}
	if a.GeomRate != nil {
		add("geom rate", o.formatRate(*a.GeomRate), ColorNone)
	}
	if a.HasPosition() {
		add("position", fmt.Sprintf("%.4f, %.4f", *a.Lat, *a.Lon), ColorNone)
	}
	if nm, dir, ok := query.ResolveDst(a, o.Receiver); ok {
		add("dst", o.formatDist(nm)+" "+o.distUnit()+" from receiver", ColorGreen)
		if a.Dir != nil || (o.Receiver.HasPosition() && a.HasPosition()) {
			add("dir", fmt.Sprintf("%.0f deg from receiver", dir), ColorNone)
		}
	}
	if a.RSSI != nil {
		add("rssi", fmt.Sprintf("%.1f dBFS", *a.RSSI), ColorNone)
	}
	if a.Messages != nil {
		add("messages", fmt.Sprintf("%d", *a.Messages), ColorNone)
	}
	if a.Seen != nil {
		add("seen", fmt.Sprintf("%.1f s", *a.Seen), ColorNone)
	}
	if a.SeenPos != nil {
		add("seen pos", fmt.Sprintf("%.1f s", *a.SeenPos), ColorNone)
	}
	add("mlat", strings.Join(a.MLAT, ", "), ColorNone)
	add("tisb", strings.Join(a.TISB, ", "), ColorNone)

	width := 0
	for _, r := range rows {
		if len(r.label) > width {
			width = len(r.label)
		}
	}
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "%-*s %s\n", width, r.label, o.Style.Wrap(r.color, r.value))
	}
	return b.String()
}

// --- stats table ----------------------------------------------------------

// StatsTable renders stats.json as one row per metric and one column per
// time window. It returns "" when the document carried no window at all.
func StatsTable(s dump1090.Stats, o Options) string {
	windows := []struct {
		name string
		win  *dump1090.StatsWindow
	}{
		{"latest", s.Latest},
		{"last1min", s.Last1Min},
		{"last5min", s.Last5Min},
		{"last15min", s.Last15Min},
		{"total", s.Total},
	}
	if s.Latest == nil && s.Last1Min == nil && s.Last5Min == nil && s.Last15Min == nil && s.Total == nil {
		return ""
	}

	// Nested accessors that treat a missing sub-object as "no value"
	// instead of panicking.
	local := func(f func(*dump1090.LocalStats) *float64) func(*dump1090.StatsWindow) *float64 {
		return func(w *dump1090.StatsWindow) *float64 {
			if w == nil || w.Local == nil {
				return nil
			}
			return f(w.Local)
		}
	}
	cpu := func(f func(*dump1090.CPUStats) *float64) func(*dump1090.StatsWindow) *float64 {
		return func(w *dump1090.StatsWindow) *float64 {
			if w == nil || w.CPU == nil {
				return nil
			}
			return f(w.CPU)
		}
	}
	cpr := func(f func(*dump1090.CPRStats) *float64) func(*dump1090.StatsWindow) *float64 {
		return func(w *dump1090.StatsWindow) *float64 {
			if w == nil || w.CPR == nil {
				return nil
			}
			return f(w.CPR)
		}
	}
	tracks := func(f func(*dump1090.TracksStats) *float64) func(*dump1090.StatsWindow) *float64 {
		return func(w *dump1090.StatsWindow) *float64 {
			if w == nil || w.Tracks == nil {
				return nil
			}
			return f(w.Tracks)
		}
	}
	plain := func(f func(*dump1090.StatsWindow) *float64) func(*dump1090.StatsWindow) *float64 {
		return func(w *dump1090.StatsWindow) *float64 {
			if w == nil {
				return nil
			}
			return f(w)
		}
	}

	type rowDef struct {
		label string
		get   func(*dump1090.StatsWindow) *float64
		// decimals is the printf precision: counters are whole numbers,
		// signal levels and CPU milliseconds keep one digit.
		decimals int
	}
	rows := []rowDef{
		{"messages", plain(func(w *dump1090.StatsWindow) *float64 { return w.Messages }), 0},
		{"blocks_processed", local(func(l *dump1090.LocalStats) *float64 { return l.BlocksProcessed }), 0},
		{"blocks_dropped", local(func(l *dump1090.LocalStats) *float64 { return l.BlocksDropped }), 0},
		{"modes", local(func(l *dump1090.LocalStats) *float64 { return l.Modes }), 0},
		{"modeac", local(func(l *dump1090.LocalStats) *float64 { return l.ModeAC }), 0},
		{"bad", local(func(l *dump1090.LocalStats) *float64 { return l.Bad }), 0},
		{"unknown_icao", local(func(l *dump1090.LocalStats) *float64 { return l.UnknownICAO }), 0},
		{"signal", local(func(l *dump1090.LocalStats) *float64 { return l.Signal }), 1},
		{"peak_signal", local(func(l *dump1090.LocalStats) *float64 { return l.PeakSignal }), 1},
		{"strong_signals", local(func(l *dump1090.LocalStats) *float64 { return l.StrongSignals }), 0},
		{"cpu.demod_ms", cpu(func(c *dump1090.CPUStats) *float64 { return c.Demod }), 1},
		{"cpu.reader_ms", cpu(func(c *dump1090.CPUStats) *float64 { return c.Reader }), 1},
		{"cpu.background_ms", cpu(func(c *dump1090.CPUStats) *float64 { return c.Background }), 1},
		{"cpr.global_ok", cpr(func(c *dump1090.CPRStats) *float64 { return c.GlobalOK }), 0},
		{"cpr.global_bad", cpr(func(c *dump1090.CPRStats) *float64 { return c.GlobalBad }), 0},
		{"cpr.global_skipped", cpr(func(c *dump1090.CPRStats) *float64 { return c.GlobalSkipped }), 0},
		{"cpr.local_ok", cpr(func(c *dump1090.CPRStats) *float64 { return c.LocalOK }), 0},
		{"cpr.local_bad", cpr(func(c *dump1090.CPRStats) *float64 { return c.LocalBad }), 0},
		{"cpr.local_skipped", cpr(func(c *dump1090.CPRStats) *float64 { return c.LocalSkipped }), 0},
		{"tracks.all", tracks(func(t *dump1090.TracksStats) *float64 { return t.All }), 0},
		{"tracks.single_message", tracks(func(t *dump1090.TracksStats) *float64 { return t.SingleMessage }), 0},
	}

	t := NewTable(header("METRIC", AlignLeft))
	for _, w := range windows {
		t.columns = append(t.columns, header(w.name, AlignRight))
	}
	for _, r := range rows {
		cells := make([]Cell, 0, len(windows)+1)
		cells = append(cells, Cell{Text: r.label})
		for _, w := range windows {
			v := r.get(w.win)
			if v == nil {
				cells = append(cells, unknownCell())
				continue
			}
			cells = append(cells, Cell{Text: fmt.Sprintf("%.*f", r.decimals, *v)})
		}
		t.Add(cells...)
	}
	return t.Render(o.Style)
}

// --- squawk table ---------------------------------------------------------

// SquawksTable renders the squawk grouping plus a one-line summary.
// total is every tracked aircraft, withSquawk how many carried a code.
func SquawksTable(groups []query.SquawkGroup, total, withSquawk int, o Options) string {
	if len(groups) == 0 {
		return "no aircraft reporting a squawk\n"
	}
	t := NewTable(
		header("SQUAWK", AlignLeft),
		header("COUNT", AlignRight),
		header("AIRCRAFT", AlignLeft),
	)
	for _, g := range groups {
		code := Cell{Text: g.Squawk, Color: ColorRed}
		if !query.IsEmergencySquawk(g.Squawk) {
			code.Color = ColorNone
		}
		t.Add(code, Cell{Text: fmt.Sprintf("%d", g.Count)}, Cell{Text: strings.Join(g.Hexes, ", ")})
	}
	return t.Render(o.Style) +
		fmt.Sprintf("%d aircraft, %d with squawk, %d distinct squawks\n", total, withSquawk, len(groups))
}

// --- emergency table ------------------------------------------------------

// EmergencyTable renders the aircraft in emergency. Callers check for an
// empty slice first and print the friendly message instead.
func EmergencyTable(acs []dump1090.Aircraft, o Options) string {
	t := NewTable(
		header("HEX", AlignLeft),
		header("CALLSIGN", AlignLeft),
		header("SQUAWK", AlignRight),
		header("EMERGENCY", AlignLeft),
		header(o.altHeader(), AlignRight),
		header(o.gsHeader(), AlignRight),
		header(o.dstHeader(), AlignRight),
	)
	for i := range acs {
		a := &acs[i]
		squawk := Cell{Text: a.Squawk, Color: ColorRed}
		if a.Squawk == "" {
			squawk = unknownCell()
		} else if !query.IsEmergencySquawk(a.Squawk) {
			squawk.Color = ColorNone
		}
		emergency := Cell{Text: a.Emergency, Color: ColorRed}
		if em := strings.ToLower(strings.TrimSpace(a.Emergency)); em == "" || em == "none" {
			emergency = unknownCell()
		}
		gs := unknownCell()
		if a.GS != nil {
			gs = Cell{Text: o.formatSpeed(*a.GS)}
		}
		dst := unknownCell()
		if nm, _, ok := query.ResolveDst(a, o.Receiver); ok {
			dst = Cell{Text: o.formatDist(nm), Color: ColorGreen}
		}
		callsign := Cell{Text: a.Callsign(), Color: ColorBold}
		if callsign.Text == "" {
			callsign = unknownCell()
		}
		t.Add(
			Cell{Text: a.Hex, Color: ColorCyan},
			callsign,
			squawk,
			emergency,
			o.altCell(a),
			gs,
			dst,
		)
	}
	return t.Render(o.Style)
}
