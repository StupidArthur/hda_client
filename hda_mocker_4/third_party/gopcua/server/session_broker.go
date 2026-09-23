package server

import (
	mrand "math/rand"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/gopcua/opcua/ua"
	"github.com/gopcua/opcua/uasc"
)

type session struct {
	cfg sessionConfig

	ID                *ua.NodeID
	AuthTokenID       *ua.NodeID
	serverNonce       []byte
	remoteCertificate []byte
	RemoteAddress     string
	ApplicationName   string
	ApplicationURI    string
	ProductURI        string
	SessionName       string
	CreatedAt         time.Time
	ActivatedAt       time.Time

	PublishRequests chan PubReq
}

type SessionInfo struct {
	RemoteAddress   string
	ApplicationName string
	ApplicationURI  string
	ProductURI      string
	SessionName     string
	CreatedAt       time.Time
	ActivatedAt     time.Time
}

type sessionConfig struct {
	sessionTimeout time.Duration
}

type sessionBroker struct {
	// mu protects concurrent modification of s
	mu sync.Mutex

	// s contains all sessions watched by the session broker
	s                map[string]*session
	channels         map[*uasc.SecureChannel]map[string]struct{}
	bound            map[string]*uasc.SecureChannel
	orphanTimers     map[string]*time.Timer
	orphanGeneration map[string]uint64
	nextGeneration   uint64
	onExpire         func(*session)
	logger           Logger
}

func newSessionBroker(logger Logger) *sessionBroker {
	return &sessionBroker{
		s:                make(map[string]*session),
		channels:         make(map[*uasc.SecureChannel]map[string]struct{}),
		bound:            make(map[string]*uasc.SecureChannel),
		orphanTimers:     make(map[string]*time.Timer),
		orphanGeneration: make(map[string]uint64),
		logger:           logger,
	}
}

func (sb *sessionBroker) NewSession() *session {
	s := &session{
		ID:              ua.NewGUIDNodeID(1, uuid.New().String()),
		PublishRequests: make(chan PubReq, 100),
		CreatedAt:       time.Now().UTC(),
	}

	sb.mu.Lock()
	for {
		s.AuthTokenID = ua.NewNumericNodeID(0, uint32(mrand.Int31()))
		if sb.s[s.AuthTokenID.String()] == nil {
			break
		}
	}
	sb.s[s.AuthTokenID.String()] = s
	sb.mu.Unlock()

	return s
}

func (sb *sessionBroker) Infos() []SessionInfo {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	out := make([]SessionInfo, 0, len(sb.s))
	for _, s := range sb.s {
		out = append(out, SessionInfo{
			RemoteAddress: s.RemoteAddress, ApplicationName: s.ApplicationName,
			ApplicationURI: s.ApplicationURI, ProductURI: s.ProductURI,
			SessionName: s.SessionName, CreatedAt: s.CreatedAt, ActivatedAt: s.ActivatedAt,
		})
	}
	return out
}

func (sb *sessionBroker) Close(authToken *ua.NodeID) error {
	if authToken == nil {
		return ua.StatusBadSessionIDInvalid
	}
	sb.mu.Lock()
	defer sb.mu.Unlock()

	id := authToken.String()
	if sb.s[id] == nil {
		if sb.logger != nil {
			sb.logger.Warn("sessionBroker.Close: error looking up session %v", authToken)
		}
		return ua.StatusBadSessionIDInvalid
	}
	delete(sb.s, id)
	sb.unbindLocked(id)
	sb.cancelExpiryLocked(id)

	return nil
}

func (sb *sessionBroker) cancelExpiryLocked(id string) {
	if timer := sb.orphanTimers[id]; timer != nil {
		timer.Stop()
	}
	delete(sb.orphanTimers, id)
	delete(sb.orphanGeneration, id)
}

func (sb *sessionBroker) unbindLocked(id string) {
	channel := sb.bound[id]
	delete(sb.bound, id)
	if channel == nil {
		return
	}
	delete(sb.channels[channel], id)
	if len(sb.channels[channel]) == 0 {
		delete(sb.channels, channel)
	}
}

func (sb *sessionBroker) Bind(sess *session, channel *uasc.SecureChannel) {
	if sess == nil || channel == nil || sess.AuthTokenID == nil {
		return
	}
	sb.mu.Lock()
	defer sb.mu.Unlock()
	id := sess.AuthTokenID.String()
	if sb.s[id] != sess {
		return
	}
	sb.cancelExpiryLocked(id)
	sb.unbindLocked(id)
	if sb.channels[channel] == nil {
		sb.channels[channel] = make(map[string]struct{})
	}
	sb.channels[channel][id] = struct{}{}
	sb.bound[id] = channel
}

func (sb *sessionBroker) CloseChannel(channel *uasc.SecureChannel) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	for id := range sb.channels[channel] {
		if sess := sb.s[id]; sess != nil {
			sb.cancelExpiryLocked(id)
			timeout := sess.cfg.sessionTimeout
			if timeout <= 0 {
				timeout = sessionTimeoutDefault
			}
			sb.nextGeneration++
			generation := sb.nextGeneration
			sb.orphanGeneration[id] = generation
			sb.orphanTimers[id] = time.AfterFunc(timeout, func() { sb.expireOrphan(id, generation, sess) })
		}
		delete(sb.bound, id)
	}
	delete(sb.channels, channel)
}

func (sb *sessionBroker) expireOrphan(id string, generation uint64, sess *session) {
	sb.mu.Lock()
	if sb.orphanGeneration[id] != generation || sb.s[id] != sess || sb.bound[id] != nil {
		sb.mu.Unlock()
		return
	}
	delete(sb.s, id)
	sb.cancelExpiryLocked(id)
	onExpire := sb.onExpire
	sb.mu.Unlock()
	if onExpire != nil {
		onExpire(sess)
	}
}

func (sb *sessionBroker) Session(authToken *ua.NodeID) *session {
	if authToken == nil {
		return nil
	}
	sb.mu.Lock()
	defer sb.mu.Unlock()

	s := sb.s[authToken.String()]
	if s == nil {
		if sb.logger != nil {
			sb.logger.Warn("sessionBroker.Session: error looking up session %v", authToken)
		}
	}

	return s
}
