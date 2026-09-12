package main

import (
	"testing"
	"time"

	"github.com/gopcua/opcua/ua"
)

func TestDataValuePreservesQuality(t *testing.T) {
	v := 3.0
	d := dataValueFromSample(Sample{TS: time.UnixMicro(1).UTC(), Value: &v, Quality: uint32(ua.StatusBadNoData)}, ua.TimestampsToReturnBoth)
	if d.Status != ua.StatusBadNoData || !d.Has(ua.DataValueValue) {
		t.Fatalf("quality/value lost: status=%v mask=%x", d.Status, d.EncodingMask)
	}
}

func TestHistoryTimestampModes(t *testing.T) {
	v := 1.0
	x := Sample{TS: time.UnixMicro(7).UTC(), Value: &v}
	for _, tt := range []struct {
		mode           ua.TimestampsToReturn
		source, server bool
	}{
		{ua.TimestampsToReturnSource, true, false}, {ua.TimestampsToReturnServer, false, true}, {ua.TimestampsToReturnBoth, true, true},
	} {
		d := dataValueFromSample(x, tt.mode)
		if d.Has(ua.DataValueSourceTimestamp) != tt.source || d.Has(ua.DataValueServerTimestamp) != tt.server {
			t.Fatalf("mode %v mask=%x", tt.mode, d.EncodingMask)
		}
		if tt.source && !d.SourceTimestamp.Equal(x.TS) {
			t.Fatal("source timestamp")
		}
		if tt.server && !d.ServerTimestamp.Equal(x.TS) {
			t.Fatal("server timestamp")
		}
	}
}

func TestHistoryRejectsNeitherTimestamp(t *testing.T) {
	e := &HistoryEngine{nsID: 1}
	got := e.page("s", &ua.ReadRawModifiedDetails{}, &ua.HistoryReadValueID{NodeID: ua.NewStringNodeID(1, "tag")}, false, ua.TimestampsToReturnNeither)
	if got.StatusCode != ua.StatusBadTimestampsToReturnInvalid {
		t.Fatalf("status=%v", got.StatusCode)
	}
}

func TestRawHistoryDetailsSupport(t *testing.T) {
	if !supportsRawHistory(&ua.ReadRawModifiedDetails{}) {
		t.Fatal("plain raw history should be supported")
	}
	modified := &ua.ReadRawModifiedDetails{IsReadModified: true}
	if supportsRawHistory(modified) {
		t.Fatal("modified history should be rejected")
	}
	bounds := &ua.ReadRawModifiedDetails{ReturnBounds: true}
	if supportsRawHistory(bounds) {
		t.Fatal("bounded history should be rejected")
	}
}
