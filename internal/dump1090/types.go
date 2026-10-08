// Package dump1090 provides typed, tolerant access to the JSON documents
// published by dump1090-fa and readsb: aircraft.json, receiver.json and
// stats.json (see http://HOST/data/).
//
// Every field except hex is optional. Decoders omit keys they do not know,
// older versions emit fewer keys than newer ones, and both are fine: a
// document only fails to decode when it is not valid JSON at all. Unknown
// keys added by future decoder versions are ignored, and each aircraft
// element keeps its original bytes in Raw so --json can hand them back
// verbatim.
package dump1090

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Receiver mirrors receiver.json: http://HOST/data/receiver.json
type Receiver struct {
	// Version is the decoder version string, e.g. "readsb 4.0.0".
	Version string `json:"version"`
	// Refresh is the configured JSON write interval in seconds. It is nil
	// when the decoder did not report one.
	Refresh *float64 `json:"refresh"`
	// Lat and Lon are the receiver location. They are nil when the decoder
	// has no position configured (common for mobile setups).
	Lat *float64 `json:"lat"`
	Lon *float64 `json:"lon"`
}

// HasPosition reports whether the receiver has a usable lat/lon pair.
func (r *Receiver) HasPosition() bool {
	return r != nil && r.Lat != nil && r.Lon != nil
}

// Stats mirrors stats.json: http://HOST/data/stats.json
//
// The decoder keeps one window per interval; only Total is cumulative since
// the decoder started, the others are rolling windows.
type Stats struct {
	// Latest covers the most recent interval.
	Latest *StatsWindow `json:"latest"`
	// Last1Min covers the previous minute.
	Last1Min *StatsWindow `json:"last1min"`
	// Last5Min covers the previous five minutes.
	Last5Min *StatsWindow `json:"last5min"`
	// Last15Min covers the previous fifteen minutes.
	Last15Min *StatsWindow `json:"last15min"`
	// Total is cumulative since the decoder started.
	Total *StatsWindow `json:"total"`
}

// StatsWindow is one interval of decoder statistics. All sub-objects are
// optional and nil when the decoder did not report them.
type StatsWindow struct {
	// Local holds the RF/demodulator counters for this window.
	Local *LocalStats `json:"local"`
	// CPU holds CPU time consumed per pipeline stage, in milliseconds.
	CPU *CPUStats `json:"cpu"`
	// CPR holds compact-position-report decoding counters.
	CPR *CPRStats `json:"cpr"`
	// Tracks holds aircraft track counters.
	Tracks *TracksStats `json:"tracks"`
	// Messages is the number of messages attributed to this window.
	Messages *float64 `json:"messages"`
}

// LocalStats holds the local (demodulator) counters of one stats window.
type LocalStats struct {
	// BlocksProcessed is the number of sample blocks demodulated.
	BlocksProcessed *float64 `json:"blocks_processed"`
	// BlocksDropped is the number of sample blocks lost to overload.
	BlocksDropped *float64 `json:"blocks_dropped"`
	// ModeAC counts decoded Mode A/C messages.
	ModeAC *float64 `json:"modeac"`
	// Modes counts decoded Mode S messages.
	Modes *float64 `json:"modes"`
	// Bad counts messages that failed validation.
	Bad *float64 `json:"bad"`
	// UnknownICAO counts Mode S messages from addresses the decoder does
	// not recognise as aircraft.
	UnknownICAO *float64 `json:"unknown_icao"`
	// Accepted is the decoder's per-class accepted-message tally. Its
	// element type is decoder-specific, so it is kept opaque and never
	// rendered as a number.
	Accepted json.RawMessage `json:"accepted"`
	// Signal is the average signal level of the window, in dBFS.
	Signal *float64 `json:"signal"`
	// PeakSignal is the strongest signal seen in the window, in dBFS.
	PeakSignal *float64 `json:"peak_signal"`
	// StrongSignals counts messages above the strong-signal threshold.
	StrongSignals *float64 `json:"strong_signals"`
}

