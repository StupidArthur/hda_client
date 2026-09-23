package server

import (
	"log"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gopcua/opcua/ua"
	"github.com/gopcua/opcua/uasc"
)

// MonitoredItemService implements the MonitoredItem Service Set.
//
// https://reference.opcfoundation.org/Core/Part4/v105/docs/5.13
type MonitoredItemService struct {
	SubService *SubscriptionService
	Mu         sync.Mutex

	// items tracked by ID
	Items map[uint32]*MonitoredItem
	// items tracked by node
	Nodes map[string][]*MonitoredItem
	// items tracked by subscription
	Subs map[uint32][]*MonitoredItem

	id uint32
}

// function to get rid of all references to a specific Monitored Item (by ID number)
func (s *MonitoredItemService) DeleteMonitoredItem(id uint32) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.deleteMonitoredItemLocked(id)
}

// Caller must hold Mu.
func (s *MonitoredItemService) deleteMonitoredItemLocked(id uint32) {
	item := s.Items[id]
	delete(s.Items, id)
	if item == nil {
		return
	}
	if item.Req != nil && item.Req.ItemToMonitor != nil && item.Req.ItemToMonitor.NodeID != nil {
		nodeid := item.Req.ItemToMonitor.NodeID.String()
		items := slices.DeleteFunc(s.Nodes[nodeid], func(n *MonitoredItem) bool { return n == nil || n.ID == id })
		if len(items) == 0 {
			delete(s.Nodes, nodeid)
		} else {
			s.Nodes[nodeid] = items
		}
	}
	if item.Sub != nil {
		subID := item.Sub.ID
		items := slices.DeleteFunc(s.Subs[subID], func(n *MonitoredItem) bool { return n == nil || n.ID == id })
		if len(items) == 0 {
			delete(s.Subs, subID)
		} else {
			s.Subs[subID] = items
		}
	}
}

// function to delete all monitored items associated with a specific sub (as indicated by id number)
func (s *MonitoredItemService) DeleteSub(id uint32) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	for _, item := range slices.Clone(s.Subs[id]) {
		if item != nil {
			s.deleteMonitoredItemLocked(item.ID)
		}
	}
	delete(s.Subs, id)
}

func (s *MonitoredItemService) ChangeNotification(n *ua.NodeID) {

	s.Mu.Lock()
	defer s.Mu.Unlock()
	items, ok := s.Nodes[n.String()]

	if !ok {
		// this node isn't monitored - don't have to do anything.
		return
	}

	ns, err := s.SubService.srv.Namespace(int(n.Namespace()))

	for i := range items {
		item := items[i]
		if item == nil {
			continue
		}
		val := new(ua.MonitoredItemNotification)
		val.ClientHandle = item.Req.RequestedParameters.ClientHandle
		if err != nil {
			if s.SubService.srv.cfg.logger != nil {
				s.SubService.srv.cfg.logger.Warn("error getting namespace %d: %v", n.Namespace(), err)
			}
			val.Value = &ua.DataValue{}
			val.Value.Status = ua.StatusBad
			val.Value.EncodingMask |= ua.DataValueStatusCode
			item.Sub.Enqueue(val)
			continue
		}
		dv := ns.Attribute(n, item.Req.ItemToMonitor.AttributeID)
		val.Value = applyTimestampsToReturn(dv, item.TimestampsToReturn)
		item.Sub.Enqueue(val)
	}

}

func (s *MonitoredItemService) NextID() uint32 {
	i := atomic.AddUint32(&s.id, 1)
	if i == 0 {
		i = atomic.AddUint32(&s.id, 1)
	}
	return i
}

type MonitoredItem struct {
	ID  uint32
	Sub *Subscription
	Req *ua.MonitoredItemCreateRequest

	//TODO: use this
	Mode               ua.MonitoringMode
	TimestampsToReturn ua.TimestampsToReturn
}

