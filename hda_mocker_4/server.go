package main

import (
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gopcua/opcua/id"
	"github.com/gopcua/opcua/server"
	"github.com/gopcua/opcua/server/attrs"
	"github.com/gopcua/opcua/ua"
	"github.com/gopcua/opcua/uasc"
)

type mockServer struct {
	*server.Server
	store    *Store
	playback *Playback
	daTags   map[string]bool
	history  *HistoryEngine
	cfg      Config
	ns       *server.NodeNameSpace
}

func newServer(cfg Config, store *Store, p *Playback) (*mockServer, error) {
	u, e := url.Parse(cfg.Server.Endpoint)
	if e != nil {
		return nil, e
	}
	host := u.Hostname()
	port, _ := strconv.Atoi(u.Port())
	if host == "" {
		host = "0.0.0.0"
	}
	if port == 0 {
		port = 4840
	}
	base := server.New(server.EndPoint(host, port), server.EnableSecurity("None", ua.MessageSecurityModeNone), server.EnableAuthMode(ua.UserTokenTypeAnonymous), server.ServerName("HDA Mocker 4"))
	for len(base.Namespaces()) < cfg.Server.NS {
		index := len(base.Namespaces())
		base.AddNamespace(server.NewNameSpace(fmt.Sprintf("urn:hda:mocker4:reserved:%d", index)))
	}
	if len(base.Namespaces()) != cfg.Server.NS {
		return nil, fmt.Errorf("server.ns %d is already occupied; next available namespace index is %d", cfg.Server.NS, len(base.Namespaces()))
	}
	ns := server.NewNodeNameSpace(base, cfg.Server.Namespace)
	if int(ns.ID()) != cfg.Server.NS {
		return nil, fmt.Errorf("configured namespace index %d, assigned %d", cfg.Server.NS, ns.ID())
	}
	root := ns.Objects()
	folder := server.NewNode(
		ua.NewStringNodeID(ns.ID(), "HDA_Mocker"),
		server.Attributes{
			ua.AttributeIDNodeClass:     server.DataValueFromValue(uint32(ua.NodeClassObject)),
			ua.AttributeIDBrowseName:    server.DataValueFromValue(&ua.QualifiedName{NamespaceIndex: ns.ID(), Name: "HDA_Mocker"}),
			ua.AttributeIDDisplayName:   server.DataValueFromValue(attrs.DisplayName("HDA_Mocker", "HDA_Mocker")),
			ua.AttributeIDDescription:   server.DataValueFromValue(attrs.DisplayName("HDA_Mocker", "HDA_Mocker")),
			ua.AttributeIDEventNotifier: server.DataValueFromValue(int16(0)),
		},
		server.References{},
		nil,
	)
	ns.AddNode(folder)
	folder.AddRef(base.Node(ua.NewNumericNodeID(0, id.BaseObjectType)), id.HasTypeDefinition, true)
	root.AddRef(folder, id.Organizes, true)
	standard, e := base.Namespace(0)
	if e != nil {
		return nil, fmt.Errorf("get standard namespace: %w", e)
	}
	standard.Objects().AddRef(folder, id.Organizes, true)
	folder.AddRef(standard.Objects(), id.Organizes, false)
	tags, e := store.tags()
	if e != nil {
		return nil, e
	}
	daTags := map[string]bool{}
	if p != nil {
		for _, f := range p.files {
			for _, name := range f.Tags {
				daTags[name] = true
			}
		}
	}
	m := &mockServer{Server: base, store: store, playback: p, daTags: daTags, cfg: cfg, ns: ns}
	for _, t := range tags {
		t := t
		n := server.NewVariableNode(ua.NewStringNodeID(ns.ID(), t.Name), t.Name, func() *ua.DataValue { return m.value(t.Name) })
		n.SetAttribute(ua.AttributeIDBrowseName, server.DataValueFromValue(&ua.QualifiedName{NamespaceIndex: ns.ID(), Name: t.Name}))
		n.SetAttribute(ua.AttributeIDDataType, server.DataValueFromValue(ua.NewNumericNodeID(0, id.Double)))
		n.SetAttribute(ua.AttributeIDValueRank, server.DataValueFromValue(int32(-1)))
		n.SetAttribute(ua.AttributeIDArrayDimensions, server.DataValueFromValue([]uint32{}))
		n.SetAttribute(ua.AttributeIDAccessLevel, server.DataValueFromValue(uint8(ua.AccessLevelTypeCurrentRead|ua.AccessLevelTypeHistoryRead)))
		n.SetAttribute(ua.AttributeIDUserAccessLevel, server.DataValueFromValue(uint8(ua.AccessLevelTypeCurrentRead|ua.AccessLevelTypeHistoryRead)))
		n.SetAttribute(ua.AttributeIDHistorizing, server.DataValueFromValue(true))
		n.AddRef(base.Node(ua.NewNumericNodeID(0, id.BaseDataVariableType)), id.HasTypeDefinition, true)
		ns.AddNode(n)
		folder.AddRef(n, id.HasComponent, true)
		n.AddRef(folder, id.HasComponent, false)
	}
	m.history = newHistory(store, cfg.Server.MaxPageSize, ns.ID())
	if p != nil {
		p.SetNotify(func(name string) { ns.ChangeNotification(ua.NewStringNodeID(ns.ID(), name)) })
	}
	base.RegisterHandler(id.HistoryReadRequest_Encoding_DefaultBinary, m.historyRead)
	base.RegisterHandler(id.GetEndpointsRequest_Encoding_DefaultBinary, m.getEndpoints)
	return m, nil
}
func (m *mockServer) value(name string) *ua.DataValue {
	var x *Sample
	if m.playback != nil {
		if v, ok := m.playback.Current(name); ok {
			x = v
		}
	}
	if x == nil && !m.daTags[name] {
		x, _ = m.store.latest(name)
	}
	if x == nil {
		now := time.Now().UTC()
		return &ua.DataValue{EncodingMask: ua.DataValueStatusCode | ua.DataValueSourceTimestamp | ua.DataValueServerTimestamp, Status: ua.StatusBadWaitingForInitialData, SourceTimestamp: now, ServerTimestamp: now}
	}
	dv := &ua.DataValue{EncodingMask: ua.DataValueStatusCode | ua.DataValueSourceTimestamp | ua.DataValueServerTimestamp, Status: ua.StatusCode(x.Quality), SourceTimestamp: x.TS, ServerTimestamp: x.TS}
	if x.Value != nil {
		dv.EncodingMask |= ua.DataValueValue
		dv.Value = ua.MustVariant(*x.Value)
	}
	return dv
}
func (m *mockServer) historyRead(sc *uasc.SecureChannel, r ua.Request, reqID uint32) (ua.Response, error) {
	req, ok := r.(*ua.HistoryReadRequest)
	if !ok {
		return nil, ua.StatusBadRequestTypeInvalid
	}
	if req.HistoryReadDetails == nil {
		return &ua.HistoryReadResponse{ResponseHeader: responseHeader(requestHandle(req), ua.StatusBadHistoryOperationInvalid)}, nil
	}
	d, ok := req.HistoryReadDetails.Value.(*ua.ReadRawModifiedDetails)
	if !ok {
		return &ua.HistoryReadResponse{ResponseHeader: responseHeader(requestHandle(req), ua.StatusBadHistoryOperationUnsupported)}, nil
	}
	if !supportsRawHistory(d) {
		return &ua.HistoryReadResponse{ResponseHeader: responseHeader(requestHandle(req), ua.StatusBadHistoryOperationUnsupported)}, nil
	}
	session := "anonymous"
	if req.RequestHeader != nil && req.RequestHeader.AuthenticationToken != nil {
		session = req.RequestHeader.AuthenticationToken.String()
	}
	out := make([]*ua.HistoryReadResult, len(req.NodesToRead))
	for i, item := range req.NodesToRead {
		out[i] = m.history.page(session, d, item, req.ReleaseContinuationPoints, req.TimestampsToReturn)
	}
	return &ua.HistoryReadResponse{ResponseHeader: responseHeader(requestHandle(req), ua.StatusOK), Results: out}, nil
}

