package query

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/technothatch/dump1090-cli/internal/dump1090"
)

// fp returns a pointer to v (the usual float helper).
func fp(v float64) *float64 { return &v }

// pos builds an airborne aircraft with a position.
func pos(hex string, lat, lon float64) dump1090.Aircraft {
	return dump1090.Aircraft{Hex: hex, Lat: fp(lat), Lon: fp(lon)}
}

// at builds an aircraft from a raw aircraft.json element.
func at(t *testing.T, raw string) dump1090.Aircraft {
	t.Helper()
	var a dump1090.Aircraft
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	return a
}

func TestNearestOrdering(t *testing.T) {
	acs := []dump1090.Aircraft{
		pos("bbbbbb", 0, 0.020), // ~1.2 nm
		pos("aaaaaa", 0, 0.005), // ~0.3 nm
		pos("cccccc", 0, 0.010), // ~0.6 nm
	}
	got := Nearest(acs, 0, 0, 0)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	wantOrder := []string{"aaaaaa", "cccccc", "bbbbbb"}
	for i, want := range wantOrder {
		if got[i].Aircraft.Hex != want {
			t.Fatalf("order[%d] = %s, want %s (dst %.3f)", i, got[i].Aircraft.Hex, want, got[i].DstNm)
		}
	}
	if got[0].DstNm >= got[1].DstNm || got[1].DstNm >= got[2].DstNm {
		t.Fatalf("distances not ascending: %.3f %.3f %.3f", got[0].DstNm, got[1].DstNm, got[2].DstNm)
	}
	// Bearing from the origin to a point due east is 90 degrees.
	if got[0].DirDeg < 89.9 || got[0].DirDeg > 90.1 {
		t.Fatalf("DirDeg = %.3f, want ~90", got[0].DirDeg)
	}
}

func TestNearestLimit(t *testing.T) {
	acs := []dump1090.Aircraft{
		pos("aaaaaa", 0, 0.03),
		pos("bbbbbb", 0, 0.02),
		pos("cccccc", 0, 0.01),
	}
	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{name: "positive limit truncates", limit: 2, want: 2},
		{name: "zero means unlimited", limit: 0, want: 3},
		{name: "negative means unlimited", limit: -1, want: 3},
		{name: "limit beyond the fleet", limit: 10, want: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Nearest(acs, 0, 0, tt.limit)
			if len(got) != tt.want {
				t.Fatalf("len = %d, want %d", len(got), tt.want)
			}
			if tt.limit > 0 && len(got) > 0 && got[0].Aircraft.Hex != "cccccc" {
				t.Fatalf("closest aircraft = %s, want cccccc", got[0].Aircraft.Hex)
			}
		})
	}
}

// TestNearestExcludesPositionless covers the "ground" case: an aircraft
// the decoder reports as on the ground typically has no lat/lon, and must
// not be ranked (nor treated as 0,0 in the Gulf of Guinea).
func TestNearestExcludesPositionless(t *testing.T) {
	acs := []dump1090.Aircraft{
		at(t, `{"hex":"4ca1b2","alt_baro":"ground"}`),
		at(t, `{"hex":"abc123"}`),
		{Hex: "badbad", Lat: fp(91), Lon: fp(0)}, // invalid latitude
		pos("3c673a", 48.2, 11.5),
	}
	got := Nearest(acs, 48.2, 11.5, 0)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (only the positioned aircraft)", len(got))
	}
	if got[0].Aircraft.Hex != "3c673a" {
		t.Fatalf("got %s, want 3c673a", got[0].Aircraft.Hex)
	}
	if got[0].DstNm != 0 {
		t.Fatalf("distance to itself = %v, want 0", got[0].DstNm)
	}
}

func TestNearestTieBreaksByHex(t *testing.T) {
	acs := []dump1090.Aircraft{
		pos("zz9999", 10, 10),
		pos("aa1111", 10, 10),
		pos("mm5555", 10, 10),
	}
	got := Nearest(acs, 10, 10, 0)
	want := []string{"aa1111", "mm5555", "zz9999"}
	for i, w := range want {
		if got[i].Aircraft.Hex != w {
			t.Fatalf("order[%d] = %s, want %s", i, got[i].Aircraft.Hex, w)
		}
	}
}

