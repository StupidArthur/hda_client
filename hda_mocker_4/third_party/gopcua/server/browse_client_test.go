package server_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/id"
	"github.com/gopcua/opcua/server"
	"github.com/gopcua/opcua/ua"
	"github.com/stretchr/testify/require"
)

func TestGopcuaBrowseMockerTree(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	s := server.New(server.EndPoint("127.0.0.1", port), server.EnableSecurity("None", ua.MessageSecurityModeNone), server.EnableAuthMode(ua.UserTokenTypeAnonymous))
	for len(s.Namespaces()) < 3 {
		s.AddNamespace(server.NewNameSpace("reserved"))
	}
	ns := server.NewNodeNameSpace(s, "mocker")
	folder := server.NewNode(ua.NewStringNodeID(3, "HDA_Mocker"), server.Attributes{
		ua.AttributeIDNodeClass:   server.DataValueFromValue(uint32(ua.NodeClassObject)),
		ua.AttributeIDBrowseName:  server.DataValueFromValue(&ua.QualifiedName{Name: "HDA_Mocker", NamespaceIndex: 3}),
		ua.AttributeIDDisplayName: server.DataValueFromValue(ua.NewLocalizedText("HDA_Mocker")),
		ua.AttributeIDDataType:    server.DataValueFromValue(ua.NewNumericExpandedNodeID(0, id.FolderType)),
	}, nil, nil)
	ns.AddNode(folder)
	standard, err := s.Namespace(0)
	require.NoError(t, err)
	standard.Objects().AddRef(folder, id.Organizes, true)
	for i := 0; i < 103; i++ {
		name := fmt.Sprintf("AI%04d.PV", i)
		child := server.NewVariableNode(ua.NewStringNodeID(3, name), name, float64(i))
		ns.AddNode(child)
		folder.AddRef(child, id.Organizes, true)
	}
	require.NoError(t, s.Start(ctx))
	defer s.Close()
	c, err := opcua.NewClient(s.URLs()[0], opcua.SecurityMode(ua.MessageSecurityModeNone), opcua.SecurityPolicy(ua.SecurityPolicyURINone), opcua.AuthAnonymous())
	require.NoError(t, err)
	require.NoError(t, c.Connect(ctx))
	defer c.Close(ctx)
	children, err := c.Node(ua.NewNumericNodeID(0, id.ObjectsFolder)).Children(ctx, 0, ua.NodeClassUnspecified)
	require.NoError(t, err)
	found := false
	for _, child := range children {
		if child.ID.Equal(folder.ID()) {
			found = true
		}
	}
	require.True(t, found, "Objects must expose HDA_Mocker")
	tags, err := c.Node(folder.ID()).Children(ctx, 0, ua.NodeClassVariable)
	require.NoError(t, err)
	require.Len(t, tags, 103)
}
