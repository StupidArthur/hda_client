package server

import (
	"testing"
	"time"

	"github.com/gopcua/opcua/ua"
)

func TestApplyTimestampsToReturn(t *testing.T) {
	ts := time.Unix(1, 0).UTC()
	in := &ua.DataValue{
		EncodingMask:    ua.DataValueValue | ua.DataValueSourceTimestamp | ua.DataValueServerTimestamp,
		Value:           ua.MustVariant(1.0),
		SourceTimestamp: ts,
		ServerTimestamp: ts,
	}
	for _, tc := range []struct {
		mode           ua.TimestampsToReturn
		source, server bool
	}{
		{ua.TimestampsToReturnSource, true, false},
		{ua.TimestampsToReturnServer, false, true},
		{ua.TimestampsToReturnBoth, true, true},
		{ua.TimestampsToReturnNeither, false, false},
	} {
		got := applyTimestampsToReturn(in, tc.mode)
		if got.Has(ua.DataValueSourceTimestamp) != tc.source || got.Has(ua.DataValueServerTimestamp) != tc.server {
			t.Fatalf("mode=%v mask=%#x", tc.mode, got.EncodingMask)
		}
	}
	if !in.Has(ua.DataValueSourceTimestamp) || !in.Has(ua.DataValueServerTimestamp) {
		t.Fatal("input DataValue was mutated")
	}
}
