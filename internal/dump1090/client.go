package dump1090

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxJSONSize caps how much of a response body is read. aircraft.json on a
// busy receiver is a few hundred kilobytes; the cap protects against a
// broken or hostile server streaming forever.
const maxJSONSize = 32 << 20 // 32 MiB

// Document paths, in the order a Client tries them. dump1090-fa and readsb
// serve the documents under /data/ on their HTTP port (default 8080); the
// readsb API port (--net-api-port, default 8042) serves the aircraft list
// from the root instead, which is why "/" is the last candidate.
var (
	// AircraftPaths are tried in order until one answers with a JSON
	// object carrying an "aircraft" array.
	AircraftPaths = []string{"/data/aircraft.json", "/aircraft.json", "/"}
	// ReceiverPaths are tried in order for receiver.json.
	ReceiverPaths = []string{"/data/receiver.json", "/receiver.json"}
	// StatsPaths are tried in order for stats.json.
	StatsPaths = []string{"/data/stats.json", "/stats.json"}
)

// Client fetches the JSON documents of one decoder. It is safe for
// sequential use from a single goroutine; concurrent fetches should be
// serialised by the caller.
type Client struct {
	// baseURL is the decoder origin, e.g. "http://localhost:8080".
	baseURL string
	// http does the actual transfers.
	http *http.Client
}

// NewClient builds a Client for the decoder at baseURL (scheme://host[:port],
// no path) with the given request timeout. A zero timeout means no timeout.
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		http:    &http.Client{Timeout: timeout},
	}
}

// BaseURL returns the normalised origin the client talks to.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// FetchAircraft retrieves aircraft.json. The document is only returned when
// every byte decoded and the result carried an "aircraft" array, so an HTML
// error page from a reverse proxy can never be mistaken for decoder data.
func (c *Client) FetchAircraft(ctx context.Context) (Doc, error) {
	var doc Doc
	if _, err := c.getJSONAny(ctx, AircraftPaths, looksLikeAircraftDoc, &doc); err != nil {
		return Doc{}, err
	}
	return doc, nil
}

// FetchReceiver retrieves receiver.json. The receiver position only feeds
// distance fallbacks, so callers treat an error as "position unknown".
func (c *Client) FetchReceiver(ctx context.Context) (Receiver, error) {
	var recv Receiver
	if _, err := c.getJSONAny(ctx, ReceiverPaths, looksLikeJSONObject, &recv); err != nil {
		return Receiver{}, err
	}
	return recv, nil
}

// StatsDoc is one stats.json fetch: the decoded view for rendering plus the
// original bytes for --json.
type StatsDoc struct {
	// Stats is the decoded view.
	Stats Stats
	// Raw is the response body as the decoder sent it.
	Raw json.RawMessage
}

// FetchStats retrieves stats.json, keeping the original bytes for --json.
func (c *Client) FetchStats(ctx context.Context) (StatsDoc, error) {
	var sd StatsDoc
	raw, err := c.getJSONAny(ctx, StatsPaths, looksLikeJSONObject, &sd.Stats)
	if err != nil {
		return StatsDoc{}, err
	}
	sd.Raw = append(json.RawMessage(nil), raw...)
	return sd, nil
}

// getJSONAny tries each path in order and decodes the first response that
// is HTTP 200, passes validate and unmarshals into v. It returns the raw
// body it decoded. Transport errors abort immediately: if the host is down,
// the other paths on the same host are down too.
func (c *Client) getJSONAny(ctx context.Context, paths []string, validate func([]byte) bool, v any) ([]byte, error) {
	var attempts []string
	for _, p := range paths {
		url := c.baseURL + p
		body, status, err := c.get(ctx, url)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			attempts = append(attempts, fmt.Sprintf("%s: %s", url, statusText(status)))
			continue
		}
		if validate != nil && !validate(body) {
			attempts = append(attempts, fmt.Sprintf("%s: not the expected JSON document", url))
			continue
		}
		if err := json.Unmarshal(body, v); err != nil {
			attempts = append(attempts, fmt.Sprintf("%s: decode: %v", url, err))
			continue
		}
		return body, nil
	}
	return nil, fmt.Errorf("dump1090: no usable response (tried %s)", strings.Join(attempts, "; "))
}

// get fetches url and returns the body plus the HTTP status. Non-200
// responses still return their body bytes so callers can describe them.
func (c *Client) get(ctx context.Context, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("dump1090: build request for %s: %w", url, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "dump1090-cli/0.1")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("dump1090: fetch %s: %w", url, err)
	}
	defer func() {
		// The body is fully consumed or abandoned below; closing is what
		// returns the connection to the pool.
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONSize+1))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("dump1090: read %s: %w", url, err)
	}
	if len(body) > maxJSONSize {
		return nil, resp.StatusCode, fmt.Errorf("dump1090: read %s: body larger than %d bytes", url, maxJSONSize)
	}
	return body, resp.StatusCode, nil
}

// looksLikeAircraftDoc reports whether body is a JSON object carrying an
// "aircraft" array — the one shape every dump1090/readsb aircraft document
// has, on every port that serves one.
func looksLikeAircraftDoc(body []byte) bool {
	var probe struct {
		Aircraft *json.RawMessage `json:"aircraft"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}
	return probe.Aircraft != nil
}

// looksLikeJSONObject reports whether body is any JSON object. Used for
// receiver.json and stats.json, whose keys vary between decoder versions.
func looksLikeJSONObject(body []byte) bool {
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	return len(trimmed) > 0 && trimmed[0] == '{'
}

// statusText describes a non-200 status for error messages, collapsing the
// usually empty reason phrase.
func statusText(code int) string {
	text := http.StatusText(code)
	if text == "" {
		return fmt.Sprintf("HTTP %d", code)
	}
	return fmt.Sprintf("%d %s", code, text)
}