func TestBoxFilter(t *testing.T) {
	inside := pos("3c673a", 48.2, 11.5)
	outsideLat := pos("4ca1b2", 52.5, 13.4)
	outsideLon := pos("abc123", 48.2, 20.0)
	ground := at(t, `{"hex":"f00bar1","alt_baro":"ground"}`)
	acs := []dump1090.Aircraft{inside, outsideLat, outsideLon, ground}

	tests := []struct {
		name                   string
		lat1, lat2, lon1, lon2 float64
		want                   []string
	}{
		{name: "normal box", lat1: 47, lat2: 49, lon1: 10, lon2: 12, want: []string{"3c673a"}},
		{name: "swapped corners", lat1: 49, lat2: 47, lon1: 12, lon2: 10, want: []string{"3c673a"}},
		{name: "wide box keeps order", lat1: 40, lat2: 55, lon1: 5, lon2: 25, want: []string{"3c673a", "4ca1b2", "abc123"}},
		{name: "empty box", lat1: -10, lat2: -5, lon1: -10, lon2: -5, want: nil},
		{name: "ground aircraft never matches", lat1: 45, lat2: 50, lon1: 8, lon2: 14, want: []string{"3c673a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Box(acs, tt.lat1, tt.lat2, tt.lon1, tt.lon2)
			var hexes []string
			for _, a := range got {
				hexes = append(hexes, a.Hex)
			}
			if strings.Join(hexes, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("Box = %v, want %v", hexes, tt.want)
			}
		})
	}
}

func TestBoxAntimeridian(t *testing.T) {
	acs := []dump1090.Aircraft{
		pos("east11", 48, 175),
		pos("west22", 48, -175),
		pos("mid000", 48, 0),
	}
	got := Box(acs, 45, 50, 170, -170)
	var hexes []string
	for _, a := range got {
		hexes = append(hexes, a.Hex)
	}
	if strings.Join(hexes, ",") != "east11,west22" {
		t.Fatalf("Box across the antimeridian = %v, want [east11 west22]", hexes)
	}
}

func TestResolveDst(t *testing.T) {
	recv := &dump1090.Receiver{Lat: fp(48.208), Lon: fp(11.512)}
	tests := []struct {
		name    string
		ac      dump1090.Aircraft
		recv    *dump1090.Receiver
		wantOK  bool
		wantNm  float64
		wantDir float64
	}{
		{
			name:    "decoder dst and dir win",
			ac:      dump1090.Aircraft{Hex: "abc123", Dst: fp(12.4), Dir: fp(271)},
			recv:    recv,
			wantOK:  true,
			wantNm:  12.4,
			wantDir: 271,
		},
		{
			name:    "decoder dst without dir falls back to the receiver bearing",
			ac:      dump1090.Aircraft{Hex: "abc123", Dst: fp(12.4), Lat: fp(49.208), Lon: fp(11.512)},
			recv:    recv,
			wantOK:  true,
			wantNm:  12.4,
			wantDir: 0, // due north of the receiver
		},
		{
			name:    "decoder dst without dir or receiver leaves the bearing unknown",
			ac:      dump1090.Aircraft{Hex: "abc123", Dst: fp(12.4)},
			recv:    &dump1090.Receiver{},
			wantOK:  true,
			wantNm:  12.4,
			wantDir: 0,
		},
		{
			name:    "computed from the receiver position",
			ac:      dump1090.Aircraft{Hex: "abc123", Lat: fp(49.208), Lon: fp(11.512)},
			recv:    recv,
			wantOK:  true,
			wantNm:  60, // one degree of latitude
			wantDir: 0,
		},
		{
			name:   "no dst and no receiver position",
			ac:     dump1090.Aircraft{Hex: "abc123", Lat: fp(49.208), Lon: fp(11.512)},
			recv:   &dump1090.Receiver{},
			wantOK: false,
		},
		{
			name:   "nothing at all",
			ac:     dump1090.Aircraft{Hex: "abc123"},
			recv:   nil,
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nm, dir, ok := ResolveDst(&tt.ac, tt.recv)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if diff := nm - tt.wantNm; diff > 0.05 || diff < -0.05 {
				t.Errorf("nm = %.3f, want %.3f", nm, tt.wantNm)
			}
			if diff := dir - tt.wantDir; diff > 0.05 || diff < -0.05 {
				t.Errorf("dir = %.3f, want %.3f", dir, tt.wantDir)
			}
		})
	}

	t.Run("nil aircraft", func(t *testing.T) {
		if _, _, ok := ResolveDst(nil, recv); ok {
			t.Fatal("nil aircraft must not resolve")
		}
	})
}

