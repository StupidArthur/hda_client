package server

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gopcua/opcua/ua"
	"github.com/gopcua/opcua/uasc"
)

// SubscriptionService implements the Subscription Service Set.
//
// https://reference.opcfoundation.org/Core/Part4/v105/docs/5.13
type SubscriptionService struct {
	srv *Server
	// pub sub stuff
	Mu             sync.Mutex
	Subs           map[uint32]*Subscription
	nextID         uint32
	coalescedTotal atomic.Uint64
	lastCoalesced  atomic.Int64
}

type SubscriptionStats struct {
	Subscriptions        int
	MonitoredItems       int
	PendingNotifications int
	CoalescedTotal       uint64
	LastCoalesced        time.Time
}

func (s *SubscriptionService) Stats() SubscriptionStats {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	stats := SubscriptionStats{Subscriptions: len(s.Subs), CoalescedTotal: s.coalescedTotal.Load()}
	if last := s.lastCoalesced.Load(); last != 0 {
		stats.LastCoalesced = time.Unix(0, last).UTC()
	}
	for _, sub := range s.Subs {
		stats.PendingNotifications += sub.PendingNotifications()
	}
	if s.srv.MonitoredItemService != nil {
		s.srv.MonitoredItemService.Mu.Lock()
		stats.MonitoredItems = len(s.srv.MonitoredItemService.Items)
		s.srv.MonitoredItemService.Mu.Unlock()
	}
	return stats
}

func (s *SubscriptionService) recordCoalesced() {
	s.coalescedTotal.Add(1)
	s.lastCoalesced.Store(time.Now().UTC().UnixNano())
}

// get rid of all references to a subscription and all monitored items that are pointed at this subscription.
func (s *SubscriptionService) DeleteSubscription(id uint32) {
	s.Mu.Lock()
	sub, ok := s.Subs[id]
	if !ok {
		s.Mu.Unlock()
		return
	}
	delete(s.Subs, id)
	sub.Mu.Lock()
	if sub.running {
		sub.running = false
		close(sub.shutdown)
	}
	sub.cancel()
	sub.Mu.Unlock()

	// CreateMonitoredItems uses the same service-then-items lock order. Keep
	// the service lock until the associated items have been removed.
	s.srv.MonitoredItemService.DeleteSub(id)
	s.Mu.Unlock()
}

func (s *SubscriptionService) DeleteSessionSubscriptions(session *session) {
	if session == nil {
		return
	}
	s.Mu.Lock()
	ids := make([]uint32, 0)
	for id, sub := range s.Subs {
		if sub != nil && sub.Session == session {
			ids = append(ids, id)
		}
	}
	s.Mu.Unlock()
	for _, id := range ids {
		s.DeleteSubscription(id)
	}
}

// https://reference.opcfoundation.org/Core/Part4/v105/docs/5.13.2
func (s *SubscriptionService) CreateSubscription(sc *uasc.SecureChannel, r ua.Request, reqID uint32) (ua.Response, error) {
	if s.srv.cfg.logger != nil {
		s.srv.cfg.logger.Debug("Handling %T", r)
	}

	req, err := safeReq[*ua.CreateSubscriptionRequest](r)
	if err != nil {
		return nil, err
	}

	session := s.srv.Session(req.RequestHeader)
	if session == nil {
		return nil, ua.StatusBadSessionIDInvalid
	}
	s.Mu.Lock()
	defer s.Mu.Unlock()

	s.nextID++
	if s.nextID == 0 {
		return nil, ua.StatusBadTooManySubscriptions
	}
	newsubid := s.nextID

	if s.srv.cfg.logger != nil {
		s.srv.cfg.logger.Info("New Sub %d for %v", newsubid, sc.RemoteAddr())
	}

	sub := NewSubscription()
	sub.srv = s
	sub.Session = session
	sub.Channel = sc
	sub.ID = newsubid
	sub.RevisedPublishingInterval = req.RequestedPublishingInterval
	sub.RevisedLifetimeCount = req.RequestedLifetimeCount
	sub.RevisedMaxKeepAliveCount = req.RequestedMaxKeepAliveCount

	s.Subs[newsubid] = sub
	sub.running = true
	sub.Start()

	resp := &ua.CreateSubscriptionResponse{
		ResponseHeader: &ua.ResponseHeader{
			Timestamp:          time.Now(),
			RequestHandle:      req.RequestHeader.RequestHandle,
			ServiceResult:      ua.StatusOK,
			ServiceDiagnostics: &ua.DiagnosticInfo{},
			StringTable:        []string{},
			AdditionalHeader:   ua.NewExtensionObject(nil),
		},
		SubscriptionID:            uint32(newsubid),
		RevisedPublishingInterval: req.RequestedPublishingInterval,
		RevisedLifetimeCount:      req.RequestedLifetimeCount,
		RevisedMaxKeepAliveCount:  req.RequestedMaxKeepAliveCount,
	}
	return resp, nil
}