// https://reference.opcfoundation.org/Core/Part4/v105/docs/5.13.2
// TODO: per-item results are never validated; every item is accepted with
// StatusOK whether or not the node exists. Part 4 §5.13.2.4 (Table 65) defines
// operation-level result codes such as Bad_NodeIdUnknown. Once implemented,
// client failure-mode tests (e.g. a rejected item during subscription
// recreation) could run against this server instead of an integration fixture.
func (s *MonitoredItemService) CreateMonitoredItems(sc *uasc.SecureChannel, r ua.Request, reqID uint32) (response ua.Response, serviceErr error) {
	if s.SubService.srv.cfg.logger != nil {
		s.SubService.srv.cfg.logger.Debug("Handling %T", r)
	}

	stage := "decode request"
	itemIndex := -1
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("opcua CreateMonitoredItems panic request_id=%d stage=%s item_index=%d panic=%v\n%s", reqID, stage, itemIndex, recovered, debug.Stack())
			panic(recovered)
		}
		if serviceErr != nil {
			log.Printf("opcua CreateMonitoredItems failed request_id=%d stage=%s item_index=%d error=%v", reqID, stage, itemIndex, serviceErr)
		}
	}()
	req, err := safeReq[*ua.CreateMonitoredItemsRequest](r)
	if err != nil {
		return nil, err
	}
	remote := "unknown"
	if sc != nil && sc.RemoteAddr() != nil {
		remote = sc.RemoteAddr().String()
	}
	preview := make([]string, 0, 5)
	for _, item := range req.ItemsToCreate {
		if len(preview) == cap(preview) {
			break
		}
		if item == nil || item.ItemToMonitor == nil || item.ItemToMonitor.NodeID == nil {
			preview = append(preview, "<nil>")
		} else {
			preview = append(preview, item.ItemToMonitor.NodeID.String())
		}
	}
	var handle uint32
	sessionID := "unknown"
	if req.RequestHeader != nil {
		handle = req.RequestHeader.RequestHandle
		if sess := s.SubService.srv.Session(req.RequestHeader); sess != nil && sess.ID != nil {
			sessionID = sess.ID.String()
		}
	}
	var sampling float64
	var queue uint32
	var mode ua.MonitoringMode
	if len(req.ItemsToCreate) > 0 && req.ItemsToCreate[0] != nil {
		first := req.ItemsToCreate[0]
		mode = first.MonitoringMode
		if first.RequestedParameters != nil {
			sampling = first.RequestedParameters.SamplingInterval
			queue = first.RequestedParameters.QueueSize
		}
	}
	log.Printf("opcua CreateMonitoredItems request remote=%s session_id=%s request_id=%d handle=%d subscription_id=%d count=%d timestamps=%v first_sampling_ms=%g first_queue_size=%d first_mode=%v node_ids=[%s]", remote, sessionID, reqID, handle, req.SubscriptionID, len(req.ItemsToCreate), req.TimestampsToReturn, sampling, queue, mode, strings.Join(preview, ", "))
	stage = "validate timestamps"
	if req.TimestampsToReturn > ua.TimestampsToReturnNeither {
		log.Printf("opcua CreateMonitoredItems rejected request_id=%d stage=%s status=%v", reqID, stage, ua.StatusBadTimestampsToReturnInvalid)
		return &ua.CreateMonitoredItemsResponse{ResponseHeader: responseHeader(req.RequestHeader.RequestHandle, ua.StatusBadTimestampsToReturnInvalid)}, nil
	}
	count := len(req.ItemsToCreate)

	res := make([]*ua.MonitoredItemCreateResult, count)
	initial := make([]*ua.NodeID, 0, count)

	subID := req.SubscriptionID
	if s.SubService.srv.cfg.logger != nil {
		s.SubService.srv.cfg.logger.Debug("Creating monitored items for sub #%d", subID)
	}
	stage = "lookup subscription"
	s.SubService.Mu.Lock()
	defer s.SubService.Mu.Unlock()
	sub, ok := s.SubService.Subs[subID]
	if !ok {
		return nil, ua.StatusBadSubscriptionIDInvalid
	}

	stage = "validate session"
	sess := s.SubService.srv.Session(req.RequestHeader)
	if sess == nil || sub.Session != sess {
		return nil, ua.StatusBadSessionIDInvalid
	}
	s.Mu.Lock()
	defer s.Mu.Unlock()

	for i := range req.ItemsToCreate {
		stage = "create monitored item"
		itemIndex = i
		itemreq := req.ItemsToCreate[i]
		nodeid := itemreq.ItemToMonitor.NodeID
		item := MonitoredItem{
			ID:                 s.NextID(),
			Sub:                sub,
			Req:                itemreq,
			TimestampsToReturn: req.TimestampsToReturn,
		}

		// book keeping of the new item
		s.Items[item.ID] = &item
		list, ok := s.Nodes[item.Req.ItemToMonitor.NodeID.String()]
		if !ok {
			list = make([]*MonitoredItem, 0, 1)
		}
		s.Nodes[item.Req.ItemToMonitor.NodeID.String()] = append(list, &item)

		list, ok = s.Subs[item.Sub.ID]
		if !ok {
			list = make([]*MonitoredItem, 0, 1)
		}
		s.Subs[item.Sub.ID] = append(list, &item)

		if s.SubService.srv.cfg.logger != nil {
			s.SubService.srv.cfg.logger.Debug("Adding monitored item '%s' to sub #%d as %d->%d",
				nodeid.String(),
				subID,
				item.ID,
				itemreq.RequestedParameters.ClientHandle)
		}
		res[i] = &ua.MonitoredItemCreateResult{
			StatusCode:              ua.StatusOK,
			MonitoredItemID:         item.ID,
			RevisedSamplingInterval: sub.RevisedPublishingInterval,
			RevisedQueueSize:        1,
			FilterResult:            ua.NewExtensionObject(nil),
		}
		initial = append(initial, nodeid)

	}
	itemIndex = -1

	resp := &ua.CreateMonitoredItemsResponse{
		ResponseHeader: &ua.ResponseHeader{
			Timestamp:          time.Now(),
			RequestHandle:      req.RequestHeader.RequestHandle,
			ServiceResult:      ua.StatusOK,
			ServiceDiagnostics: &ua.DiagnosticInfo{},
			StringTable:        []string{},
			AdditionalHeader:   ua.NewExtensionObject(nil),
		},
		Results:         res,                    //                  []StatusCode
		DiagnosticInfos: []*ua.DiagnosticInfo{}, //          []*DiagnosticInfo
	}

	// One worker per request avoids a goroutine per monitored item.
	if len(initial) > 0 {
		go func() {
			for _, nodeid := range initial {
				s.ChangeNotification(nodeid)
			}
		}()
	}
	log.Printf("opcua CreateMonitoredItems completed request_id=%d subscription_id=%d created=%d", reqID, subID, len(res))
	return resp, nil

}

