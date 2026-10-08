package query

import (
	"math"
	"testing"
)

func TestHaversineNm(t *testing.T) {
	tests := []struct {
		name                   string
		lat1, lon1, lat2, lon2 float64
		want, tolerance        float64
	}{
		{name: "identical points", lat1: 48.208, lon1: 11.512, lat2: 48.208, lon2: 11.512, want: 0, tolerance: 1e-9},
		{name: "one degree of latitude is 60 nm", lat1: 0, lon1: 0, lat2: 1, lon2: 0, want: 60, tolerance: 0.05},
		{name: "one degree of longitude at 60 north is 30 nm", lat1: 60, lon1: 0, lat2: 60, lon2: 1, want: 30, tolerance: 0.05},
		{name: "quarter degree of latitude", lat1: 48, lon1: 11, lat2: 48.25, lon2: 11, want: 15, tolerance: 0.05},
		{name: "antipodal longitudes at the equator", lat1: 0, lon1: 0, lat2: 0, lon2: 180, want: math.Pi * 3440.065, tolerance: 1},
		{name: "short hop", lat1: 48.208, lon1: 11.512, lat2: 48.209, lon2: 11.512, want: 0.06, tolerance: 0.005},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HaversineNm(tt.lat1, tt.lon1, tt.lat2, tt.lon2)
			if math.Abs(got-tt.want) > tt.tolerance {
				t.Fatalf("HaversineNm = %.4f, want %.4f ± %.4f", got, tt.want, tt.tolerance)
			}
			// Distance must be symmetric.
			back := HaversineNm(tt.lat2, tt.lon2, tt.lat1, tt.lon1)
			if math.Abs(got-back) > 1e-9 {
				t.Errorf("not symmetric: %.9f vs %.9f", got, back)
			}
		})
	}
}

func TestBearingDeg(t *testing.T) {
	tests := []struct {
		name                   string
		lat1, lon1, lat2, lon2 float64
		want, tolerance        float64
	}{
		{name: "due north", lat1: 0, lon1: 0, lat2: 1, lon2: 0, want: 0, tolerance: 1e-6},
		{name: "due east", lat1: 0, lon1: 0, lat2: 0, lon2: 1, want: 90, tolerance: 1e-6},
		{name: "due south", lat1: 0, lon1: 0, lat2: -1, lon2: 0, want: 180, tolerance: 1e-6},
		{name: "due west", lat1: 0, lon1: 0, lat2: 0, lon2: -1, want: 270, tolerance: 1e-6},
		// A great-circle track is not a rhumb line: the initial bearing
		// to a 45-degree diagonal point drifts slightly off 315.
		{name: "north west diagonal", lat1: 0, lon1: 0, lat2: 1, lon2: -1, want: 315, tolerance: 0.01},
		{name: "coincident points report 0", lat1: 48.2, lon1: 11.5, lat2: 48.2, lon2: 11.5, want: 0, tolerance: 0},
		{name: "just west of north normalises into 359", lat1: 0, lon1: 0, lat2: 1, lon2: -0.001, want: 359.94, tolerance: 0.05},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BearingDeg(tt.lat1, tt.lon1, tt.lat2, tt.lon2)
			if math.Abs(got-tt.want) > tt.tolerance {
				t.Fatalf("BearingDeg = %.4f, want %.4f ± %.4f", got, tt.want, tt.tolerance)
			}
			if got < 0 || got >= 360 {
				t.Fatalf("bearing %.4f outside [0, 360)", got)
			}
		})
	}
}

func TestInBox(t *testing.T) {
	tests := []struct {
		name                   string
		lat, lon               float64
		lat1, lat2, lon1, lon2 float64
		want                   bool
	}{
		{name: "inside", lat: 48, lon: 11, lat1: 47, lat2: 49, lon1: 10, lon2: 12, want: true},
		{name: "outside latitude", lat: 50, lon: 11, lat1: 47, lat2: 49, lon1: 10, lon2: 12, want: false},
		{name: "outside longitude", lat: 48, lon: 13, lat1: 47, lat2: 49, lon1: 10, lon2: 12, want: false},
		{name: "corners may be given in any order", lat: 48, lon: 11, lat1: 49, lat2: 47, lon1: 12, lon2: 10, want: true},
		{name: "southern corner", lat: -33.9, lon: 151.2, lat1: -34, lat2: -33, lon1: 150, lon2: 152, want: true},
		{name: "south bound out of range", lat: -35, lon: 151, lat1: -34, lat2: -33, lon1: 150, lon2: 152, want: false},
		{name: "edges are inclusive (min corner)", lat: 47, lon: 10, lat1: 47, lat2: 49, lon1: 10, lon2: 12, want: true},
		{name: "edges are inclusive (max corner)", lat: 49, lon: 12, lat1: 47, lat2: 49, lon1: 10, lon2: 12, want: true},
		{name: "just past the edge", lat: 46.9999, lon: 11, lat1: 47, lat2: 49, lon1: 10, lon2: 12, want: false},
		{name: "antimeridian wrap: east side", lat: 48, lon: 175, lat1: 45, lat2: 50, lon1: 170, lon2: -170, want: true},
		{name: "antimeridian wrap: west side", lat: 48, lon: -175, lat1: 45, lat2: 50, lon1: 170, lon2: -170, want: true},
		{name: "antimeridian wrap: prime meridian excluded", lat: 48, lon: 0, lat1: 45, lat2: 50, lon1: 170, lon2: -170, want: false},
		{name: "antimeridian wrap: just outside east", lat: 48, lon: 169.9, lat1: 45, lat2: 50, lon1: 170, lon2: -170, want: false},
		{name: "antimeridian wrap: latitude still enforced", lat: 60, lon: 175, lat1: 45, lat2: 50, lon1: 170, lon2: -170, want: false},
		{name: "non-wrapping box given as -170..170 covers the middle", lat: 48, lon: 0, lat1: 45, lat2: 50, lon1: -170, lon2: 170, want: true},
		{name: "non-wrapping box given as -170..170 excludes the far east", lat: 48, lon: 175, lat1: 45, lat2: 50, lon1: -170, lon2: 170, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InBox(tt.lat, tt.lon, tt.lat1, tt.lat2, tt.lon1, tt.lon2); got != tt.want {
				t.Fatalf("InBox(%g, %g, %g, %g, %g, %g) = %v, want %v",
					tt.lat, tt.lon, tt.lat1, tt.lat2, tt.lon1, tt.lon2, got, tt.want)
			}
		})
	}
}
