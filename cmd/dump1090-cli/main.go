// Command dump1090-cli is a fast, pretty command-line viewer for the JSON
// documents served by dump1090-fa and readsb: an aircraft table (ls), a
// full-screen live view (watch), detail (info), spatial queries (nearest,
// box), decoder statistics (stats) and transponder views (squawks,
// emergency).
//
// Every command supports --json for scripting, picks its decoder URL by
// auto-discovery (flag, DUMP1090_URL, then localhost 8080/8081/8042), and
// only colours its output when stdout is a terminal and NO_COLOR is unset.
// The whole tool is standard library: no cobra, no tview.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/technothatch/dump1090-cli/internal/dump1090"
	"github.com/technothatch/dump1090-cli/internal/query"
	"github.com/technothatch/dump1090-cli/internal/render"
)

// Defaults for the duration flags and the nearest view.
const (
	defaultInterval     = time.Second
	defaultTimeout      = 5 * time.Second
	defaultNearestLimit = 10
)

// hexRe matches ICAO addresses, including non-ICAO '~' addresses.
var hexRe = regexp.MustCompile(`^(?:~)?[0-9a-f]{6}$`)

// Indirections over real time, the network and the terminal so tests can
// drive auto-discovery, watch frames and color decisions without a decoder
// or a console. Tests run sequentially and restore them.
var (
	probeFn   = dump1090.Probe
	basesFn   = func() []string { return dump1090.DefaultBases }
	nowFn     = time.Now
	outputEnv = render.DefaultEnv
)

// ANSI sequences for the watch view: hide/restore the cursor and repaint
// the whole frame.
const (
	ansiHideCursor   = "\x1b[?25l"
	ansiShowCursor   = "\x1b[?25h"
	ansiClearAndHome = "\x1b[2J\x1b[H"
)

// options is the fully resolved command line.
type options struct {
	url      string
	interval time.Duration
	timeout  time.Duration
	jsonOut  bool
	metric   bool
	limit    int // -1 means "not given"
	help     bool

	command string
	args    []string

	// Parsed positional coordinates.
	lat, lon   float64
	lat1, lat2 float64
	lon1, lon2 float64
	hex        string

	// auto records that neither --url nor DUMP1090_URL was set, which
	// enables the discovery hint on fetch errors.
	auto  bool
	bases []string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run is main's testable entry point. Exit codes: 0 success (including
// --help), 1 decoder failure or aircraft not found, 2 usage error.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	o, err := parseArgs(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(stderr, "dump1090-cli: %v\n", err)
		fmt.Fprintln(stderr, "try 'dump1090-cli --help'")
		return 2
	}
	if o.help {
		usage(stdout)
		return 0
	}
	if o.command == "" {
		usage(stderr)
		return 2
	}
	if err := o.resolveArgs(); err != nil {
		fmt.Fprintf(stderr, "dump1090-cli: %v\n", err)
		return 2
	}

	o.bases = basesFn()
	o.auto = dump1090.WasAuto(o.url, os.Getenv)
	base := dump1090.Resolve(ctx, o.url, os.Getenv, probeFn, o.bases)
	client := dump1090.NewClient(base, o.timeout)

	switch o.command {
	case "ls":
		return cmdList(ctx, client, &o, stdout, stderr)
	case "watch":
		return cmdWatch(ctx, client, &o, stdout, stderr)
	case "info":
		return cmdInfo(ctx, client, &o, stdout, stderr)
	case "nearest":
		return cmdNearest(ctx, client, &o, stdout, stderr)
	case "box":
		return cmdBox(ctx, client, &o, stdout, stderr)
	case "stats":
		return cmdStats(ctx, client, &o, stdout, stderr)
	case "squawks":
		return cmdSquawks(ctx, client, &o, stdout, stderr)
	case "emergency":
		return cmdEmergency(ctx, client, &o, stdout, stderr)
	default:
		// resolveArgs already rejected unknown commands.
		fmt.Fprintf(stderr, "dump1090-cli: unknown command %q\n", o.command)
		return 2
	}
}