// https://reference.opcfoundation.org/Core/Part4/v105/docs/5.13.3
func (s *MonitoredItemService) ModifyMonitoredItems(sc *uasc.SecureChannel, r ua.Request, reqID uint32) (ua.Response, error) {
	if s.SubService.srv.cfg.logger != nil {
		s.SubService.srv.cfg.logger.Debug("Handling %T", r)
	}

	req, err := safeReq[*ua.ModifyMonitoredItemsRequest](r)
	if err != nil {
		return nil, err
	}
	return serviceUnsupported(req.RequestHeader), nil
}

// https://reference.opcfoundation.org/Core/Part4/v105/docs/5.13.4
func (s *MonitoredItemService) SetMonitoringMode(sc *uasc.SecureChannel, r ua.Request, reqID uint32) (ua.Response, error) {
	if s.SubService.srv.cfg.logger != nil {
		s.SubService.srv.cfg.logger.Debug("Handling %T", r)
	}

	req, err := safeReq[*ua.SetMonitoringModeRequest](r)
	if err != nil {
		return nil, err
	}
	s.Mu.Lock()
	defer s.Mu.Unlock()

	results := make([]ua.StatusCode, len(req.MonitoredItemIDs))

	sess := s.SubService.srv.Session(req.RequestHeader)

	for i := range req.MonitoredItemIDs {
		id := req.MonitoredItemIDs[i]
		item, ok := s.Items[id]

		if item.Sub.Session.AuthTokenID.String() != sess.AuthTokenID.String() {
			results[i] = ua.StatusBadSessionIDInvalid
		}

		if !ok {
			results[i] = ua.StatusBadMonitoredItemIDInvalid
			continue
		}
		item.Mode = req.MonitoringMode
		results[i] = ua.StatusOK
	}

	return &ua.SetMonitoringModeResponse{
		ResponseHeader: &ua.ResponseHeader{
			Timestamp:          time.Now(),
			RequestHandle:      req.RequestHeader.RequestHandle,
			ServiceResult:      ua.StatusOK,
			ServiceDiagnostics: &ua.DiagnosticInfo{},
			StringTable:        []string{},
			AdditionalHeader:   ua.NewExtensionObject(nil),
		},
		Results:         results,
		DiagnosticInfos: []*ua.DiagnosticInfo{},
	}, nil

}