func TestSortForDisplay(t *testing.T) {
	recv := &dump1090.Receiver{Lat: fp(0), Lon: fp(0)}

	t.Run("decoder dst field orders the fleet", func(t *testing.T) {
		acs := []dump1090.Aircraft{
			{Hex: "bbbbbb", Dst: fp(30), Dir: fp(90)},
			{Hex: "aaaaaa", Dst: fp(10), Dir: fp(180)},
			{Hex: "cccccc", Dst: fp(20), Dir: fp(270)},
		}
		SortForDisplay(acs, nil)
		if acs[0].Hex != "aaaaaa" || acs[1].Hex != "cccccc" || acs[2].Hex != "bbbbbb" {
			t.Fatalf("order = %s, %s, %s", acs[0].Hex, acs[1].Hex, acs[2].Hex)
		}
	})

	t.Run("distances computed from the receiver", func(t *testing.T) {
		acs := []dump1090.Aircraft{
			pos("far001", 2, 0),   // 120 nm
			pos("near01", 0.1, 0), // 6 nm
			pos("mid001", 1, 0),   // 60 nm
		}
		SortForDisplay(acs, recv)
		if acs[0].Hex != "near01" || acs[1].Hex != "mid001" || acs[2].Hex != "far001" {
			t.Fatalf("order = %s, %s, %s", acs[0].Hex, acs[1].Hex, acs[2].Hex)
		}
	})

	t.Run("unknown distances sort last, hex within them", func(t *testing.T) {
		acs := []dump1090.Aircraft{
			{Hex: "zzzzzz"},
			{Hex: "aaaaaa", Dst: fp(5), Dir: fp(0)},
			{Hex: "mmmmmm"},
		}
		SortForDisplay(acs, nil)
		if acs[0].Hex != "aaaaaa" || acs[1].Hex != "mmmmmm" || acs[2].Hex != "zzzzzz" {
			t.Fatalf("order = %s, %s, %s", acs[0].Hex, acs[1].Hex, acs[2].Hex)
		}
	})

	t.Run("equal distances tie-break by hex", func(t *testing.T) {
		acs := []dump1090.Aircraft{
			{Hex: "cccccc", Dst: fp(10), Dir: fp(0)},
			{Hex: "aaaaaa", Dst: fp(10), Dir: fp(0)},
			{Hex: "bbbbbb", Dst: fp(10), Dir: fp(0)},
		}
		SortForDisplay(acs, nil)
		if acs[0].Hex != "aaaaaa" || acs[1].Hex != "bbbbbb" || acs[2].Hex != "cccccc" {
			t.Fatalf("order = %s, %s, %s", acs[0].Hex, acs[1].Hex, acs[2].Hex)
		}
	})

	t.Run("empty fleet", func(t *testing.T) {
		acs := []dump1090.Aircraft{}
		SortForDisplay(acs, nil)
		if len(acs) != 0 {
			t.Fatalf("len = %d, want 0", len(acs))
		}
	})
}

func TestIsEmergencySquawk(t *testing.T) {
	tests := []struct {
		squawk string
		want   bool
	}{
		{squawk: "7500", want: true},
		{squawk: "7600", want: true},
		{squawk: "7700", want: true},
		{squawk: "1200", want: false},
		{squawk: "7701", want: false},
		{squawk: "", want: false},
		{squawk: "770", want: false},
	}
	for _, tt := range tests {
		if got := IsEmergencySquawk(tt.squawk); got != tt.want {
			t.Errorf("IsEmergencySquawk(%q) = %v, want %v", tt.squawk, got, tt.want)
		}
	}
}

func TestEmergencies(t *testing.T) {
	acs := []dump1090.Aircraft{
		at(t, `{"hex":"aaaaaa","squawk":"7500","emergency":"none"}`),
		at(t, `{"hex":"bbbbbb","squawk":"1200","emergency":"lifeguard"}`),
		at(t, `{"hex":"cccccc","squawk":"7700"}`),
		at(t, `{"hex":"dddddd","squawk":"1200","emergency":"none"}`),
		at(t, `{"hex":"eeeeee","squawk":"1000"}`),
		at(t, `{"hex":"ffffff","emergency":"unlawful"}`),
	}
	got := Emergencies(acs)
	var hexes []string
	for _, a := range got {
		hexes = append(hexes, a.Hex)
	}
	want := "aaaaaa,bbbbbb,cccccc,ffffff"
	if strings.Join(hexes, ",") != want {
		t.Fatalf("Emergencies = %v, want %s", hexes, want)
	}

	t.Run("no emergencies", func(t *testing.T) {
		if out := Emergencies([]dump1090.Aircraft{at(t, `{"hex":"dddddd","squawk":"1200","emergency":"none"}`)}); out != nil {
			t.Fatalf("out = %v, want nil", out)
		}
	})
}