// --- argument parsing -----------------------------------------------------

// splitArgs classifies argv into flag tokens (in order, with their values)
// and positional tokens, so flags may appear before or after the command
// and its arguments. Rules: '--' ends flag parsing; a known boolean flag
// stands alone; a known value flag swallows the next token unless it is
// itself a known flag (which means the value is missing); a '-'-prefixed
// token that parses as a number is a positional (box takes negative
// coordinates); anything else '-'-prefixed is an unknown flag error.
func splitArgs(args []string) (flags, pos []string, err error) {
	valueFlags := map[string]bool{"url": true, "interval": true, "timeout": true, "limit": true}
	boolFlags := map[string]bool{"json": true, "metric": true, "help": true, "h": true}
	known := func(tok string) bool {
		name := flagName(tok)
		return valueFlags[name] || boolFlags[name]
	}

	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' || a == "-" {
			pos = append(pos, a)
			continue
		}
		name := flagName(a)
		switch {
		case boolFlags[name]:
			flags = append(flags, a)
		case valueFlags[name]:
			flags = append(flags, a)
			if !strings.Contains(a, "=") {
				if i+1 >= len(args) || known(args[i+1]) {
					return nil, nil, fmt.Errorf("flag needs an argument: -%s", name)
				}
				i++
				flags = append(flags, args[i])
			}
		default:
			if _, cerr := strconv.ParseFloat(strings.TrimLeft(a, "-"), 64); cerr == nil {
				pos = append(pos, a)
				continue
			}
			return nil, nil, fmt.Errorf("unknown flag %q", a)
		}
	}
	return flags, pos, nil
}

// flagName strips leading dashes and any =value suffix from a flag token.
func flagName(tok string) string {
	name := strings.TrimLeft(tok, "-")
	if eq := strings.IndexByte(name, '='); eq >= 0 {
		name = name[:eq]
	}
	return name
}

