package iss_driver

import (
	"math"
	"testing"
	"time"
)

func TestPropagateECIPreservesOrbitalRadius(t *testing.T) {
	pos := [3]float64{6771, 0, 0}
	vel := [3]float64{0, 7670, 0} // m/s

	next := propagateECI(pos, vel, 10*60)
	r0 := radius(pos)
	r1 := radius(next)

	if math.Abs(r1-r0) > 0.001 {
		t.Fatalf("radius drifted: before %.6f km after %.6f km", r0, r1)
	}
	if next[1] <= 0 {
		t.Fatalf("expected prograde orbital motion, got position %#v", next)
	}
}

func TestVelocityKMPerSecAcceptsMetersOrKilometresPerSecond(t *testing.T) {
	_, mpsY, _ := velocityKMPerSec(0, 7670, 0)
	_, kpsY, _ := velocityKMPerSec(0, 7.67, 0)

	if math.Abs(mpsY-7.67) > 0.0001 {
		t.Fatalf("m/s conversion failed: %.6f", mpsY)
	}
	if math.Abs(kpsY-7.67) > 0.0001 {
		t.Fatalf("km/s passthrough failed: %.6f", kpsY)
	}
}

func TestECIToLatLonAltUsesSampleTime(t *testing.T) {
	sampleAt := time.Date(2026, 5, 20, 13, 0, 0, 0, time.UTC)
	_, lon0, _ := eciToLatLonAlt(6771, 0, 0, sampleAt)
	_, lon1, _ := eciToLatLonAlt(6771, 0, 0, sampleAt.Add(time.Hour))

	delta := normalizeDegrees(lon1 - lon0)
	if math.Abs(delta+15.041) > 0.1 {
		t.Fatalf("longitude delta = %.3f°, want about -15.041°", delta)
	}
}

func TestInterpolateEphemerisMatchesKnownISSPosition(t *testing.T) {
	points := []ephemerisPoint{
		{
			at:  time.Date(2026, 5, 20, 13, 48, 0, 0, time.UTC),
			pos: [3]float64{4236.902616800400, 3149.742417991190, -4292.957925372070},
			vel: [3]float64{-1.30726266586248, 6.62953291159074, 3.58372547621890},
		},
		{
			at:  time.Date(2026, 5, 20, 13, 52, 0, 0, time.UTC),
			pos: [3]float64{3773.633997170520, 4607.604071148420, -3287.506386162970},
			vel: [3]float64{-2.53024790052392, 5.44510093270569, 4.74434615523097},
		},
	}
	at := time.Date(2026, 5, 20, 13, 50, 16, 0, time.UTC)

	pos, vel, ok := interpolateEphemeris(points, at)
	if !ok {
		t.Fatal("interpolateEphemeris failed")
	}

	lat, lon, alt := eciToLatLonAlt(pos[0], pos[1], pos[2], at)
	if math.Abs(lat-(-33.5211)) > 0.01 {
		t.Fatalf("lat = %.4f, want -33.5211", lat)
	}
	if math.Abs(normalizeDegrees(lon-(-40.7956))) > 0.01 {
		t.Fatalf("lon = %.4f, want -40.7956", lon)
	}
	if math.Abs(alt-432.5210) > 0.05 {
		t.Fatalf("alt = %.4f, want 432.5210", alt)
	}
	if speed := radius(vel); math.Abs(speed-7.655) > 0.01 {
		t.Fatalf("speed = %.4f, want about 7.655 km/s", speed)
	}
}

func radius(v [3]float64) float64 {
	return math.Sqrt(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])
}

func normalizeDegrees(v float64) float64 {
	for v > 180 {
		v -= 360
	}
	for v < -180 {
		v += 360
	}
	return v
}
