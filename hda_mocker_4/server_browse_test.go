package main

import (
	"path/filepath"
	"testing"

	"github.com/gopcua/opcua/id"
	"github.com/gopcua/opcua/ua"
)

func TestMockerBrowseMetadata(t *testing.T) {
	s, err := openStore(filepath.Join(t.TempDir(), "history.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec("INSERT INTO tags VALUES (1, 'AI1303A5.PV')"); err != nil {
		t.Fatal(err)
	}
	var cfg Config
	cfg.Server.Endpoint = "opc.tcp://127.0.0.1:0"
	cfg.Server.NS = 3
	cfg.Server.Namespace = "urn:test:mocker"
	m, err := newServer(cfg, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	folder := ua.NewStringNodeID(3, "HDA_Mocker")
	for _, ref := range []uint32{id.HasComponent, id.HierarchicalReferences} {
		result := m.ns.Browse(&ua.BrowseDescription{NodeID: folder, BrowseDirection: ua.BrowseDirectionForward, ReferenceTypeID: ua.NewNumericNodeID(0, ref), IncludeSubtypes: true, NodeClassMask: uint32(ua.NodeClassVariable), ResultMask: uint32(ua.BrowseResultMaskAll)})
		if len(result.References) != 1 {
			t.Fatalf("ref=%d: got %d", ref, len(result.References))
		}
		r := result.References[0]
		if r.BrowseName.NamespaceIndex != 3 || !r.TypeDefinition.NodeID.Equal(ua.NewNumericNodeID(0, id.BaseDataVariableType)) {
			t.Fatalf("invalid metadata: %+v", r)
		}
	}
	node := ua.NewStringNodeID(3, "AI1303A5.PV")
	checks := map[ua.AttributeID]any{
		ua.AttributeIDValueRank: int32(-1), ua.AttributeIDHistorizing: true,
		ua.AttributeIDAccessLevel: uint8(5), ua.AttributeIDUserAccessLevel: uint8(5),
	}
	for attr, want := range checks {
		dv := m.ns.Attribute(node, attr)
		if dv.Value == nil || dv.Value.Value() != want {
			t.Fatalf("attribute %v: %+v, want %v", attr, dv, want)
		}
	}
	dt := m.ns.Attribute(node, ua.AttributeIDDataType)
	if dt.Value.Type() != ua.TypeIDNodeID || !dt.Value.Value().(*ua.NodeID).Equal(ua.NewNumericNodeID(0, id.Double)) {
		t.Fatalf("invalid DataType: %+v", dt)
	}
}