// parseArgs splits argv, parses the flags (twice-worth: the same set is
// registered once, defaults keep whatever earlier flags set) and attaches
// the positionals.
func parseArgs(args []string, stderr io.Writer) (options, error) {
	o := options{interval: defaultInterval, timeout: defaultTimeout, limit: -1}
	flagTokens, pos, err := splitArgs(args)
	if err != nil {
		return o, err
	}
	fs := flag.NewFlagSet("dump1090-cli", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { usage(stderr) }
	fs.StringVar(&o.url, "url", o.url, "decoder base URL (default $DUMP1090_URL, else auto-discover)")
	fs.DurationVar(&o.interval, "interval", o.interval, "watch refresh interval")
	fs.DurationVar(&o.timeout, "timeout", o.timeout, "HTTP timeout per request")
	fs.BoolVar(&o.jsonOut, "json", o.jsonOut, "emit JSON instead of tables")
	fs.BoolVar(&o.metric, "metric", o.metric, "metric units (m, km/h, km)")
	fs.IntVar(&o.limit, "limit", o.limit, "maximum rows to print (0 = all)")
	fs.BoolVar(&o.help, "help", o.help, "show this help")
	fs.BoolVar(&o.help, "h", o.help, "show this help (shorthand)")
	if err := fs.Parse(flagTokens); err != nil {
		return o, err
	}
	if len(pos) > 0 {
		o.command = pos[0]
		o.args = pos[1:]
	}
	return o, nil
}

// resolveArgs validates global flags, the command name and the command's
// positional arguments, parsing coordinates into o on success.
func (o *options) resolveArgs() error {
	if o.interval <= 0 {
		return fmt.Errorf("--interval must be positive (got %s)", o.interval)
	}
	if o.timeout <= 0 {
		return fmt.Errorf("--timeout must be positive (got %s)", o.timeout)
	}
	if o.limit < -1 {
		return fmt.Errorf("--limit must be >= 0 (got %d)", o.limit)
	}
	switch o.command {
	case "ls", "watch", "stats", "squawks", "emergency":
		if len(o.args) != 0 {
			return fmt.Errorf("%s takes no arguments (got %q)", o.command, o.args[0])
		}
	case "info":
		if len(o.args) != 1 {
			return errors.New("usage: dump1090-cli info HEX")
		}
		hex := strings.ToLower(strings.TrimSpace(o.args[0]))
		if !hexRe.MatchString(hex) {
			return fmt.Errorf("invalid hex address %q (want 6 hex digits, optionally ~-prefixed)", o.args[0])
		}
		o.hex = hex
	case "nearest":
		if len(o.args) != 2 {
			return errors.New("usage: dump1090-cli nearest LAT LON")
		}
		lat, lon, err := parseLatLon(o.args[0], o.args[1])
		if err != nil {
			return err
		}
		o.lat, o.lon = lat, lon
	case "box":
		if len(o.args) != 4 {
			return errors.New("usage: dump1090-cli box LAT1 LAT2 LON1 LON2")
		}
		vals := make([]float64, 4)
		for i, a := range o.args {
			v, err := strconv.ParseFloat(a, 64)
			if err != nil {
				return fmt.Errorf("invalid coordinate %q", a)
			}
			vals[i] = v
		}
		if err := checkLat(vals[0]); err != nil {
			return fmt.Errorf("LAT1: %w", err)
		}
		if err := checkLat(vals[1]); err != nil {
			return fmt.Errorf("LAT2: %w", err)
		}
		if err := checkLon(vals[2]); err != nil {
			return fmt.Errorf("LON1: %w", err)
		}
		if err := checkLon(vals[3]); err != nil {
			return fmt.Errorf("LON2: %w", err)
		}
		o.lat1, o.lat2, o.lon1, o.lon2 = vals[0], vals[1], vals[2], vals[3]
	default:
		return fmt.Errorf("unknown command %q (try --help)", o.command)
	}
	return nil
}

// parseLatLon parses and range-checks one coordinate pair.
func parseLatLon(latStr, lonStr string) (float64, float64, error) {
	lat, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid latitude %q", latStr)
	}
	if err := checkLat(lat); err != nil {
		return 0, 0, fmt.Errorf("latitude %q: %w", latStr, err)
	}
	lon, err := strconv.ParseFloat(lonStr, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid longitude %q", lonStr)
	}
	if err := checkLon(lon); err != nil {
		return 0, 0, fmt.Errorf("longitude %q: %w", lonStr, err)
	}
	return lat, lon, nil
}

func checkLat(v float64) error {
	if v < -90 || v > 90 {
		return fmt.Errorf("must be between -90 and 90 (got %g)", v)
	}
	return nil
}

func checkLon(v float64) error {
	if v < -180 || v > 180 {
		return fmt.Errorf("must be between -180 and 180 (got %g)", v)
	}
	return nil
}

// --- output plumbing ------------------------------------------------------

// fetch loads aircraft.json and, best effort, receiver.json for distances.
// A decoder failure prints one diagnostic (plus the auto-discovery hint
// when no URL was configured) and yields exit code 1.
func fetch(ctx context.Context, client *dump1090.Client, o *options, stderr io.Writer) (dump1090.Doc, *dump1090.Receiver, int) {
	doc, err := client.FetchAircraft(ctx)
	if err != nil {
		if ctx.Err() == nil {
			fmt.Fprintf(stderr, "dump1090-cli: %s: %v\n", o.command, err)
			if o.auto {
				fmt.Fprintf(stderr, "dump1090-cli: auto-discovery checked %s\n", strings.Join(o.bases, ", "))
				fmt.Fprintln(stderr, "dump1090-cli: is dump1090/readsb running? Try --url http://HOST:8080")
			}
		}
		return doc, nil, 1
	}
	recv, err := client.FetchReceiver(ctx)
	if err != nil {
		// Distances then rely on the decoder's own dst field, if any.
		return doc, &dump1090.Receiver{}, 0
	}
	return doc, &recv, 0
}

