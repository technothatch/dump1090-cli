package dump1090

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newDocServer serves the given path→body routes and 404s everything else.
// A nil body means "respond 404", letting tests model decoders that do not
// implement a document.
func newDocServer(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for path, body := range routes {
		path, body := path, body
		if body == "" {
			continue
		}
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

const fixtureAircraft = `{"now":1759470000.5,"messages":123456,"aircraft":[
	{"hex":"3c673a","flight":"BAW117  ","alt_baro":34000},
	{"hex":"4ca1b2","alt_baro":"ground"}]}`

const fixtureReceiver = `{"version":"readsb 4.0.0","refresh":1,"lat":48.208,"lon":11.512}`

const fixtureStats = `{"latest":{"local":{"blocks_dropped":0},"messages":5678},"total":{"messages":1234567}}`

func TestFetchAircraftPrefersDataPath(t *testing.T) {
	srv := newDocServer(t, map[string]string{
		"/data/aircraft.json": fixtureAircraft,
		"/aircraft.json":      `{"now":1,"messages":1,"aircraft":[]}`,
		"/":                   `{"now":2,"messages":2,"aircraft":[]}`,
	})
	doc, err := NewClient(srv.URL, 2*time.Second).FetchAircraft(context.Background())
	if err != nil {
		t.Fatalf("FetchAircraft: %v", err)
	}
	if doc.Messages != 123456 {
		t.Fatalf("Messages = %d, want the /data/ document's 123456", doc.Messages)
	}
	if len(doc.Aircraft) != 2 {
		t.Fatalf("len(Aircraft) = %d, want 2", len(doc.Aircraft))
	}
}

func TestFetchAircraftPathFallback(t *testing.T) {
	tests := []struct {
		name         string
		routes       map[string]string
		wantMessages int64
	}{
		{
			name:         "readsb api style root only",
			routes:       map[string]string{"/": `{"now":3,"messages":33,"aircraft":[]}`},
			wantMessages: 33,
		},
		{
			name:         "bare aircraft.json only",
			routes:       map[string]string{"/aircraft.json": `{"now":4,"messages":44,"aircraft":[]}`},
			wantMessages: 44,
		},
		{
			name:         "data path preferred when several answer",
			routes:       map[string]string{"/": `{"now":5,"messages":55,"aircraft":[]}`, "/data/aircraft.json": `{"now":6,"messages":66,"aircraft":[]}`},
			wantMessages: 66,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newDocServer(t, tt.routes)
			doc, err := NewClient(srv.URL, 2*time.Second).FetchAircraft(context.Background())
			if err != nil {
				t.Fatalf("FetchAircraft: %v", err)
			}
			if doc.Messages != tt.wantMessages {
				t.Fatalf("Messages = %d, want %d", doc.Messages, tt.wantMessages)
			}
		})
	}
}

func TestFetchAircraftRejectsNonDocuments(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "html error page", body: "<html><body>proxy says no</body></html>", wantErr: "not the expected JSON document"},
		{name: "json but no aircraft key", body: `{"hello":"world"}`, wantErr: "not the expected JSON document"},
		{name: "truncated json", body: `{"aircraft":[`, wantErr: "not the expected JSON document"},
		{name: "json array", body: `[]`, wantErr: "not the expected JSON document"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			handler := func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			}
			mux.HandleFunc("/data/aircraft.json", handler)
			mux.HandleFunc("/aircraft.json", handler)
			mux.HandleFunc("/", handler)
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			_, err := NewClient(srv.URL, 2*time.Second).FetchAircraft(context.Background())
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tt.wantErr)
			}
			if !strings.Contains(err.Error(), "/data/aircraft.json") {
				t.Fatalf("error %q should name the attempted URLs", err)
			}
		})
	}
}

func TestFetchAircraftStatusErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   string
	}{
		{name: "not found", status: http.StatusNotFound, want: "404 Not Found"},
		{name: "server error", status: http.StatusInternalServerError, want: "500 Internal Server Error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}
			mux := http.NewServeMux()
			mux.HandleFunc("/", handler)
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			_, err := NewClient(srv.URL, 2*time.Second).FetchAircraft(context.Background())
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

// TestFetchAircraftTransportErrorAborts checks that a dead host fails fast
// with the URL in the message instead of walking the remaining paths.
func TestFetchAircraftTransportErrorAborts(t *testing.T) {
	srv := newDocServer(t, map[string]string{"/data/aircraft.json": fixtureAircraft})
	srv.Close()
	_, err := NewClient(srv.URL, 500*time.Millisecond).FetchAircraft(context.Background())
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "fetch") || !strings.Contains(err.Error(), srv.URL) {
		t.Fatalf("error %q should name the fetch failure and URL", err)
	}
	if strings.Contains(err.Error(), "tried") {
		t.Fatalf("transport errors should not be reported as path attempts: %v", err)
	}
}

func TestFetchReceiver(t *testing.T) {
	t.Run("full document", func(t *testing.T) {
		srv := newDocServer(t, map[string]string{"/data/receiver.json": fixtureReceiver})
		recv, err := NewClient(srv.URL, 2*time.Second).FetchReceiver(context.Background())
		if err != nil {
			t.Fatalf("FetchReceiver: %v", err)
		}
		if recv.Version != "readsb 4.0.0" || recv.Lat == nil || *recv.Lat != 48.208 || recv.Lon == nil || *recv.Lon != 11.512 {
			t.Fatalf("receiver = %+v", recv)
		}
	})

	t.Run("bare receiver.json fallback", func(t *testing.T) {
		srv := newDocServer(t, map[string]string{"/receiver.json": `{"version":"dump1090-fa 9.0"}`})
		recv, err := NewClient(srv.URL, 2*time.Second).FetchReceiver(context.Background())
		if err != nil {
			t.Fatalf("FetchReceiver: %v", err)
		}
		if recv.Version != "dump1090-fa 9.0" {
			t.Fatalf("Version = %q", recv.Version)
		}
	})

	t.Run("missing document is an error", func(t *testing.T) {
		srv := newDocServer(t, nil)
		if _, err := NewClient(srv.URL, 2*time.Second).FetchReceiver(context.Background()); err == nil {
			t.Fatal("want error, got nil")
		}
	})
}

func TestFetchStats(t *testing.T) {
	srv := newDocServer(t, map[string]string{"/data/stats.json": fixtureStats})
	sd, err := NewClient(srv.URL, 2*time.Second).FetchStats(context.Background())
	if err != nil {
		t.Fatalf("FetchStats: %v", err)
	}
	if sd.Stats.Latest == nil || sd.Stats.Latest.Messages == nil || *sd.Stats.Latest.Messages != 5678 {
		t.Fatalf("latest window = %+v", sd.Stats.Latest)
	}
	if sd.Stats.Total == nil || sd.Stats.Total.Messages == nil || *sd.Stats.Total.Messages != 1234567 {
		t.Fatalf("total window = %+v", sd.Stats.Total)
	}
	if sd.Stats.Latest.Local == nil || sd.Stats.Latest.Local.BlocksDropped == nil {
		t.Fatal("local counters not decoded")
	}
	// --json promises the decoder's original bytes.
	if string(sd.Raw) != fixtureStats {
		t.Fatalf("Raw = %s, want verbatim %s", sd.Raw, fixtureStats)
	}

	t.Run("missing windows stay nil", func(t *testing.T) {
		srv := newDocServer(t, map[string]string{"/data/stats.json": `{"total":{"messages":9}}`})
		sd, err := NewClient(srv.URL, 2*time.Second).FetchStats(context.Background())
		if err != nil {
			t.Fatalf("FetchStats: %v", err)
		}
		if sd.Stats.Latest != nil || sd.Stats.Last1Min != nil {
			t.Fatal("absent windows must be nil")
		}
		if sd.Stats.Total == nil {
			t.Fatal("total window missing")
		}
	})
}

// TestClientBaseURL checks normalisation: trailing slashes never leak into
// the request URLs.
func TestClientBaseURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "http://localhost:8080", want: "http://localhost:8080"},
		{in: "http://localhost:8080/", want: "http://localhost:8080"},
		{in: "  http://host:8042//  ", want: "http://host:8042"},
	}
	for _, tt := range tests {
		if got := NewClient(tt.in, time.Second).BaseURL(); got != tt.want {
			t.Errorf("NewClient(%q).BaseURL() = %q, want %q", tt.in, got, tt.want)
		}
	}
}
