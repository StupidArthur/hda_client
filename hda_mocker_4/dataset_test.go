package main

import (
	"math"
	"testing"

	"github.com/gopcua/opcua/ua"
)

func TestNormalizeValueQuality(t *testing.T) {
	if v, q := normalizeValue(nil); v != nil || q != uint32(ua.StatusBadWaitingForInitialData) {
		t.Fatal("nil quality")
	}
	if v, q := normalizeValue(float64(1.5)); v == nil || *v != 1.5 || q != 0 {
		t.Fatal("finite value")
	}
	if v, q := normalizeValue(math.NaN()); v != nil || q != uint32(ua.StatusBadWaitingForInitialData) {
		t.Fatal("NaN quality")
	}
}

func TestNormalizeUAStatus(t *testing.T) {
	for _, tt := range []struct {
		in   any
		want uint32
	}{
		{int64(0), uint32(ua.StatusOK)},
		{int64(ua.StatusBadNoCommunication), uint32(ua.StatusBadNoCommunication)},
		{uint64(ua.StatusUncertainLastUsableValue), uint32(ua.StatusUncertainLastUsableValue)},
		{uint64(ua.StatusGoodLocalOverride), uint32(ua.StatusGoodLocalOverride)},
		{nil, uint32(ua.StatusOK)},
		{int64(-1), uint32(ua.StatusBad)}, {uint64(4294967296), uint32(ua.StatusBad)},
	} {
		if got := normalizeUAStatus(tt.in); got != tt.want {
			t.Fatalf("status %v: got %#x want %#x", tt.in, got, tt.want)
		}
	}
}