// https://reference.opcfoundation.org/Core/Part4/v105/docs/5.13.3
func (s *SubscriptionService) ModifySubscription(sc *uasc.SecureChannel, r ua.Request, reqID uint32) (ua.Response, error) {
	if s.srv.cfg.logger != nil {
		s.srv.cfg.logger.Debug("Handling %T", r)
	}

	req, err := safeReq[*ua.ModifySubscriptionRequest](r)
	if err != nil {
		return nil, err
	}

	// When this gets implemented, be sure to check the subscription session vs the request session!

	return serviceUnsupported(req.RequestHeader), nil
}

// https://reference.opcfoundation.org/Core/Part4/v105/docs/5.13.4
func (s *SubscriptionService) SetPublishingMode(sc *uasc.SecureChannel, r ua.Request, reqID uint32) (ua.Response, error) {
	if s.srv.cfg.logger != nil {
		s.srv.cfg.logger.Debug("Handling %T", r)
	}

	req, err := safeReq[*ua.SetPublishingModeRequest](r)
	if err != nil {
		return nil, err
	}
	// When this gets implemented, be sure to check the subscription session vs the request session!
	return serviceUnsupported(req.RequestHeader), nil
}

// https://reference.opcfoundation.org/Core/Part4/v105/docs/5.13.5
func (s *SubscriptionService) Publish(sc *uasc.SecureChannel, r ua.Request, reqID uint32) (ua.Response, error) {
	if s.srv.cfg.logger != nil {
		s.srv.cfg.logger.Debug("Raw Publish req")
	}

	req, err := safeReq[*ua.PublishRequest](r)
	if err != nil {
		if s.srv.cfg.logger != nil {
			s.srv.cfg.logger.Error("ERROR: bad PublishRequest Struct")
		}
		return nil, err
	}

	session := s.srv.Session(req.RequestHeader)

	if session == nil {
		response := &ua.PublishResponse{
			ResponseHeader: &ua.ResponseHeader{
				Timestamp:          time.Now(),
				RequestHandle:      req.RequestHeader.RequestHandle,
				ServiceResult:      ua.StatusBadSessionIDInvalid,
				ServiceDiagnostics: &ua.DiagnosticInfo{},
				StringTable:        []string{},
				AdditionalHeader:   ua.NewExtensionObject(nil),
			},
			SubscriptionID:           0,
			MoreNotifications:        false,
			NotificationMessage:      &ua.NotificationMessage{NotificationData: []*ua.ExtensionObject{}},
			AvailableSequenceNumbers: []uint32{}, // an empty array indicates taht we don't support retransmission of messages
			Results:                  []ua.StatusCode{},
			DiagnosticInfos:          []*ua.DiagnosticInfo{},
		}

		return response, nil
	}

	select {
	case session.PublishRequests <- PubReq{Req: req, ID: reqID}:
	default:
		if s.srv.cfg.logger != nil {
			s.srv.cfg.logger.Warn("Too many publish reqs.")
		}
	}

	// per opcua spec, we don't respond now.  When data is available on the subscription,
	// the Subscription will respond in the background.
	return nil, nil
}

// https://reference.opcfoundation.org/Core/Part4/v105/docs/5.13.6
func (s *SubscriptionService) Republish(sc *uasc.SecureChannel, r ua.Request, reqID uint32) (ua.Response, error) {
	if s.srv.cfg.logger != nil {
		s.srv.cfg.logger.Debug("Handling %T", r)
	}

	req, err := safeReq[*ua.RepublishRequest](r)
	if err != nil {
		return nil, err
	}
	return serviceUnsupported(req.RequestHeader), nil
}

// https://reference.opcfoundation.org/Core/Part4/v105/docs/5.13.7
func (s *SubscriptionService) TransferSubscriptions(sc *uasc.SecureChannel, r ua.Request, reqID uint32) (ua.Response, error) {
	if s.srv.cfg.logger != nil {
		s.srv.cfg.logger.Debug("Handling %T", r)
	}

	req, err := safeReq[*ua.TransferSubscriptionsRequest](r)
	if err != nil {
		return nil, err
	}
	// When this gets implemented, be sure to check the subscription session vs the request session!
	return serviceUnsupported(req.RequestHeader), nil
}

