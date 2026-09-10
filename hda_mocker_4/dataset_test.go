package main

import (
	"math"
	"testing"
)

func TestNormalizeValueQuality(t *testing.T) {
	if v, q := normalizeValue(nil); v != nil || q != 0x809B0000 {
		t.Fatal("nil quality")
	}
	if v, q := normalizeValue(float64(1.5)); v == nil || *v != 1.5 || q != 0 {
		t.Fatal("finite value")
	}
	if v, q := normalizeValue(math.NaN()); v != nil || q != 0x809B0000 {
		t.Fatal("NaN quality")
	}
}
