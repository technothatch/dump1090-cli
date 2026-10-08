package query

import (
	"sort"
	"strings"

	"github.com/technothatch/dump1090-cli/internal/dump1090"
)

// emergencySquawks are the three international emergency Mode A codes.
var emergencySquawks = map[string]bool{
	"7500": true, // unlawful interference
	"7600": true, // radio failure
	"7700": true, // emergency
}

// IsEmergencySquawk reports whether s is 7500, 7600 or 7700.
func IsEmergencySquawk(s string) bool {
	return emergencySquawks[s]
}

// IsEmergency reports whether an aircraft should show up in the emergency
// view: a non-"none" emergency classification from the decoder, or any of
// the three emergency squawks. Decoders often broadcast the squawk without
// setting the emergency field, so both signals count.
func IsEmergency(a *dump1090.Aircraft) bool {
	if a == nil {
		return false
	}
	if a.Squawk != "" && IsEmergencySquawk(a.Squawk) {
		return true
	}
	e := strings.ToLower(strings.TrimSpace(a.Emergency))
	return e != "" && e != "none"
}

// Emergencies returns the aircraft in emergency, preserving input order
// (callers apply the display sort afterwards).
func Emergencies(acs []dump1090.Aircraft) []dump1090.Aircraft {
	var out []dump1090.Aircraft
	for i := range acs {
		if IsEmergency(&acs[i]) {
			out = append(out, acs[i])
		}
	}
	return out
}

// WithPosition returns the aircraft that carry a usable lat/lon pair, in
// input order. Aircraft on the ground without a position (alt_baro
// "ground") never appear here.
func WithPosition(acs []dump1090.Aircraft) []dump1090.Aircraft {
	var out []dump1090.Aircraft
	for i := range acs {
		if acs[i].HasPosition() {
			out = append(out, acs[i])
		}
	}
	return out
}

// Near is one result of Nearest: the aircraft plus its distance and
// bearing from the query point.
type Near struct {
	// Aircraft is the matched aircraft.
	Aircraft dump1090.Aircraft
	// DstNm is the great-circle distance from the query point, nautical
	// miles.
	DstNm float64
	// DirDeg is the initial bearing from the query point to the aircraft,
	// degrees true.
	DirDeg float64
}

// Nearest ranks the aircraft that have a position by great-circle distance
// from (lat, lon): closest first, ties broken by hex so the order is
// stable. A positive limit keeps only the first n results; 0 or less means
// unlimited.
func Nearest(acs []dump1090.Aircraft, lat, lon float64, limit int) []Near {
	var out []Near
	for i := range acs {
		if !acs[i].HasPosition() {
			continue
		}
		out = append(out, Near{
			Aircraft: acs[i],
			DstNm:    HaversineNm(lat, lon, *acs[i].Lat, *acs[i].Lon),
			DirDeg:   BearingDeg(lat, lon, *acs[i].Lat, *acs[i].Lon),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DstNm != out[j].DstNm {
			return out[i].DstNm < out[j].DstNm
		}
		return out[i].Aircraft.Hex < out[j].Aircraft.Hex
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Box returns the aircraft inside the bounding box, preserving input
// order. Corner order does not matter and a box crossing the antimeridian
// (lon1 > lon2) wraps; aircraft without a position never match.
func Box(acs []dump1090.Aircraft, lat1, lat2, lon1, lon2 float64) []dump1090.Aircraft {
	var out []dump1090.Aircraft
	for i := range acs {
		a := &acs[i]
		if !a.HasPosition() {
			continue
		}
		if InBox(*a.Lat, *a.Lon, lat1, lat2, lon1, lon2) {
			out = append(out, *a)
		}
	}
	return out
}

// ResolveDst returns an aircraft's distance and bearing from the receiver
// for display. The decoder-computed dst/dir fields win when present (they
// are what the receiver itself measured); a missing bearing is filled in
// from receiver.json's position, and with no dst field at all both values
// are computed there. The last return value is false when neither source
// can place the aircraft.
func ResolveDst(a *dump1090.Aircraft, recv *dump1090.Receiver) (nm, dir float64, ok bool) {
	if a == nil {
		return 0, 0, false
	}
	if a.Dst != nil {
		nm = *a.Dst
		if a.Dir != nil {
			return nm, *a.Dir, true
		}
		if recv.HasPosition() && a.HasPosition() {
			return nm, BearingDeg(*recv.Lat, *recv.Lon, *a.Lat, *a.Lon), true
		}
		return nm, 0, true
	}
	if recv.HasPosition() && a.HasPosition() {
		return HaversineNm(*recv.Lat, *recv.Lon, *a.Lat, *a.Lon),
			BearingDeg(*recv.Lat, *recv.Lon, *a.Lat, *a.Lon),
			true
	}
	return 0, 0, false
}

// SortForDisplay orders aircraft the way the tables show them: closest to
// the receiver first, aircraft whose distance is unknown last, hex as the
// tie-breaker. The slice is reordered in place.
func SortForDisplay(acs []dump1090.Aircraft, recv *dump1090.Receiver) {
	type key struct {
		idx    int
		dst    float64
		hasDst bool
	}
	keys := make([]key, len(acs))
	for i := range acs {
		nm, _, ok := ResolveDst(&acs[i], recv)
		keys[i] = key{idx: i, dst: nm, hasDst: ok}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.hasDst != b.hasDst {
			return a.hasDst
		}
		if a.hasDst && a.dst != b.dst {
			return a.dst < b.dst
		}
		return acs[a.idx].Hex < acs[b.idx].Hex
	})
	sorted := make([]dump1090.Aircraft, len(acs))
	for i, k := range keys {
		sorted[i] = acs[k.idx]
	}
	copy(acs, sorted)
}

// SquawkGroup is one Mode A code and the aircraft broadcasting it.
type SquawkGroup struct {
	// Squawk is the 4-digit Mode A code.
	Squawk string
	// Hexes are the addresses of the aircraft on this squawk, sorted.
	Hexes []string
	// Count is len(Hexes).
	Count int
}

// GroupSquawks buckets aircraft by Mode A code, skipping aircraft without
// a squawk. Groups are ordered by descending count (busiest squawk first),
// then by squawk code; the hexes inside each group are sorted.
func GroupSquawks(acs []dump1090.Aircraft) []SquawkGroup {
	bySquawk := make(map[string][]string)
	for i := range acs {
		s := acs[i].Squawk
		if s == "" {
			continue
		}
		bySquawk[s] = append(bySquawk[s], acs[i].Hex)
	}
	out := make([]SquawkGroup, 0, len(bySquawk))
	for s, hexes := range bySquawk {
		sort.Strings(hexes)
		out = append(out, SquawkGroup{Squawk: s, Hexes: hexes, Count: len(hexes)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Squawk < out[j].Squawk
	})
	return out
}

// FindHex returns the aircraft with the given ICAO address. Matching is
// case-insensitive and covers non-ICAO '~' addresses; ok is false when no
// aircraft matches.
func FindHex(acs []dump1090.Aircraft, hex string) (*dump1090.Aircraft, bool) {
	want := strings.ToLower(strings.TrimSpace(hex))
	for i := range acs {
		if strings.EqualFold(acs[i].Hex, want) {
			return &acs[i], true
		}
	}
	return nil, false
}