// https://reference.opcfoundation.org/Core/Part4/v105/docs/5.13.8
func (s *SubscriptionService) DeleteSubscriptions(sc *uasc.SecureChannel, r ua.Request, reqID uint32) (ua.Response, error) {
	if s.srv.cfg.logger != nil {
		s.srv.cfg.logger.Debug("Handling %T", r)
	}

	req, err := safeReq[*ua.DeleteSubscriptionsRequest](r)
	if err != nil {
		return nil, err
	}
	session := s.srv.Session(req.Header())

	results := make([]ua.StatusCode, len(req.SubscriptionIDs))
	for i := range req.SubscriptionIDs {

		subid := req.SubscriptionIDs[i]
		if s.srv.cfg.logger != nil {
			s.srv.cfg.logger.Info("Subscription %d deleted by client", subid)
		}
		s.Mu.Lock()
		sub, ok := s.Subs[subid]
		if !ok {
			s.Mu.Unlock()
			results[i] = ua.StatusBadSubscriptionIDInvalid
			continue
		}
		if session == nil || sub.Session != session {
			s.Mu.Unlock()
			results[i] = ua.StatusBadSessionIDInvalid
			continue
		}
		s.Mu.Unlock()
		s.DeleteSubscription(subid)
		results[i] = ua.StatusOK
	}
	return &ua.DeleteSubscriptionsResponse{
		ResponseHeader: &ua.ResponseHeader{
			Timestamp:          time.Now(),
			RequestHandle:      req.RequestHeader.RequestHandle,
			ServiceResult:      ua.StatusOK,
			ServiceDiagnostics: &ua.DiagnosticInfo{},
			StringTable:        []string{},
			AdditionalHeader:   ua.NewExtensionObject(nil),
		},
		Results:         results,                //                  []StatusCode
		DiagnosticInfos: []*ua.DiagnosticInfo{}, //          []*DiagnosticInfo
	}, nil
}

type PubReq struct {
	// The data of the publish request
	Req *ua.PublishRequest

	// The request ID (from the header) of the publish request.  This has to be used when replying.
	ID uint32
}

