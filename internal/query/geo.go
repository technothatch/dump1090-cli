// Package query filters and ranks aircraft for the CLI commands: great-
// circle distance and bearing math, bounding-box and nearest queries,
// display ordering, squawk grouping and emergency detection.
package query

import "math"

// earthRadiusNm is the mean Earth radius in nautical miles (6371.0088 km
// divided by 1.852 km per nautical mile). One degree of latitude is
// therefore almost exactly 60 nm, which is the number every aviation
// distance check starts from.
const earthRadiusNm = 3440.065

// HaversineNm returns the great-circle distance between two WGS-84 points
// in nautical miles. Inputs are decimal degrees.
func HaversineNm(lat1, lon1, lat2, lon2 float64) float64 {
	const deg = math.Pi / 180
	dLat := (lat2 - lat1) * deg
	dLon := (lon2 - lon1) * deg
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*deg)*math.Cos(lat2*deg)*math.Sin(dLon/2)*math.Sin(dLon/2)
	// Clamp a into [0,1]: floating point can push it a hair outside, and
	// sqrt of a negative number would return NaN.
	if a > 1 {
		a = 1
	}
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusNm * c
}

// BearingDeg returns the initial true bearing from point 1 to point 2 in
// degrees, normalised to [0, 360). Coincident points report 0.
func BearingDeg(lat1, lon1, lat2, lon2 float64) float64 {
	const deg = math.Pi / 180
	if lat1 == lat2 && lon1 == lon2 {
		return 0
	}
	theta := math.Atan2(
		math.Sin((lon2-lon1)*deg)*math.Cos(lat2*deg),
		math.Cos(lat1*deg)*math.Sin(lat2*deg)-math.Sin(lat1*deg)*math.Cos(lat2*deg)*math.Cos((lon2-lon1)*deg),
	) * 180 / math.Pi
	for theta < 0 {
		theta += 360
	}
	for theta >= 360 {
		theta -= 360
	}
	return theta
}

// InBox reports whether the point (lat, lon) lies inside the rectangle
// spanned by two corner points. Corner order does not matter: latitude and
// longitude bounds are normalised. A box whose longitude bounds run from a
// larger to a smaller value crosses the antimeridian and wraps (for example
// lon 170 to -170 covers 170 … 180 and -180 … -170). Edges are inclusive.
func InBox(lat, lon, lat1, lat2, lon1, lon2 float64) bool {
	latMin, latMax := minMax(lat1, lat2)
	if lat < latMin || lat > latMax {
		return false
	}
	// lon1 > lon2 is either swapped corners (10 and 12) or an
	// antimeridian crossing (170 and -170). The crossing interpretation
	// only applies when it yields the narrower rectangle.
	if lon1 > lon2 && lon1-lon2 > 180 {
		return lon >= lon1 || lon <= lon2
	}
	lonMin, lonMax := minMax(lon1, lon2)
	return lon >= lonMin && lon <= lonMax
}

// minMax returns its arguments sorted ascending.
func minMax(a, b float64) (float64, float64) {
	if a > b {
		return b, a
	}
	return a, b
}