// ropts assembles the render options for one command invocation.
func (o *options) ropts(stdout io.Writer, recv *dump1090.Receiver) render.Options {
	return render.Options{Metric: o.metric, Style: outputEnv.Style(stdout), Receiver: recv}
}

// limitRows applies --limit: o.limit < 0 means unlimited (nearest
// substitutes its own default first).
func (o *options) limitRows(acs []dump1090.Aircraft) []dump1090.Aircraft {
	if o.limit > 0 && len(acs) > o.limit {
		return acs[:o.limit]
	}
	return acs
}

// writeJSON emits v as indented JSON with a trailing newline.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// rawElement returns the aircraft's original JSON bytes, falling back to
// re-marshalling the typed view for aircraft built in code (tests).
func rawElement(a *dump1090.Aircraft) json.RawMessage {
	if len(a.Raw) > 0 {
		return a.Raw
	}
	b, err := json.Marshal(a)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}

// aircraftDoc is the aircraft.json-shaped document --json emits for ls,
// box and emergency: the decoder's own header fields plus the filtered
// aircraft, verbatim.
type aircraftDoc struct {
	Now      float64           `json:"now"`
	Messages int64             `json:"messages"`
	Aircraft []json.RawMessage `json:"aircraft"`
}

// writeAircraftDoc emits the filtered view as an aircraft.json document.
func writeAircraftDoc(w io.Writer, doc dump1090.Doc, acs []dump1090.Aircraft) error {
	out := aircraftDoc{
		Now:      doc.Now,
		Messages: doc.Messages,
		Aircraft: make([]json.RawMessage, 0, len(acs)),
	}
	for i := range acs {
		out.Aircraft = append(out.Aircraft, rawElement(&acs[i]))
	}
	return writeJSON(w, out)
}

// writeRawJSON emits the decoder's original bytes, adding a trailing
// newline when the document lacks one.
func writeRawJSON(w io.Writer, raw json.RawMessage) error {
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		_, err := io.WriteString(w, "\n")
		return err
	}
	return nil
}

// --- commands -------------------------------------------------------------

// cmdList prints the aircraft table, closest to the receiver first.
func cmdList(ctx context.Context, client *dump1090.Client, o *options, stdout, stderr io.Writer) int {
	doc, recv, code := fetch(ctx, client, o, stderr)
	if code != 0 {
		return code
	}
	acs := doc.Aircraft
	query.SortForDisplay(acs, recv)
	acs = o.limitRows(acs)
	if o.jsonOut {
		if err := writeAircraftDoc(stdout, doc, acs); err != nil {
			return writeErr(o, stderr, err)
		}
		return 0
	}
	if len(acs) == 0 {
		fmt.Fprintln(stdout, "no aircraft")
		return 0
	}
	fmt.Fprint(stdout, render.AircraftTable(acs, o.ropts(stdout, recv)))
	return 0
}

// cmdInfo prints every known field of one aircraft.
func cmdInfo(ctx context.Context, client *dump1090.Client, o *options, stdout, stderr io.Writer) int {
	doc, recv, code := fetch(ctx, client, o, stderr)
	if code != 0 {
		return code
	}
	ac, ok := query.FindHex(doc.Aircraft, o.hex)
	if !ok {
		fmt.Fprintf(stderr, "dump1090-cli: info: no aircraft with hex %q\n", o.hex)
		return 1
	}
	if o.jsonOut {
		if err := writeJSON(stdout, rawElement(ac)); err != nil {
			return writeErr(o, stderr, err)
		}
		return 0
	}
	fmt.Fprint(stdout, render.Info(ac, o.ropts(stdout, recv)))
	return 0
}