// https://reference.opcfoundation.org/Core/Part4/v105/docs/5.13.5
func (s *MonitoredItemService) SetTriggering(sc *uasc.SecureChannel, r ua.Request, reqID uint32) (ua.Response, error) {
	if s.SubService.srv.cfg.logger != nil {
		s.SubService.srv.cfg.logger.Debug("Handling %T", r)
	}

	req, err := safeReq[*ua.SetTriggeringRequest](r)
	if err != nil {
		return nil, err
	}
	return serviceUnsupported(req.RequestHeader), nil
}

// https://reference.opcfoundation.org/Core/Part4/v105/docs/5.13.6
func (s *MonitoredItemService) DeleteMonitoredItems(sc *uasc.SecureChannel, r ua.Request, reqID uint32) (ua.Response, error) {
	if s.SubService.srv.cfg.logger != nil {
		s.SubService.srv.cfg.logger.Debug("Handling %T", r)
	}

	req, err := safeReq[*ua.DeleteMonitoredItemsRequest](r)
	if err != nil {
		return nil, err
	}

	s.Mu.Lock()
	defer s.Mu.Unlock()

	sess := s.SubService.srv.Session(req.RequestHeader)

	results := make([]ua.StatusCode, len(req.MonitoredItemIDs))
	for i := range req.MonitoredItemIDs {
		id := req.MonitoredItemIDs[i]
		item, ok := s.Items[id]
		if !ok || item == nil || item.Sub == nil || item.Sub.Session == nil {
			results[i] = ua.StatusBadMonitoredItemIDInvalid
			continue
		}

		if sess == nil || item.Sub.Session.AuthTokenID == nil || sess.AuthTokenID == nil ||
			item.Sub.Session.AuthTokenID.String() != sess.AuthTokenID.String() {
			results[i] = ua.StatusBadSessionIDInvalid
			continue
		}

		if item.Sub.ID != req.SubscriptionID {
			results[i] = ua.StatusBadMonitoredItemIDInvalid
			continue
		}

		s.deleteMonitoredItemLocked(id)
		results[i] = ua.StatusOK
	}

	response := &ua.DeleteMonitoredItemsResponse{
		ResponseHeader: &ua.ResponseHeader{
			Timestamp:          time.Now(),
			RequestHandle:      req.RequestHeader.RequestHandle,
			ServiceResult:      ua.StatusOK,
			ServiceDiagnostics: &ua.DiagnosticInfo{},
			StringTable:        []string{},
			AdditionalHeader:   ua.NewExtensionObject(nil),
		},
		Results:         results,
		DiagnosticInfos: []*ua.DiagnosticInfo{},
	}
	return response, nil

}
