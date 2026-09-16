package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gopcua/opcua/id"
	"github.com/gopcua/opcua/server"
	"github.com/gopcua/opcua/ua"
	"github.com/gopcua/opcua/uasc"
)

type mockServer struct {
	*server.Server
	engine    *HistoryEngine
	namespace *server.NodeNameSpace
	settings  Settings
}

func newMockServer(s Settings) *mockServer {
	base := server.New(server.EndPoint(s.Host, s.Port), server.EnableSecurity("None", ua.MessageSecurityModeNone), server.EnableAuthMode(ua.UserTokenTypeAnonymous), server.ServerName("HDA Mocker 3"))
	specs := buildSpecs(s)
	ns := server.NewNodeNameSpace(base, s.NamespaceURI)
	root := ns.Objects()
	hda := server.NewFolderNode(ua.NewStringNodeID(ns.ID(), "HDA_Mocker"), "HDA_Mocker")
	ns.AddNode(hda)
	root.AddRef(hda, id.Organizes, true)
	folders := map[nodeKind]*server.Node{}
	for k, n := range map[nodeKind]string{kindConstant: "Static", kindDynamic: "Dynamic", kindBadRealtime: "BadRealtime"} {
		f := server.NewFolderNode(ua.NewStringNodeID(ns.ID(), "HDA_Mocker/"+n), n)
		ns.AddNode(f)
		hda.AddRef(f, id.Organizes, true)
		folders[k] = f
	}
	for _, spec := range specs {
		spec := spec
		n := server.NewVariableNode(ua.NewStringNodeID(ns.ID(), spec.ID), spec.ID, func() *ua.DataValue {
			now := timeNowUTC()
			status := ua.StatusOK
			if spec.Kind == kindBadRealtime && realtimeBad(now.Unix(), s.GoodDuration, s.BadDuration) {
				status = ua.StatusBadNoCommunication
			}
			return &ua.DataValue{EncodingMask: ua.DataValueValue | ua.DataValueStatusCode | ua.DataValueSourceTimestamp, Value: ua.MustVariant(valueAt(spec, now.Unix())), Status: status, SourceTimestamp: now}
		})
		ns.AddNode(n)
		folders[spec.Kind].AddRef(n, id.Organizes, true)
	}
	m := &mockServer{Server: base, engine: newHistoryEngine(s, specs), namespace: ns, settings: s}
	// Register before Start: gopcua's default handler registration preserves this override.
	base.RegisterHandler(id.HistoryReadRequest_Encoding_DefaultBinary, m.historyRead)
	return m
}

var timeNowUTC = func() time.Time { return time.Now().UTC() }

func realtimeBad(sec int64, good, bad time.Duration) bool {
	cycle := int64((good + bad) / time.Second)
	return cycle > 0 && sec%cycle >= int64(good/time.Second)
}
func (m *mockServer) historyRead(sc *uasc.SecureChannel, r ua.Request, reqID uint32) (ua.Response, error) {
	req, ok := r.(*ua.HistoryReadRequest)
	if !ok {
		return nil, ua.StatusBadRequestTypeInvalid
	}
	if req.HistoryReadDetails == nil {
		return &ua.HistoryReadResponse{ResponseHeader: response(requestHandle(req), ua.StatusBadHistoryOperationInvalid)}, nil
	}
	details, ok := req.HistoryReadDetails.Value.(*ua.ReadRawModifiedDetails)
	if !ok || details.IsReadModified {
		return &ua.HistoryReadResponse{ResponseHeader: response(requestHandle(req), ua.StatusBadHistoryOperationUnsupported)}, nil
	}
	session := "anonymous"
	if req.RequestHeader != nil && req.RequestHeader.AuthenticationToken != nil {
		session = req.RequestHeader.AuthenticationToken.String()
	}
	results := make([]*ua.HistoryReadResult, len(req.NodesToRead))
	for i, item := range req.NodesToRead {
		results[i] = m.engine.page(session, details, item, req.ReleaseContinuationPoints)
	}
	return &ua.HistoryReadResponse{ResponseHeader: response(requestHandle(req), ua.StatusOK), Results: results, DiagnosticInfos: []*ua.DiagnosticInfo{}}, nil
}

func requestHandle(req *ua.HistoryReadRequest) uint32 {
	if req != nil && req.RequestHeader != nil {
		return req.RequestHeader.RequestHandle
	}
	return 0
}
func response(handle uint32, status ua.StatusCode) *ua.ResponseHeader {
	return &ua.ResponseHeader{Timestamp: timeNowUTC(), RequestHandle: handle, ServiceResult: status, ServiceDiagnostics: &ua.DiagnosticInfo{}, StringTable: []string{}, AdditionalHeader: ua.NewExtensionObject(nil)}
}
func (m *mockServer) endpoint() string {
	return fmt.Sprintf("opc.tcp://%s:%d/hda-mocker/", m.settings.Host, m.settings.Port)
}
func runServer(s Settings) error {
	m := newMockServer(s)
	defer m.engine.Close()
	if err := m.Start(contextBackground()); err != nil {
		return err
	}
	defer m.Close()
	log.Printf("HDA Mocker 3: %s (ns=2, %d nodes, page cap=%d, CP=%s)", m.endpoint(), len(m.engine.specs), s.PageCap, s.CPMode)
	waitForInterrupt()
	return nil
}

// Small seams keep the server package free of global state and make Windows shutdown reliable.
var contextBackground = func() context.Context { return context.Background() }

func waitForInterrupt() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	signal.Stop(ch)
}
