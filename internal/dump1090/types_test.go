package dump1090

import (
	"encoding/json"
	"strings"
	"testing"
)

// decodeAircraft parses one aircraft.json element the way the client does.
func decodeAircraft(t *testing.T, raw string) Aircraft {
	t.Helper()
	var a Aircraft
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	return a
}

func TestAltFeet(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want float64
		ok   bool
	}{
		{name: "whole feet", raw: `{"hex":"abc123","alt_baro":34000}`, want: 34000, ok: true},
		{name: "fractional feet", raw: `{"hex":"abc123","alt_baro":1200.5}`, want: 1200.5, ok: true},
		{name: "zero", raw: `{"hex":"abc123","alt_baro":0}`, want: 0, ok: true},
		{name: "ground sentinel is 0 ft", raw: `{"hex":"abc123","alt_baro":"ground"}`, want: 0, ok: true},
		{name: "absent", raw: `{"hex":"abc123"}`, ok: false},
		{name: "null", raw: `{"hex":"abc123","alt_baro":null}`, ok: false},
		{name: "unknown string", raw: `{"hex":"abc123","alt_baro":"fl240"}`, ok: false},
		{name: "object", raw: `{"hex":"abc123","alt_baro":{"ft":34000}}`, ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := decodeAircraft(t, tt.raw)
			got, ok := a.AltFeet()
			if ok != tt.ok {
				t.Fatalf("AltFeet() ok = %v, want %v", ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Fatalf("AltFeet() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOnGround(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "ground string", raw: `{"hex":"abc123","alt_baro":"ground"}`, want: true},
		{name: "numeric altitude", raw: `{"hex":"abc123","alt_baro":1500}`, want: false},
		{name: "zero altitude is not ground", raw: `{"hex":"abc123","alt_baro":0}`, want: false},
		{name: "absent", raw: `{"hex":"abc123"}`, want: false},
		{name: "null", raw: `{"hex":"abc123","alt_baro":null}`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := decodeAircraft(t, tt.raw)
			if got := a.OnGround(); got != tt.want {
				t.Fatalf("OnGround() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasPosition(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "valid pair", raw: `{"hex":"abc123","lat":51.47,"lon":-0.45}`, want: true},
		{name: "antimeridian edges", raw: `{"hex":"abc123","lat":-90,"lon":180}`, want: true},
		{name: "no position", raw: `{"hex":"abc123"}`, want: false},
		{name: "lat only", raw: `{"hex":"abc123","lat":51.47}`, want: false},
		{name: "lat out of range", raw: `{"hex":"abc123","lat":91,"lon":0}`, want: false},
		{name: "lon out of range", raw: `{"hex":"abc123","lat":0,"lon":-181}`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := decodeAircraft(t, tt.raw)
			if got := a.HasPosition(); got != tt.want {
				t.Fatalf("HasPosition() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReceiverHasPosition(t *testing.T) {
	tests := []struct {
		name string
		json string
		want bool
	}{
		{name: "both coordinates", json: `{"version":"readsb 4.0.0","lat":48.208,"lon":11.512}`, want: true},
		{name: "no coordinates", json: `{"version":"readsb 4.0.0"}`, want: false},
		{name: "partial", json: `{"lat":48.208}`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r Receiver
			if err := json.Unmarshal([]byte(tt.json), &r); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := r.HasPosition(); got != tt.want {
				t.Fatalf("HasPosition() = %v, want %v", got, tt.want)
			}
		})
	}
	if (&Receiver{}).HasPosition() {
		t.Error("nil-position receiver should not report a position")
	}
	var nilRecv *Receiver
	if nilRecv.HasPosition() {
		t.Error("nil receiver should not report a position")
	}
}

// TestAircraftUnmarshalKeepsRaw checks the --json contract: the original
// element bytes survive decoding byte for byte, unknown keys included.
func TestAircraftUnmarshalKeepsRaw(t *testing.T) {
	raw := `{"hex":"abc123","flight":"TEST1  ","some_future_field":[1,2],"rssi":-9.5}`
	a := decodeAircraft(t, raw)
	if string(a.Raw) != raw {
		t.Fatalf("Raw = %s, want %s", a.Raw, raw)
	}
	if a.Hex != "abc123" || a.Flight != "TEST1  " || a.RSSI == nil || *a.RSSI != -9.5 {
		t.Fatalf("typed fields not decoded: %+v", a)
	}
	if !strings.Contains(string(a.Raw), "some_future_field") {
		t.Error("Raw dropped an unknown key")
	}

	t.Run("invalid json is an error", func(t *testing.T) {
		var a Aircraft
		if err := json.Unmarshal([]byte(`{"hex":`), &a); err == nil {
			t.Fatal("want error for truncated element")
		}
	})
	t.Run("hex may be missing without failing", func(t *testing.T) {
		a := decodeAircraft(t, `{"flight":"GHOST1"}`)
		if a.Hex != "" {
			t.Fatalf("Hex = %q, want empty", a.Hex)
		}
	})
}

func TestCallsignAndTypeName(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		callsign string
		typeName string
	}{
		{
			name:     "space padded flight and type key",
			raw:      `{"hex":"abc123","flight":"BAW117  ","type":"A320"}`,
			callsign: "BAW117",
			typeName: "A320",
		},
		{
			name:     "short t key fallback",
			raw:      `{"hex":"abc123","t":"B738"}`,
			callsign: "",
			typeName: "B738",
		},
		{
			name:     "type wins over t",
			raw:      `{"hex":"abc123","type":"A320","t":"A319"}`,
			callsign: "",
			typeName: "A320",
		},
		{
			name:     "nothing known",
			raw:      `{"hex":"abc123"}`,
			callsign: "",
			typeName: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := decodeAircraft(t, tt.raw)
			if got := a.Callsign(); got != tt.callsign {
				t.Errorf("Callsign() = %q, want %q", got, tt.callsign)
			}
			if got := a.TypeName(); got != tt.typeName {
				t.Errorf("TypeName() = %q, want %q", got, tt.typeName)
			}
		})
	}
}

// TestDocDecode covers the aircraft.json envelope, including the int64
// message counter and document order.
func TestDocDecode(t *testing.T) {
	const doc = `{"now":1759470000.5,"messages":1234567,"aircraft":[
		{"hex":"3c673a"},
		{"hex":"4ca1b2"},
		{"hex":"abc123"}]}`
	var d Doc
	if err := json.Unmarshal([]byte(doc), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.Now != 1759470000.5 {
		t.Errorf("Now = %v", d.Now)
	}
	if d.Messages != 1234567 {
		t.Errorf("Messages = %d, want 1234567", d.Messages)
	}
	if len(d.Aircraft) != 3 {
		t.Fatalf("len(Aircraft) = %d, want 3", len(d.Aircraft))
	}
	for i, want := range []string{"3c673a", "4ca1b2", "abc123"} {
		if d.Aircraft[i].Hex != want {
			t.Errorf("Aircraft[%d].Hex = %q, want %q", i, d.Aircraft[i].Hex, want)
		}
	}

	t.Run("empty document", func(t *testing.T) {
		var d Doc
		if err := json.Unmarshal([]byte(`{"now":1,"messages":0,"aircraft":[]}`), &d); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(d.Aircraft) != 0 {
			t.Fatalf("len(Aircraft) = %d, want 0", len(d.Aircraft))
		}
	})
}
