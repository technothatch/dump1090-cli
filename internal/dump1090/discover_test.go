package dump1090

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// recordingProber is a Prober that answers yes only for the bases in
// alive and records every probe in order.
func recordingProber(alive map[string]bool, calls *[]string) Prober {
	return func(_ context.Context, base string) bool {
		*calls = append(*calls, base)
		return alive[base]
	}
}

func TestResolveFlagWins(t *testing.T) {
	calls := []string{}
	getenv := func(k string) string { return "http://env:9090/" }
	probe := recordingProber(map[string]bool{"http://flag:8080": true}, &calls)

	got := Resolve(context.Background(), "http://flag:8080//", getenv, probe, DefaultBases)
	if got != "http://flag:8080" {
		t.Fatalf("Resolve = %q, want %q", got, "http://flag:8080")
	}
	if len(calls) != 0 {
		t.Fatalf("flag set means no probing, but probed %v", calls)
	}
}

func TestResolveEnvironmentWinsOverProbing(t *testing.T) {
	calls := []string{}
	getenv := func(k string) string {
		if k == envURL {
			return "http://env:9090/"
		}
		return ""
	}
	probe := recordingProber(nil, &calls)

	got := Resolve(context.Background(), "", getenv, probe, DefaultBases)
	if got != "http://env:9090" {
		t.Fatalf("Resolve = %q, want %q", got, "http://env:9090")
	}
	if len(calls) != 0 {
		t.Fatalf("env set means no probing, but probed %v", calls)
	}
}

func TestResolveProbesDefaultsInOrder(t *testing.T) {
	tests := []struct {
		name      string
		alive     map[string]bool
		want      string
		wantCalls []string
	}{
		{
			name:      "first candidate answers",
			alive:     map[string]bool{"http://localhost:8080": true},
			want:      "http://localhost:8080",
			wantCalls: []string{"http://localhost:8080"},
		},
		{
			name: "8080 dead, 8081 alive",
			alive: map[string]bool{
				"http://localhost:8080": false,
				"http://localhost:8081": true,
			},
			want:      "http://localhost:8081",
			wantCalls: []string{"http://localhost:8080", "http://localhost:8081"},
		},
		{
			name: "only the readsb api port answers",
			alive: map[string]bool{
				"http://localhost:8080": false,
				"http://localhost:8081": false,
				"http://localhost:8042": true,
			},
			want:      "http://localhost:8042",
			wantCalls: []string{"http://localhost:8080", "http://localhost:8081", "http://localhost:8042"},
		},
		{
			name: "nothing answers falls back to the first default",
			alive: map[string]bool{
				"http://localhost:8080": false,
				"http://localhost:8081": false,
				"http://localhost:8042": false,
			},
			want:      "http://localhost:8080",
			wantCalls: []string{"http://localhost:8080", "http://localhost:8081", "http://localhost:8042"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := []string{}
			got := Resolve(context.Background(), "", func(string) string { return "" },
				recordingProber(tt.alive, &calls), DefaultBases)
			if got != tt.want {
				t.Fatalf("Resolve = %q, want %q", got, tt.want)
			}
			if !reflect.DeepEqual(calls, tt.wantCalls) {
				t.Fatalf("probe order = %v, want %v", calls, tt.wantCalls)
			}
		})
	}
}

func TestResolveWhitespaceFallsThrough(t *testing.T) {
	calls := []string{}
	getenv := func(string) string { return "   " }
	got := Resolve(context.Background(), "  ", getenv, recordingProber(
		map[string]bool{"http://localhost:8081": true}, &calls), DefaultBases)
	if got != "http://localhost:8081" {
		t.Fatalf("Resolve = %q, want 8081", got)
	}
	if len(calls) == 0 {
		t.Fatal("expected probing after blank flag and env")
	}
}

func TestResolveNilProber(t *testing.T) {
	got := Resolve(context.Background(), "", nil, nil, DefaultBases)
	if got != DefaultBases[0] {
		t.Fatalf("Resolve = %q, want %q", got, DefaultBases[0])
	}
}

func TestWasAuto(t *testing.T) {
	tests := []struct {
		name string
		flag string
		env  string
		want bool
	}{
		{name: "nothing set is auto", flag: "", env: "", want: true},
		{name: "flag disables auto", flag: "http://x:8080", env: "", want: false},
		{name: "env disables auto", flag: "", env: "http://y:8080", want: false},
		{name: "whitespace flag is still auto", flag: "  ", env: "", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WasAuto(tt.flag, func(string) string { return tt.env })
			if got != tt.want {
				t.Fatalf("WasAuto(%q, %q) = %v, want %v", tt.flag, tt.env, got, tt.want)
			}
		})
	}
}

// TestProbeAgainstServer exercises the real Prober against an httptest
// server: a decoder-style server is alive, an HTML impostor and a dead
// port are not.
func TestProbeAgainstServer(t *testing.T) {
	t.Run("decoder serving /data/aircraft.json", func(t *testing.T) {
		srv := newDocServer(t, map[string]string{"/data/aircraft.json": fixtureAircraft})
		if !Probe(context.Background(), srv.URL) {
			t.Fatal("Probe = false, want true")
		}
	})

	t.Run("readsb api style root", func(t *testing.T) {
		srv := newDocServer(t, map[string]string{"/": `{"now":1,"messages":1,"aircraft":[]}`})
		if !Probe(context.Background(), srv.URL) {
			t.Fatal("Probe = false, want true")
		}
	})

	t.Run("html impostor", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("<html>not a decoder</html>"))
		}))
		t.Cleanup(srv.Close)
		if Probe(context.Background(), srv.URL) {
			t.Fatal("Probe = true, want false")
		}
	})

	t.Run("dead port", func(t *testing.T) {
		srv := newDocServer(t, map[string]string{"/data/aircraft.json": fixtureAircraft})
		srv.Close()
		if Probe(context.Background(), srv.URL) {
			t.Fatal("Probe = true, want false")
		}
	})
}

// TestDefaultBasesShape pins the documented discovery order so a refactor
// cannot silently change the URLs users rely on.
func TestDefaultBasesShape(t *testing.T) {
	want := []string{"http://localhost:8080", "http://localhost:8081", "http://localhost:8042"}
	if !reflect.DeepEqual(DefaultBases, want) {
		t.Fatalf("DefaultBases = %v, want %v", DefaultBases, want)
	}
	for _, b := range DefaultBases {
		if !strings.HasPrefix(b, "http://localhost:") {
			t.Errorf("default base %q should be a localhost http URL", b)
		}
	}
}
