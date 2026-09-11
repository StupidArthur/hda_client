package server

import (
	"fmt"
	"testing"

	"github.com/gopcua/opcua/id"
	"github.com/gopcua/opcua/ua"
	"github.com/stretchr/testify/require"
)

func TestBrowseOrganizesCompatibility(t *testing.T) {
	s := New()
	ns := NewNodeNameSpace(s, "compat")
	parent := ns.Objects()
	for i := 0; i < 103; i++ {
		parent.AddRef(ns.AddNewVariableNode(fmt.Sprintf("tag%d", i), float64(i)), id.Organizes, true)
	}
	for _, tc := range []struct {
		name string
		ref  uint32
		sub  bool
		want int
	}{
		{"all", 0, true, 103},
		{"organizes", id.Organizes, false, 103},
		{"hierarchical", id.HierarchicalReferences, true, 103},
		{"references", id.References, true, 103},
		{"exact_hierarchical", id.HierarchicalReferences, false, 0},
		{"exact_references", id.References, false, 0},
		{"unrelated", id.HasComponent, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bd := browseDesc(parent.ID())
			bd.ReferenceTypeID = ua.NewNumericNodeID(0, tc.ref)
			bd.IncludeSubtypes = tc.sub
			bd.NodeClassMask = uint32(ua.NodeClassVariable)
			result := ns.Browse(bd)
			require.Equal(t, ua.StatusOK, result.StatusCode)
			require.Len(t, result.References, tc.want)
		})
	}
}