// This is the type that with its run() function will work in the bakground fullfilling subscription
// publishes.
//
// MonitoredItems enqueue the latest value per client handle for publishing.
type Subscription struct {
	srv                       *SubscriptionService
	Session                   *session
	ID                        uint32
	RevisedPublishingInterval float64
	RevisedLifetimeCount      uint32
	RevisedMaxKeepAliveCount  uint32
	Channel                   *uasc.SecureChannel
	SequenceID                uint32
	//SeqNums                   map[uint32]struct{}
	T *time.Ticker

	notifyMu          sync.Mutex
	pending           map[uint32]*ua.MonitoredItemNotification
	notifyWake        chan struct{}
	publishQueueDepth atomic.Int64
	ModifyChannel     chan *ua.ModifySubscriptionRequest

	// the running flag and shutdown channel are used to signal the background task that it should stop.
	// multiple places can kill the subscription so make sure you check the running flag using the mutex
	// before closing the shutdown channel.
	Mu       sync.Mutex
	running  bool
	shutdown chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewSubscription() *Subscription {
	ctx, cancel := context.WithCancel(context.Background())
	return &Subscription{
		//SeqNums:       map[uint32]struct{}{},
		pending:       make(map[uint32]*ua.MonitoredItemNotification),
		notifyWake:    make(chan struct{}, 1),
		ModifyChannel: make(chan *ua.ModifySubscriptionRequest, 2),
		shutdown:      make(chan struct{}),
		ctx:           ctx,
		cancel:        cancel,
	}
}

func (s *Subscription) Enqueue(notification *ua.MonitoredItemNotification) {
	if notification == nil {
		return
	}
	select {
	case <-s.shutdown:
		return
	default:
	}
	s.notifyMu.Lock()
	if _, exists := s.pending[notification.ClientHandle]; exists && s.srv != nil {
		s.srv.recordCoalesced()
	}
	s.pending[notification.ClientHandle] = notification
	s.notifyMu.Unlock()
	select {
	case s.notifyWake <- struct{}{}:
	default:
	}
}

func (s *Subscription) PendingNotifications() int {
	s.notifyMu.Lock()
	pending := len(s.pending)
	s.notifyMu.Unlock()
	return pending + int(s.publishQueueDepth.Load())
}

func (s *Subscription) collectNotifications(queue map[uint32]*ua.MonitoredItemNotification) {
	s.notifyMu.Lock()
	pending := s.pending
	s.pending = make(map[uint32]*ua.MonitoredItemNotification)
	s.notifyMu.Unlock()
	for handle, notification := range pending {
		if _, exists := queue[handle]; exists && s.srv != nil {
			s.srv.recordCoalesced()
		}
		queue[handle] = notification
	}
	s.publishQueueDepth.Store(int64(len(queue)))
}

func (s *Subscription) Update(req *ua.ModifySubscriptionRequest) {
	s.RevisedPublishingInterval = req.RequestedPublishingInterval
	s.RevisedLifetimeCount = req.RequestedLifetimeCount
	s.RevisedMaxKeepAliveCount = req.RequestedMaxKeepAliveCount
}

func (s *Subscription) Start() {
	go s.run()

}

func (s *Subscription) keepalive(pubreq PubReq) error {
	eo := make([]*ua.ExtensionObject, 0)

	msg := ua.NotificationMessage{
		SequenceNumber:   s.SequenceID + 1, // not sure why but ua expert wants the next sequence number on keepalives.
		PublishTime:      time.Now(),
		NotificationData: eo,
	}

	response := &ua.PublishResponse{
		ResponseHeader: &ua.ResponseHeader{
			Timestamp:          time.Now(),
			RequestHandle:      pubreq.Req.RequestHeader.RequestHandle,
			ServiceResult:      ua.StatusOK,
			ServiceDiagnostics: &ua.DiagnosticInfo{},
			StringTable:        []string{},
			AdditionalHeader:   ua.NewExtensionObject(nil),
		},
		SubscriptionID:           s.ID,
		MoreNotifications:        false,
		NotificationMessage:      &msg,
		AvailableSequenceNumbers: []uint32{}, // an empty array indicates taht we don't support retransmission of messages
		Results:                  []ua.StatusCode{},
		DiagnosticInfos:          []*ua.DiagnosticInfo{},
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	err := s.Channel.SendResponseWithContext(ctx, pubreq.ID, response)
	if err != nil {
		return err
	}
	return nil
}

// this function should be run as a go-routine and will handle sending data out
// to the client at the correct rate assuming there are publish requests queued up.
// if the function returns it deletes the subscription
func (s *Subscription) run() {
	defer s.publishQueueDepth.Store(0)
	// if this go routine dies, we need to delete ourselves.
	defer func() {
		if s.srv.srv.cfg.logger != nil {
			s.srv.srv.cfg.logger.Info("Subscription %d shutting down.", s.ID)
		}
		s.srv.DeleteSubscription(s.ID)
	}()

	keepalive_counter := 0
	lifetime_counter := 0
	//TODO: if a sub is modified, this ticker time may need to change.
	s.T = time.NewTicker(time.Millisecond * time.Duration(s.RevisedPublishingInterval))
	defer s.T.Stop()

	// This is the master run event loop.  It has effectively 3 states that it can be in.  The first two are designated with
	// the labels L0, and L2.  Everything after the L2 loop is the third state where we send any pending notifications.
	// The states always go L0 -> L2 -> Sending -> L0.  L0 and L2 are both places where we wait so they are done as for loops with
	// breaks to go to the next state.
	// The sending state always runs to completion.
	//
	// L0 waits for our notification interval to expire.  Any notifications that come in
	// while waiting will be stored in the publishQueue.  Once the interval expires, we'll move on to L2 if we've got notifications.
	// In L2 we wait for a publish request.  If we get one, we'll publish the notifications in the publishQueue.  If we don't
	// get a publish request, we'll continue to count intervals without a publish request.
	//
	// In L0 and L2, If we get to the lifetime count without a publish request, we'll kill the subscription.
	for {
		// we don't need to do anything if we don't have at least one thing to publish so lets get that first
		publishQueue := make(map[uint32]*ua.MonitoredItemNotification)

		// Collect notifications until our publication interval is ready
	L0:
		for {
			select {
			case <-s.shutdown:
				return
			case <-s.notifyWake:
				s.collectNotifications(publishQueue)
			case <-s.T.C:
				if len(publishQueue) == 0 {
					// nothing to publish, increment the keepalive counter and send a keepalive if it
					// has been enough intervals.
					keepalive_counter++
					if keepalive_counter > int(s.RevisedMaxKeepAliveCount) {
						keepalive_counter = 0
						select {
						case pubreq := <-s.Session.PublishRequests:
							err := s.keepalive(pubreq)
							if err != nil {
								if s.srv.srv.cfg.logger != nil {
									s.srv.srv.cfg.logger.Warn("problem sending keepalive to subscription #%d: %v", s.ID, err)
								}
								return
							}
						default:
							lifetime_counter++
							if lifetime_counter > int(s.RevisedLifetimeCount) {
								if s.srv.srv.cfg.logger != nil {
									s.srv.srv.cfg.logger.Warn("Subscription #%d timed out.", s.ID)
								}
								return
							}
						}
					}
					continue // nothing to publish this interval
				}
				// we have things to publish so we'll break out to do that.
				break L0
			case update := <-s.ModifyChannel:
				s.Update(update)
			}
		}
		var pubreq PubReq

		// now we need to continue to collect notifications until we've got a publish request
	L2:
		for {
			select {
			case <-s.shutdown:
				return
			case pubreq = <-s.Session.PublishRequests:
				// once we get a publish request, we should move on to publish them back
				break L2
			case <-s.notifyWake:
				s.collectNotifications(publishQueue)

			case <-s.T.C:
				// we had another tick without a publish request.
				lifetime_counter++
				if lifetime_counter > int(s.RevisedLifetimeCount) {
					if s.srv.srv.cfg.logger != nil {
						s.srv.srv.cfg.logger.Warn("Subscription %d timed out.", s.ID)
					}
					return
				}
			}
		}
		lifetime_counter = 0
		keepalive_counter = 0
		s.collectNotifications(publishQueue)

		s.SequenceID++
		if s.SequenceID == 0 {
			// per the spec, the sequence ID cannot be 0
			s.SequenceID = 1
		}
		if s.srv.srv.cfg.logger != nil {
			s.srv.srv.cfg.logger.Debug("Got publish req on sub #%d.  Sequence %d", s.ID, s.SequenceID)
		}
		// then get all the tags and send them back to the client

		//for x := range pubreq.Req.SubscriptionAcknowledgements {
		//a := pubreq.Req.SubscriptionAcknowledgements[x]
		//delete(s.SeqNums, a.SequenceNumber)
		//}

		final_items := make([]*ua.MonitoredItemNotification, len(publishQueue))
		i := 0
		for k := range publishQueue {
			final_items[i] = publishQueue[k]
			i++
		}

		dcn := ua.DataChangeNotification{
			MonitoredItems:  final_items,
			DiagnosticInfos: []*ua.DiagnosticInfo{},
		}
		eo := make([]*ua.ExtensionObject, 1)
		eo[0] = ua.NewExtensionObject(&dcn)
		eo[0].UpdateMask()

		msg := ua.NotificationMessage{
			SequenceNumber:   s.SequenceID,
			PublishTime:      time.Now(),
			NotificationData: eo,
		}
		//s.SeqNums[s.SequenceID] = struct{}{}

		response := &ua.PublishResponse{
			ResponseHeader: &ua.ResponseHeader{
				Timestamp:          time.Now(),
				RequestHandle:      pubreq.Req.RequestHeader.RequestHandle,
				ServiceResult:      ua.StatusOK,
				ServiceDiagnostics: &ua.DiagnosticInfo{},
				StringTable:        []string{},
				AdditionalHeader:   ua.NewExtensionObject(nil),
			},
			SubscriptionID:           s.ID,
			MoreNotifications:        false,
			NotificationMessage:      &msg,
			AvailableSequenceNumbers: []uint32{}, // an empty array indicates taht we don't support retransmission of messages
			Results:                  []ua.StatusCode{},
			DiagnosticInfos:          []*ua.DiagnosticInfo{},
		}
		ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
		err := s.Channel.SendResponseWithContext(ctx, pubreq.ID, response)
		cancel()
		if err != nil {
			if s.srv.srv.cfg.logger != nil {
				s.srv.srv.cfg.logger.Error("problem sending channel response: %v", err)
				s.srv.srv.cfg.logger.Error("Killing subscription %d", s.ID)
			}
			return
		}
		if s.srv.srv.cfg.logger != nil {
			s.srv.srv.cfg.logger.Debug("Published %d items OK for %d", len(publishQueue), s.ID)
		}
		s.publishQueueDepth.Store(0)
		// wait till we've got a publish request.
	}
}

//PublishRequest_Encoding_DefaultBinary