func TestIsEmergency(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "emergency squawk only", raw: `{"hex":"abc123","squawk":"7600","emergency":"none"}`, want: true},
		{name: "classification only", raw: `{"hex":"abc123","emergency":"downed"}`, want: true},
		{name: "neither", raw: `{"hex":"abc123","squawk":"1200","emergency":"none"}`, want: false},
		{name: "emergency none string", raw: `{"hex":"abc123","emergency":"none"}`, want: false},
		{name: "empty fields", raw: `{"hex":"abc123"}`, want: false},
		{name: "case insensitive", raw: `{"hex":"abc123","emergency":"NONE"}`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := at(t, tt.raw)
			if got := IsEmergency(&a); got != tt.want {
				t.Fatalf("IsEmergency = %v, want %v", got, tt.want)
			}
		})
	}
	if IsEmergency(nil) {
		t.Error("nil aircraft must not be an emergency")
	}
}

func TestGroupSquawks(t *testing.T) {
	acs := []dump1090.Aircraft{
		at(t, `{"hex":"cccccc","squawk":"1200"}`),
		at(t, `{"hex":"aaaaaa","squawk":"1200"}`),
		at(t, `{"hex":"bbbbbb","squawk":"1200"}`),
		at(t, `{"hex":"eeeeee","squawk":"7700"}`),
		at(t, `{"hex":"dddddd","squawk":"2000"}`),
		at(t, `{"hex":"ffffff"}`), // no squawk: skipped
	}
	groups := GroupSquawks(acs)
	if len(groups) != 3 {
		t.Fatalf("len(groups) = %d, want 3", len(groups))
	}
	// Busiest squawk first, then by code.
	if groups[0].Squawk != "1200" || groups[0].Count != 3 {
		t.Fatalf("groups[0] = %+v, want 1200 x3", groups[0])
	}
	if groups[1].Squawk != "2000" || groups[1].Count != 1 {
		t.Fatalf("groups[1] = %+v, want 2000 x1", groups[1])
	}
	if groups[2].Squawk != "7700" || groups[2].Count != 1 {
		t.Fatalf("groups[2] = %+v, want 7700 x1", groups[2])
	}
	// Hexes inside a group are sorted.
	if strings.Join(groups[0].Hexes, ",") != "aaaaaa,bbbbbb,cccccc" {
		t.Fatalf("hexes = %v", groups[0].Hexes)
	}

	t.Run("no squawks at all", func(t *testing.T) {
		if got := GroupSquawks([]dump1090.Aircraft{{Hex: "abc123"}}); len(got) != 0 {
			t.Fatalf("got %v, want none", got)
		}
	})
}

func TestFindHex(t *testing.T) {
	acs := []dump1090.Aircraft{
		at(t, `{"hex":"3c673a","flight":"BAW117  "}`),
		at(t, `{"hex":"~4ca9f0"}`),
	}
	tests := []struct {
		name  string
		query string
		want  string
		ok    bool
	}{
		{name: "exact", query: "3c673a", want: "3c673a", ok: true},
		{name: "case insensitive", query: "3C673A", want: "3c673a", ok: true},
		{name: "padded input", query: " 3c673a ", want: "3c673a", ok: true},
		{name: "tilde address", query: "~4ca9f0", want: "~4ca9f0", ok: true},
		{name: "unknown address", query: "abc123", ok: false},
		{name: "empty fleet", query: "3c673a", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ledger := acs
			if tt.name == "empty fleet" {
				ledger = nil
			}
			a, ok := FindHex(ledger, tt.query)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && a.Hex != tt.want {
				t.Fatalf("Hex = %q, want %q", a.Hex, tt.want)
			}
		})
	}
}

func TestWithPosition(t *testing.T) {
	acs := []dump1090.Aircraft{
		pos("aaaaaa", 48.2, 11.5),
		at(t, `{"hex":"bbbbbb","alt_baro":"ground"}`),
		pos("cccccc", 51.5, -0.1),
	}
	got := WithPosition(acs)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Hex != "aaaaaa" || got[1].Hex != "cccccc" {
		t.Fatalf("order = %s, %s", got[0].Hex, got[1].Hex)
	}
}
