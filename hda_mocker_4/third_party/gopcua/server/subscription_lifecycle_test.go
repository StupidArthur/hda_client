package server

import (
	"testing"
	"time"

	"github.com/gopcua/opcua/ua"
	"github.com/gopcua/opcua/uasc"
)

func lifecycleServer() (*Server, *SubscriptionService, *MonitoredItemService, *session) {
	srv := &Server{cfg: &serverConfig{}, sb: newSessionBroker(nil)}
	subs := &SubscriptionService{srv: srv, Subs: make(map[uint32]*Subscription)}
	items := &MonitoredItemService{
		SubService: subs,
		Items:      make(map[uint32]*MonitoredItem),
		Nodes:      make(map[string][]*MonitoredItem),
		Subs:       make(map[uint32][]*MonitoredItem),
	}
	srv.SubscriptionService, srv.MonitoredItemService = subs, items
	return srv, subs, items, srv.sb.NewSession()
}

func TestSubscriptionIDsDoNotReuseGaps(t *testing.T) {
	_, subs, _, sess := lifecycleServer()
	create := func() uint32 {
		t.Helper()
		response, err := subs.CreateSubscription(nil, &ua.CreateSubscriptionRequest{
			RequestHeader:               &ua.RequestHeader{AuthenticationToken: sess.AuthTokenID},
			RequestedPublishingInterval: 1000,
			RequestedLifetimeCount:      1000,
			RequestedMaxKeepAliveCount:  100,
		}, 1)
		if err != nil {
			t.Fatal(err)
		}
		return response.(*ua.CreateSubscriptionResponse).SubscriptionID
	}
	first, middle, last := create(), create(), create()
	defer subs.DeleteSubscription(first)
	defer subs.DeleteSubscription(last)
	subs.DeleteSubscription(middle)
	next := create()
	defer subs.DeleteSubscription(next)
	if next <= last || subs.Subs[last] == nil {
		t.Fatalf("subscription ID reused: last=%d next=%d", last, next)
	}
}

func TestSubscriptionDeleteRemovesMonitoredItemsSynchronously(t *testing.T) {
	_, subs, items, sess := lifecycleServer()
	sub := NewSubscription()
	sub.ID, sub.Session = 7, sess
	subs.Subs[sub.ID] = sub
	node := ua.NewStringNodeID(3, "tag")
	item := &MonitoredItem{ID: 11, Sub: sub, Req: &ua.MonitoredItemCreateRequest{ItemToMonitor: &ua.ReadValueID{NodeID: node}}}
	items.Items[item.ID] = item
	items.Nodes[node.String()] = []*MonitoredItem{item}
	items.Subs[sub.ID] = []*MonitoredItem{item}
	subs.DeleteSubscription(sub.ID)
	if len(subs.Subs) != 0 || len(items.Items) != 0 || len(items.Nodes) != 0 || len(items.Subs) != 0 {
		t.Fatalf("subscription resources retained after deletion")
	}
}

func TestCloseSessionCleansSubscriptions(t *testing.T) {
	srv, subs, _, sess := lifecycleServer()
	sub := NewSubscription()
	sub.ID, sub.Session = 1, sess
	subs.Subs[sub.ID] = sub
	service := &SessionService{srv: srv}
	_, err := service.CloseSession(nil, &ua.CloseSessionRequest{RequestHeader: &ua.RequestHeader{AuthenticationToken: sess.AuthTokenID}, DeleteSubscriptions: true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if srv.sb.Session(sess.AuthTokenID) != nil || len(subs.Subs) != 0 {
		t.Fatal("closed session retained resources")
	}
}

func TestChannelRebindPreservesSession(t *testing.T) {
	sb := newSessionBroker(nil)
	sess := sb.NewSession()
	sess.cfg.sessionTimeout = 25 * time.Millisecond
	expired := make(chan *session, 1)
	sb.onExpire = func(s *session) { expired <- s }
	oldChannel, newChannel := new(uasc.SecureChannel), new(uasc.SecureChannel)
	sb.Bind(sess, oldChannel)
	sb.CloseChannel(oldChannel)
	sb.Bind(sess, newChannel)
	time.Sleep(60 * time.Millisecond)
	if sb.Session(sess.AuthTokenID) != sess {
		t.Fatal("old channel expired a rebound session")
	}
	sb.CloseChannel(newChannel)
	select {
	case closed := <-expired:
		if closed != sess || sb.Session(sess.AuthTokenID) != nil {
			t.Fatal("new channel did not expire its session")
		}
	case <-time.After(time.Second):
		t.Fatal("orphaned session was not expired")
	}
}

func TestNotificationsCoalesceWithoutBlocking(t *testing.T) {
	_, subs, _, _ := lifecycleServer()
	sub := NewSubscription()
	sub.srv = subs
	latest := &ua.MonitoredItemNotification{ClientHandle: 9}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			sub.Enqueue(&ua.MonitoredItemNotification{ClientHandle: 9})
		}
		sub.Enqueue(latest)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("notification producer blocked behind a slow subscriber")
	}
	if sub.PendingNotifications() != 1 || subs.coalescedTotal.Load() != 1000 {
		t.Fatalf("unexpected pending/coalesced counts: %d/%d", sub.PendingNotifications(), subs.coalescedTotal.Load())
	}
	queue := make(map[uint32]*ua.MonitoredItemNotification)
	sub.collectNotifications(queue)
	if queue[9] != latest {
		t.Fatal("the latest notification was not retained")
	}
	sub.cancel()
}

func TestLateSubscriptionRequestsReturnStatus(t *testing.T) {
	srv, subs, items, sess := lifecycleServer()
	sub := NewSubscription()
	sub.ID, sub.Session = 1, sess
	subs.Subs[sub.ID] = sub
	defer subs.DeleteSubscription(sub.ID)
	header := &ua.RequestHeader{AuthenticationToken: ua.NewStringNodeID(0, "closed-session")}

	response, err := subs.DeleteSubscriptions(nil, &ua.DeleteSubscriptionsRequest{
		RequestHeader: header, SubscriptionIDs: []uint32{sub.ID},
	}, 1)
	if err != nil || response.(*ua.DeleteSubscriptionsResponse).Results[0] != ua.StatusBadSessionIDInvalid {
		t.Fatalf("late delete response=%v error=%v", response, err)
	}
	_, err = items.CreateMonitoredItems(nil, &ua.CreateMonitoredItemsRequest{
		RequestHeader: header, SubscriptionID: sub.ID,
	}, 2)
	if err != ua.StatusBadSessionIDInvalid {
		t.Fatalf("late create monitored items error=%v", err)
	}
	if srv.sb.Session(header.AuthenticationToken) != nil {
		t.Fatal("unknown session became active")
	}
}