// nearDoc is the nearest view's JSON: an aircraft.json document plus the
// query point, with dst/dir rewritten to be relative to that point.
type nearDoc struct {
	Now      float64 `json:"now"`
	Messages int64   `json:"messages"`
	Point    struct {
		Lat float64 `json:"lat"`
		Lon float64 `json:"lon"`
	} `json:"point"`
	Aircraft []json.RawMessage `json:"aircraft"`
}

// cmdNearest prints the aircraft closest to a point, nearest first.
func cmdNearest(ctx context.Context, client *dump1090.Client, o *options, stdout, stderr io.Writer) int {
	doc, recv, code := fetch(ctx, client, o, stderr)
	if code != 0 {
		return code
	}
	limit := o.limit
	if limit < 0 {
		limit = defaultNearestLimit
	}
	near := query.Nearest(doc.Aircraft, o.lat, o.lon, limit)

	if o.jsonOut {
		out := nearDoc{Now: doc.Now, Messages: doc.Messages}
		out.Point.Lat, out.Point.Lon = o.lat, o.lon
		out.Aircraft = make([]json.RawMessage, 0, len(near))
		for i := range near {
			out.Aircraft = append(out.Aircraft, nearElement(&near[i]))
		}
		if err := writeJSON(stdout, out); err != nil {
			return writeErr(o, stderr, err)
		}
		return 0
	}
	if len(doc.Aircraft) == 0 {
		fmt.Fprintln(stdout, "no aircraft")
		return 0
	}
	if len(near) == 0 {
		fmt.Fprintln(stdout, "no aircraft with a known position")
		return 0
	}
	// Copies carry the query-relative distance so the table's DST column
	// ranks against the point the user typed, not the receiver.
	acs := make([]dump1090.Aircraft, len(near))
	for i := range near {
		ac := near[i].Aircraft
		dst, dir := near[i].DstNm, near[i].DirDeg
		ac.Dst, ac.Dir = &dst, &dir
		acs[i] = ac
	}
	fmt.Fprint(stdout, render.AircraftTable(acs, o.ropts(stdout, recv)))
	return 0
}

// nearElement merges the query-relative dst/dir into the aircraft's raw
// JSON for the nearest view.
func nearElement(n *query.Near) json.RawMessage {
	base := rawElement(&n.Aircraft)
	var m map[string]any
	if err := json.Unmarshal(base, &m); err != nil {
		return base
	}
	m["dst"] = n.DstNm
	m["dir"] = n.DirDeg
	out, err := json.Marshal(m)
	if err != nil {
		return base
	}
	return out
}

// cmdBox prints the aircraft inside a bounding box, closest first.
func cmdBox(ctx context.Context, client *dump1090.Client, o *options, stdout, stderr io.Writer) int {
	doc, recv, code := fetch(ctx, client, o, stderr)
	if code != 0 {
		return code
	}
	acs := query.Box(doc.Aircraft, o.lat1, o.lat2, o.lon1, o.lon2)
	query.SortForDisplay(acs, recv)
	acs = o.limitRows(acs)
	if o.jsonOut {
		if err := writeAircraftDoc(stdout, doc, acs); err != nil {
			return writeErr(o, stderr, err)
		}
		return 0
	}
	if len(acs) == 0 {
		fmt.Fprintln(stdout, "no aircraft in box")
		return 0
	}
	fmt.Fprint(stdout, render.AircraftTable(acs, o.ropts(stdout, recv)))
	return 0
}