func (m *mockServer) getEndpoints(sc *uasc.SecureChannel, r ua.Request, reqID uint32) (ua.Response, error) {
	req, ok := r.(*ua.GetEndpointsRequest)
	if !ok {
		return nil, ua.StatusBadRequestTypeInvalid
	}
	endpoints := m.Endpoints()
	if req.EndpointURL != "" {
		// 0.0.0.0 is a listen address, not a usable discovery address. Advertise
		// the URL through which this client reached us while preserving all other
		// endpoint capabilities.
		endpoints = cloneEndpointsWithURL(endpoints, req.EndpointURL)
	}
	log.Printf("GetEndpoints request_url=%q returned=%d", req.EndpointURL, len(endpoints))
	return &ua.GetEndpointsResponse{
		ResponseHeader: responseHeader(requestHandleGetEndpoints(req), ua.StatusOK),
		Endpoints:      endpoints,
	}, nil
}

func cloneEndpointsWithURL(src []*ua.EndpointDescription, endpointURL string) []*ua.EndpointDescription {
	out := make([]*ua.EndpointDescription, len(src))
	for i, ep := range src {
		if ep == nil {
			continue
		}
		copy := *ep
		copy.EndpointURL = endpointURL
		if ep.Server != nil {
			app := *ep.Server
			app.DiscoveryURLs = []string{endpointURL}
			copy.Server = &app
		}
		out[i] = &copy
	}
	return out
}

func requestHandleGetEndpoints(r *ua.GetEndpointsRequest) uint32 {
	if r != nil && r.RequestHeader != nil {
		return r.RequestHeader.RequestHandle
	}
	return 0
}

func supportsRawHistory(d *ua.ReadRawModifiedDetails) bool {
	return d != nil && !d.IsReadModified && !d.ReturnBounds
}

func requestHandle(r *ua.HistoryReadRequest) uint32 {
	if r != nil && r.RequestHeader != nil {
		return r.RequestHeader.RequestHandle
	}
	return 0
}
func responseHeader(h uint32, s ua.StatusCode) *ua.ResponseHeader {
	return &ua.ResponseHeader{Timestamp: time.Now().UTC(), RequestHandle: h, ServiceResult: s, ServiceDiagnostics: &ua.DiagnosticInfo{}, StringTable: []string{}, AdditionalHeader: ua.NewExtensionObject(nil)}
}
func (m *mockServer) close() {
	m.history.Close()
	if m.playback != nil {
		m.playback.Close()
	}
	m.Server.Close()
}

var _ = strings.TrimSpace
