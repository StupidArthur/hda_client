package main

import (
	"testing"
	"time"

	"github.com/gopcua/opcua/ua"
)

func TestDataValuePreservesQuality(t *testing.T) {
	v := 3.0
	d := dataValueFromSample(Sample{TS: time.UnixMicro(1).UTC(), Value: &v, Quality: uint32(ua.StatusBadNoData)})
	if d.Status != ua.StatusBadNoData || !d.Has(ua.DataValueValue) {
		t.Fatalf("quality/value lost: status=%v mask=%x", d.Status, d.EncodingMask)
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