// cmdStats prints the stats.json windows as a metric-by-window table, or
// the document verbatim with --json.
func cmdStats(ctx context.Context, client *dump1090.Client, o *options, stdout, stderr io.Writer) int {
	sd, err := client.FetchStats(ctx)
	if err != nil {
		if ctx.Err() == nil {
			fmt.Fprintf(stderr, "dump1090-cli: stats: %v\n", err)
			if o.auto {
				fmt.Fprintf(stderr, "dump1090-cli: auto-discovery checked %s\n", strings.Join(o.bases, ", "))
				fmt.Fprintln(stderr, "dump1090-cli: is dump1090/readsb running? Try --url http://HOST:8080")
			}
		}
		return 1
	}
	if o.jsonOut {
		if err := writeRawJSON(stdout, sd.Raw); err != nil {
			return writeErr(o, stderr, err)
		}
		return 0
	}
	table := render.StatsTable(sd.Stats, o.ropts(stdout, nil))
	if table == "" {
		fmt.Fprintln(stdout, "no stats available")
		return 0
	}
	fmt.Fprint(stdout, table)
	return 0
}

// squawksDoc is the squawks command's JSON shape.
type squawksDoc struct {
	Total      int           `json:"total"`
	WithSquawk int           `json:"with_squawk"`
	Squawks    []squawkEntry `json:"squawks"`
}

// squawkEntry is one Mode A code in the squawks JSON.
type squawkEntry struct {
	Squawk string   `json:"squawk"`
	Count  int      `json:"count"`
	Hexes  []string `json:"hexes"`
}

// cmdSquawks prints aircraft grouped by Mode A code.
func cmdSquawks(ctx context.Context, client *dump1090.Client, o *options, stdout, stderr io.Writer) int {
	doc, recv, code := fetch(ctx, client, o, stderr)
	if code != 0 {
		return code
	}
	groups := query.GroupSquawks(doc.Aircraft)
	with := 0
	for i := range doc.Aircraft {
		if doc.Aircraft[i].Squawk != "" {
			with++
		}
	}
	if o.jsonOut {
		out := squawksDoc{
			Total:      len(doc.Aircraft),
			WithSquawk: with,
			Squawks:    make([]squawkEntry, 0, len(groups)),
		}
		for _, g := range groups {
			out.Squawks = append(out.Squawks, squawkEntry{Squawk: g.Squawk, Count: g.Count, Hexes: g.Hexes})
		}
		if err := writeJSON(stdout, out); err != nil {
			return writeErr(o, stderr, err)
		}
		return 0
	}
	if len(doc.Aircraft) == 0 {
		fmt.Fprintln(stdout, "no aircraft")
		return 0
	}
	fmt.Fprint(stdout, render.SquawksTable(groups, len(doc.Aircraft), with, o.ropts(stdout, recv)))
	return 0
}

// cmdEmergency prints the aircraft declaring an emergency: squawk
// 7500/7600/7700 or a non-"none" emergency classification.
func cmdEmergency(ctx context.Context, client *dump1090.Client, o *options, stdout, stderr io.Writer) int {
	doc, recv, code := fetch(ctx, client, o, stderr)
	if code != 0 {
		return code
	}
	acs := query.Emergencies(doc.Aircraft)
	query.SortForDisplay(acs, recv)
	acs = o.limitRows(acs)
	if o.jsonOut {
		if err := writeAircraftDoc(stdout, doc, acs); err != nil {
			return writeErr(o, stderr, err)
		}
		return 0
	}
	if len(acs) == 0 {
		fmt.Fprintln(stdout, "no aircraft in emergency")
		return 0
	}
	fmt.Fprint(stdout, render.EmergencyTable(acs, o.ropts(stdout, recv)))
	return 0
}

