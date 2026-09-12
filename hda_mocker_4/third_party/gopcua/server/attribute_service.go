package server

import (
	"time"

	"github.com/gopcua/opcua/ua"
	"github.com/gopcua/opcua/uasc"
)

// AttributeService implements the Attribute Service Set.
//
// https://reference.opcfoundation.org/Core/Part4/v105/docs/5.10
type AttributeService struct {
	srv *Server
}

// https://reference.opcfoundation.org/Core/Part4/v105/docs/5.10.2
func (s *AttributeService) Read(sc *uasc.SecureChannel, r ua.Request, reqID uint32) (ua.Response, error) {
	if s.srv.cfg.logger != nil {
		s.srv.cfg.logger.Debug("Handling %T", r)
	}

	req, err := safeReq[*ua.ReadRequest](r)
	if err != nil {
		return nil, err
	}
	if req.TimestampsToReturn > ua.TimestampsToReturnNeither {
		return &ua.ReadResponse{ResponseHeader: responseHeader(req.RequestHeader.RequestHandle, ua.StatusBadTimestampsToReturnInvalid)}, nil
	}

	results := make([]*ua.DataValue, len(req.NodesToRead))
	for i, n := range req.NodesToRead {
		if s.srv.cfg.logger != nil {
			s.srv.cfg.logger.Debug("read: node=%s attr=%s", n.NodeID, n.AttributeID)
		}

		ns, err := s.srv.Namespace(int(n.NodeID.Namespace()))
		if err != nil {
			results[i] = &ua.DataValue{
				EncodingMask:    ua.DataValueServerTimestamp | ua.DataValueStatusCode,
				ServerTimestamp: time.Now(),
				Status:          ua.StatusBad,
			}
			continue
		}
		results[i] = applyTimestampsToReturn(ns.Attribute(n.NodeID, n.AttributeID), req.TimestampsToReturn)

	}

	response := &ua.ReadResponse{
		ResponseHeader: responseHeader(req.RequestHeader.RequestHandle, ua.StatusOK),
		Results:        results,
	}

	return response, nil
}

// applyTimestampsToReturn trims timestamps supplied by node getters. Keeping
// this in the framework makes Read and monitored-item behavior consistent.
func applyTimestampsToReturn(in *ua.DataValue, mode ua.TimestampsToReturn) *ua.DataValue {
	if in == nil {
		return in
	}
	out := *in
	switch mode {
	case ua.TimestampsToReturnSource:
		out.EncodingMask &^= ua.DataValueServerTimestamp
		out.ServerTimestamp = time.Time{}
	case ua.TimestampsToReturnServer:
		out.EncodingMask &^= ua.DataValueSourceTimestamp
		out.SourceTimestamp = time.Time{}
	case ua.TimestampsToReturnBoth:
	case ua.TimestampsToReturnNeither:
		out.EncodingMask &^= ua.DataValueSourceTimestamp | ua.DataValueServerTimestamp
		out.SourceTimestamp, out.ServerTimestamp = time.Time{}, time.Time{}
	}
	return &out
}

// https://reference.opcfoundation.org/Core/Part4/v105/docs/5.10.3
func (s *AttributeService) HistoryRead(sc *uasc.SecureChannel, r ua.Request, reqID uint32) (ua.Response, error) {
	if s.srv.cfg.logger != nil {
		s.srv.cfg.logger.Debug("Handling %T", r)
	}

	req, err := safeReq[*ua.HistoryReadRequest](r)
	if err != nil {
		return nil, err
	}
	return serviceUnsupported(req.RequestHeader), nil
}

// https://reference.opcfoundation.org/Core/Part4/v105/docs/5.10.4
func (s *AttributeService) Write(sc *uasc.SecureChannel, r ua.Request, reqID uint32) (ua.Response, error) {

	req, err := safeReq[*ua.WriteRequest](r)
	if err != nil {
		return nil, err
	}

	status := make([]ua.StatusCode, len(req.NodesToWrite))

	for i := range req.NodesToWrite {
		n := req.NodesToWrite[i]
		if s.srv.cfg.logger != nil {
			s.srv.cfg.logger.Debug("write: node=%s attr=%v", n.NodeID, n.AttributeID)
		}

		ns, err := s.srv.Namespace(int(n.NodeID.Namespace()))
		if err != nil {
			status[i] = ua.StatusBadNodeIDUnknown
			continue
		}

		status[i] = ns.SetAttribute(n.NodeID, n.AttributeID, n.Value)

	}
	response := &ua.WriteResponse{
		ResponseHeader: &ua.ResponseHeader{
			Timestamp:          time.Now(),
			RequestHandle:      req.RequestHeader.RequestHandle,
			ServiceResult:      ua.StatusOK,
			ServiceDiagnostics: &ua.DiagnosticInfo{},
			StringTable:        []string{},
			AdditionalHeader:   ua.NewExtensionObject(nil),
		},
		Results:         status,
		DiagnosticInfos: []*ua.DiagnosticInfo{},
	}

	return response, nil

}

// https://reference.opcfoundation.org/Core/Part4/v105/docs/5.10.5
func (s *AttributeService) HistoryUpdate(sc *uasc.SecureChannel, r ua.Request, reqID uint32) (ua.Response, error) {
	if s.srv.cfg.logger != nil {
		s.srv.cfg.logger.Debug("Handling %T", r)
	}

	req, err := safeReq[*ua.HistoryUpdateRequest](r)
	if err != nil {
		return nil, err
	}
	return serviceUnsupported(req.RequestHeader), nil
}