// CPUStats holds CPU time per pipeline stage, in milliseconds. Stages the
// decoder does not report are nil.
type CPUStats struct {
	// Demod is time spent in the demodulator.
	Demod *float64 `json:"demod"`
	// Reader is time spent reading samples.
	Reader *float64 `json:"reader"`
	// Background is time spent on background work.
	Background *float64 `json:"background"`
}

// CPRStats holds compact-position-report (CPR) decoding counters. A "local"
// CPR is decoded from aircraft visible to this receiver only; a "global" CPR
// needs two messages. Skipped counts CPR frames ignored because no new
// information could be derived.
type CPRStats struct {
	// GlobalOK counts successfully decoded global CPRs.
	GlobalOK *float64 `json:"global_ok"`
	// GlobalBad counts failed global CPR decodes.
	GlobalBad *float64 `json:"global_bad"`
	// GlobalSkipped counts ignored global CPR frames.
	GlobalSkipped *float64 `json:"global_skipped"`
	// LocalOK counts successfully decoded local CPRs.
	LocalOK *float64 `json:"local_ok"`
	// LocalBad counts failed local CPR decodes.
	LocalBad *float64 `json:"local_bad"`
	// LocalSkipped counts ignored local CPR frames.
	LocalSkipped *float64 `json:"local_skipped"`
}

// TracksStats holds aircraft track counters for one stats window.
type TracksStats struct {
	// All is the number of aircraft tracks maintained.
	All *float64 `json:"all"`
	// SingleMessage counts tracks that only ever produced one message.
	SingleMessage *float64 `json:"single_message"`
}

// Doc mirrors aircraft.json: http://HOST/data/aircraft.json
type Doc struct {
	// Now is the decoder clock, in seconds since the Unix epoch.
	Now float64 `json:"now"`
	// Messages is the cumulative message count since the decoder started.
	Messages int64 `json:"messages"`
	// Aircraft holds every currently tracked aircraft, in document order.
	Aircraft []Aircraft `json:"aircraft"`
}

// Aircraft mirrors one element of aircraft.json. Every field except Hex is
// optional: a message that carries no position has no lat/lon, a purely
// Mode-S message may have no altitude, and so on.
type Aircraft struct {
	// Hex is the ICAO 24-bit address in lowercase hex, e.g. "3c673a".
	// Non-ICAO addresses (TIS-B and friends) carry a '~' prefix.
	Hex string `json:"hex"`
	// Flight is the callsign as received, still space-padded; see
	// Callsign for the trimmed form.
	Flight string `json:"flight"`
	// Type is the aircraft type code from the decoder's database, e.g.
	// "A320"; T carries the same value under the shorter key some feeds
	// use. TypeName merges the two.
	Type string `json:"type"`
	// T is the aircraft type code under the short key (see Type).
	T string `json:"t"`
	// R is the registration, e.g. "G-EUUU".
	R string `json:"r"`
	// Category is the emitter category, e.g. "A3".
	Category string `json:"category"`
	// Squawk is the 4-digit Mode A code as a string, e.g. "7700".
	Squawk string `json:"squawk"`
	// Emergency is the decoder's emergency classification ("none" when
	// there is none; otherwise e.g. "lifeguard", "unlawful").
	Emergency string `json:"emergency"`
	// AltBaro is the barometric altitude in feet or the literal string
	// "ground". It is raw JSON because of the two possible types; use
	// AltFeet or OnGround to read it.
	AltBaro json.RawMessage `json:"alt_baro"`
	// AltGeom is the geometric (GNSS) altitude in feet.
	AltGeom *float64 `json:"alt_geom"`
	// GS is ground speed in knots.
	GS *float64 `json:"gs"`
	// IAS and TAS are indicated and true airspeed in knots; Mach is the
	// Mach number.
	IAS  *float64 `json:"ias"`
	TAS  *float64 `json:"tas"`
	Mach *float64 `json:"mach"`
	// Track is the true track over ground in degrees.
	Track *float64 `json:"track"`
	// BaroRate and GeomRate are vertical rates in feet per minute.
	BaroRate *float64 `json:"baro_rate"`
	GeomRate *float64 `json:"geom_rate"`
	// Lat and Lon are the aircraft position in decimal degrees, nil when
	// unknown.
	Lat *float64 `json:"lat"`
	Lon *float64 `json:"lon"`
	// Dst is the distance from the receiver in nautical miles and Dir the
	// bearing in degrees, when the decoder computed a receiver position.
	Dst *float64 `json:"dst"`
	Dir *float64 `json:"dir"`
	// RSSI is the receive signal strength in dBFS (negative).
	RSSI *float64 `json:"rssi"`
	// Messages is the number of messages seen from this aircraft.
	Messages *int64 `json:"messages"`
	// Seen and SeenPos are the seconds since this aircraft last produced
	// any message, respectively any position update.
	Seen    *float64 `json:"seen"`
	SeenPos *float64 `json:"seen_pos"`
	// MLAT and TISB list the fields sourced from multilateration or TIS-B,
	// e.g. ["lat","lon"].
	MLAT []string `json:"mlat"`
	TISB []string `json:"tisb"`

	// Raw is the complete original JSON object: unknown keys included,
	// key order preserved. It is what --json hands to scripts.
	Raw json.RawMessage `json:"-"`
}