// cmdWatch repaints the aircraft table every --interval until the context
// is cancelled (Ctrl-C). On a terminal the frame is painted in place with
// ANSI sequences; redirected output gets one frame after another instead.
func cmdWatch(ctx context.Context, client *dump1090.Client, o *options, stdout, stderr io.Writer) int {
	recv, err := client.FetchReceiver(ctx)
	if err != nil {
		recv = dump1090.Receiver{}
	}
	ansi := outputEnv.ANSI(stdout)
	first := true
	if ansi {
		if _, err := io.WriteString(stdout, ansiHideCursor); err != nil {
			return writeErr(o, stderr, err)
		}
		defer func() {
			_, _ = io.WriteString(stdout, ansiShowCursor+"\n")
		}()
	}
	ropts := o.ropts(stdout, &recv)

	// NDJSON for scripts: one compact document per refresh.
	var jsonEnc *json.Encoder
	if o.jsonOut {
		jsonEnc = json.NewEncoder(stdout)
	}
	ticker := time.NewTicker(o.interval)
	defer ticker.Stop()

	fetchAndPaint := func() error {
		doc, err := client.FetchAircraft(ctx)
		if err != nil {
			return err
		}
		acs := doc.Aircraft
		query.SortForDisplay(acs, &recv)
		acs = o.limitRows(acs)
		if o.jsonOut {
			out := aircraftDoc{Now: doc.Now, Messages: doc.Messages}
			out.Aircraft = make([]json.RawMessage, 0, len(acs))
			for i := range acs {
				out.Aircraft = append(out.Aircraft, rawElement(&acs[i]))
			}
			return jsonEnc.Encode(out)
		}
		var frame strings.Builder
		fmt.Fprintf(&frame, "%s  %d aircraft  %d messages  %s\n",
			nowFn().Format("15:04:05"), len(acs), doc.Messages, client.BaseURL())
		frame.WriteString(render.AircraftTable(acs, ropts))
		return writeFrame(stdout, frame.String(), ansi, &first)
	}

	for {
		if err := fetchAndPaint(); err != nil {
			if ctx.Err() != nil {
				return 0
			}
			fmt.Fprintf(stderr, "dump1090-cli: watch: %v\n", err)
		}
		select {
		case <-ctx.Done():
			return 0
		case <-ticker.C:
		}
	}
}

// writeFrame emits one watch frame: repainting the screen in place on a
// terminal, separating frames with a blank line when redirected.
func writeFrame(w io.Writer, frame string, ansi bool, first *bool) error {
	if ansi {
		if _, err := io.WriteString(w, ansiClearAndHome); err != nil {
			return err
		}
	} else if !*first {
		if _, err := io.WriteString(w, "\n"); err != nil {
			return err
		}
	}
	*first = false
	_, err := io.WriteString(w, frame)
	return err
}

// writeErr maps an output failure to the diagnostic + exit code 1 pair.
func writeErr(o *options, stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "dump1090-cli: %s: write output: %v\n", o.command, err)
	return 1
}

// --- usage ----------------------------------------------------------------

// usage prints the help text.
func usage(w io.Writer) {
	fmt.Fprint(w, `dump1090-cli - fast, pretty ADS-B tables for dump1090-fa and readsb

usage: dump1090-cli [flags] <command> [args]

Commands:
  ls                          list tracked aircraft as a table
  watch                       full-screen live view, sorted by distance
  info HEX                    show every known field for one aircraft
  nearest LAT LON             aircraft closest to a point, nearest first
  box LAT1 LAT2 LON1 LON2     aircraft inside a bounding box
  stats                       decoder statistics from stats.json
  squawks                     aircraft grouped by Mode A squawk
  emergency                   aircraft declaring an emergency

Flags:
  --url URL        decoder base URL (default $DUMP1090_URL, else auto-discover
                   http://localhost:8080, then :8081, then readsb API :8042)
  --interval DUR   watch refresh interval (default 1s)
  --timeout DUR    HTTP timeout per request (default 5s)
  --json           emit JSON instead of tables (every command)
  --metric         metres, km/h and kilometres instead of ft, kt, nm
  --limit N        maximum rows to print (0 = all; nearest defaults to 10)
  -h, --help       show this help

Exit codes: 0 success, 1 decoder error or aircraft not found, 2 usage error.
NO_COLOR (any value) disables colors; colors only appear on a terminal.
`)
}
