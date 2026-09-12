package main

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
)

func TestNetworkReadTimestampModes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := openStore(filepath.Join(t.TempDir(), "history.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ts := time.Unix(1704067200, 0).UTC()
	if _, err = s.db.Exec("INSERT INTO tags VALUES (1, 'tag')"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("INSERT INTO samples VALUES (1, ?, 1.0, 0, 'import', 1)", ts.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	var cfg Config
	cfg.Server.Endpoint = fmt.Sprintf("opc.tcp://127.0.0.1:%d", port)
	cfg.Server.NS = 3
	cfg.Server.Namespace = "urn:test:timestamps"
	m, err := newServer(cfg, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer m.close()

	c, err := opcua.NewClient(cfg.Server.Endpoint, opcua.SecurityMode(ua.MessageSecurityModeNone), opcua.SecurityPolicy(ua.SecurityPolicyURINone), opcua.AuthAnonymous())
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)

	for _, tc := range []struct {
		mode           ua.TimestampsToReturn
		source, server bool
	}{
		{ua.TimestampsToReturnSource, true, false},
		{ua.TimestampsToReturnServer, false, true},
		{ua.TimestampsToReturnBoth, true, true},
		{ua.TimestampsToReturnNeither, false, false},
	} {
		res, err := c.Read(ctx, &ua.ReadRequest{
			TimestampsToReturn: tc.mode,
			NodesToRead:        []*ua.ReadValueID{{NodeID: ua.NewStringNodeID(3, "tag"), AttributeID: ua.AttributeIDValue}},
		})
		if err != nil {
			t.Fatalf("mode=%v: %v", tc.mode, err)
		}
		dv := res.Results[0]
		if dv.Has(ua.DataValueSourceTimestamp) != tc.source || dv.Has(ua.DataValueServerTimestamp) != tc.server {
			t.Fatalf("mode=%v mask=%#x", tc.mode, dv.EncodingMask)
		}
		if tc.source && !dv.SourceTimestamp.Equal(ts) || tc.server && !dv.ServerTimestamp.Equal(ts) {
			t.Fatalf("mode=%v source=%v server=%v", tc.mode, dv.SourceTimestamp, dv.ServerTimestamp)
		}
	}

	_, err = c.Read(ctx, &ua.ReadRequest{
		TimestampsToReturn: ua.TimestampsToReturnInvalid,
		NodesToRead:        []*ua.ReadValueID{{NodeID: ua.NewStringNodeID(3, "tag"), AttributeID: ua.AttributeIDValue}},
	})
	if err == nil {
		t.Fatal("invalid TimestampsToReturn was accepted")
	}
}