// wire mirrors Aircraft for encoding/json; it exists so UnmarshalJSON can
// decode the typed view and keep the raw view from the same bytes without
// recursing.
type wire Aircraft

// UnmarshalJSON decodes one aircraft.json element into both the typed
// fields and the Raw passthrough bytes. Unknown keys are tolerated (and
// kept in Raw). A missing or odd hex is not an error here: a listing tool
// should show what the decoder reported rather than refuse the document.
func (a *Aircraft) UnmarshalJSON(b []byte) error {
	var w wire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*a = Aircraft(w)
	a.Raw = append(json.RawMessage(nil), b...)
	return nil
}

// AltFeet decodes AltBaro. The second return value is false when the field
// is absent or not understood; a value of "ground" decodes to 0 feet with
// true, because the aircraft really is at field elevation.
func (a *Aircraft) AltFeet() (float64, bool) {
	raw := bytes.TrimSpace(a.AltBaro)
	// A JSON null unmarshals into a float without error but means "no
	// altitude was reported" — reject it explicitly.
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0, false
	}
	var feet float64
	if err := json.Unmarshal(raw, &feet); err == nil {
		return feet, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s == "ground" {
		return 0, true
	}
	return 0, false
}

// OnGround reports whether the decoder says the aircraft is on the ground
// (alt_baro "ground"). There is no other in-band signal in aircraft.json.
func (a *Aircraft) OnGround() bool {
	if len(a.AltBaro) == 0 || bytes.Equal(bytes.TrimSpace(a.AltBaro), []byte("null")) {
		return false
	}
	var s string
	return json.Unmarshal(a.AltBaro, &s) == nil && s == "ground"
}

// HasPosition reports whether the aircraft carries a usable lat/lon pair.
func (a *Aircraft) HasPosition() bool {
	return a != nil && a.Lat != nil && a.Lon != nil &&
		*a.Lat >= -90 && *a.Lat <= 90 && *a.Lon >= -180 && *a.Lon <= 180
}

// Callsign returns the trimmed callsign (aircraft.json pads flight to
// eight characters), or "" when none was received.
func (a *Aircraft) Callsign() string {
	return strings.TrimSpace(a.Flight)
}

// TypeName returns the aircraft type code: the "type" key, falling back to
// the short "t" key some feeds use, or "" when the decoder has no
// database entry.
func (a *Aircraft) TypeName() string {
	if a.Type != "" {
		return a.Type
	}
	return a.T
}
