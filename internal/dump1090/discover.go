package dump1090

import (
	"context"
	"strings"
	"time"
)

// DefaultBases are the decoder origins auto-discovery probes, in order:
// dump1090-fa and readsb both default to HTTP port 8080, tar1090's
// alternative setup often uses 8081, and the readsb API port (--net-api-port)
// answers on 8042.
var DefaultBases = []string{
	"http://localhost:8080",
	"http://localhost:8081",
	"http://localhost:8042",
}

// envURL is the environment variable consulted after --url.
const envURL = "DUMP1090_URL"

// probeTimeout bounds one auto-discovery probe so a hung port cannot stall
// the CLI; refused connections fail far sooner anyway.
const probeTimeout = time.Second

// Prober reports whether a decoder answers at baseURL. Tests inject their
// own prober to exercise the discovery order without touching the network.
type Prober func(ctx context.Context, baseURL string) bool

// Probe checks whether baseURL serves a usable aircraft document, trying
// every known aircraft path (/data/aircraft.json first, the readsb API root
// last).
func Probe(ctx context.Context, baseURL string) bool {
	c := NewClient(baseURL, probeTimeout)
	_, err := c.FetchAircraft(ctx)
	return err == nil
}

// EnvLookup reads one environment variable. Tests inject their own to keep
// discovery independent of the calling shell.
type EnvLookup func(key string) string

// Resolve picks the decoder base URL: an explicit --url wins, then the
// DUMP1090_URL environment variable, then the first default base the prober
// confirms alive. When nothing answers, bases[0] is returned so the
// subsequent fetch error names a concrete URL. Probing is skipped entirely
// when the flag or the environment already decided.
func Resolve(ctx context.Context, flagURL string, getenv EnvLookup, prober Prober, bases []string) string {
	if v := strings.TrimSpace(flagURL); v != "" {
		return strings.TrimRight(v, "/")
	}
	if getenv != nil {
		if v := strings.TrimSpace(getenv(envURL)); v != "" {
			return strings.TrimRight(v, "/")
		}
	}
	if prober != nil {
		for _, base := range bases {
			if prober(ctx, base) {
				return base
			}
		}
	}
	if len(bases) > 0 {
		return bases[0]
	}
	return DefaultBases[0]
}

// WasAuto reports whether the base URL came from auto-discovery (neither
// --url nor DUMP1090_URL was set), which lets run() add a helpful hint when
// the decoder cannot be reached.
func WasAuto(flagURL string, getenv EnvLookup) bool {
	if strings.TrimSpace(flagURL) != "" {
		return false
	}
	if getenv != nil && strings.TrimSpace(getenv(envURL)) != "" {
		return false
	}
	return true
}
