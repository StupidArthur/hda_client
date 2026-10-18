package diagnostics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"mock_portal/internal/catalog"
)

const (
	StatePending       = "pending"
	StateOK            = "ok"
	StateDegraded      = "degraded"
	StateStale         = "stale"
	StateUnreachable   = "unreachable"
	StateUnmonitored   = "unmonitored"
	StateMisconfigured = "misconfigured"
)

// Config controls collection. Defaults follow the design document: poll every
// 10s, 3s timeout, detail cache 10s, stale after 30s, unreachable after 3
// consecutive failures.
type Config struct {
	Interval         time.Duration
	Timeout          time.Duration
	MaxConcurrent    int
	StaleAfter       time.Duration
	UnreachableAfter int
	DetailTTL        time.Duration
	MaxBodyBytes     int64
}

func DefaultConfig() Config {
	return Config{
		Interval:         10 * time.Second,
		Timeout:          3 * time.Second,
		MaxConcurrent:    4,
		StaleAfter:       30 * time.Second,
		UnreachableAfter: 3,
		DetailTTL:        10 * time.Second,
		MaxBodyBytes:     1 << 20,
	}
}

// Status separates portal collection health from the source-reported status.
type Status struct {
	State               string `json:"state"`
	SourceStatus        string `json:"source_status"`
	SourceMessage       string `json:"source_message"`
	LastAttempt         string `json:"last_attempt,omitempty"`
	LastSuccess         string `json:"last_success,omitempty"`
	LastError           string `json:"last_error,omitempty"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	FetchedAt           string `json:"fetched_at,omitempty"`
	Reason              string `json:"reason,omitempty"`
}

type detailEntry struct {
	info      map[string]any
	diag      map[string]any
	fetchedAt time.Time
	err       string
}

type presetState struct {
	target      catalog.DiagTarget
	attempts    int
	failures    int
	lastAttempt time.Time
	lastSuccess time.Time
	lastError   string
	diag        map[string]any
	fetchedAt   time.Time
	detail      *detailEntry
	detailBusy  bool
	detailDone  chan struct{}
}

// Manager fetches and caches diagnostics for discovered presets. It never
// writes to the mocks and never probes an address that was not derived from a
// preset config.
type Manager struct {
	cfg    Config
	client *http.Client
	now    func() time.Time

	mu     sync.Mutex
	states map[string]*presetState

	stopOnce sync.Once
	stop     chan struct{}
	wg       sync.WaitGroup
}

func New(cfg Config) *Manager {
	if cfg.Interval <= 0 {
		cfg.Interval = 10 * time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 3 * time.Second
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 4
	}
	if cfg.StaleAfter <= 0 {
		cfg.StaleAfter = 30 * time.Second
	}
	if cfg.UnreachableAfter <= 0 {
		cfg.UnreachableAfter = 3
	}
	if cfg.DetailTTL <= 0 {
		cfg.DetailTTL = 10 * time.Second
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = 1 << 20
	}
	return &Manager{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		now:    time.Now,
		states: make(map[string]*presetState),
		stop:   make(chan struct{}),
	}
}

// Run starts the background collector until Stop is called.
func (m *Manager) Run(cat *catalog.Catalog) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.collect(cat)
		ticker := time.NewTicker(m.cfg.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-m.stop:
				return
			case <-ticker.C:
				m.collect(cat)
			}
		}
	}()
}

func (m *Manager) Stop() {
	m.stopOnce.Do(func() { close(m.stop) })
	m.wg.Wait()
}

// CollectOnce performs a single collection pass; used by tests and the initial
// warm-up.
func (m *Manager) CollectOnce(cat *catalog.Catalog) { m.collect(cat) }

func (m *Manager) collect(cat *catalog.Catalog) {
	snap := cat.Snapshot(false)
	targets := make([]*catalog.Mock, 0, len(snap.Mocks))
	for _, mock := range snap.Mocks {
		m.ensure(mock)
		if mock.Diag.State == catalog.DiagMonitored && mock.Diag.URL != "" {
			targets = append(targets, mock)
		}
	}
	sem := make(chan struct{}, m.cfg.MaxConcurrent)
	var wg sync.WaitGroup
	for _, mock := range targets {
		mock := mock
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			m.fetchDiag(mock.ID, mock.Diag.URL)
		}()
	}
	wg.Wait()
}

// ensure registers a preset target and discards cached results if the derived
// address changed.
func (m *Manager) ensure(mock *catalog.Mock) *presetState {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.states[mock.ID]
	if !ok || state.target.URL != mock.Diag.URL || state.target.State != mock.Diag.State || state.target.Listen != mock.Diag.Listen {
		// A state object is one target generation, including A -> B -> A.
		// In-flight requests retain their own state and cannot publish into another.
		state = &presetState{target: mock.Diag}
		m.states[mock.ID] = state
	}
	return state
}

func (m *Manager) fetchDiag(id, baseURL string) {
	m.mu.Lock()
	state := m.states[id]
	if state == nil || state.target.URL != baseURL {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	diag, err := m.getJSON(baseURL + "/v1/diag")
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.states[id] != state {
		return
	}
	now := m.now().UTC()
	state.attempts++
	state.lastAttempt = now
	if err != nil {
		state.failures++
		state.lastError = err.Error()
		return
	}
	state.failures = 0
	state.lastError = ""
	state.lastSuccess = now
	state.fetchedAt = now
	state.diag = diag
}

func (m *Manager) getJSON(rawURL string) (map[string]any, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("诊断地址无效: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("诊断地址协议不支持")
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("诊断地址缺少主机")
	}
	ctx, cancel := context.WithTimeout(context.Background(), m.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求诊断接口失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("诊断接口返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, m.cfg.MaxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("读取诊断响应失败: %w", err)
	}
	if int64(len(body)) >= m.cfg.MaxBodyBytes {
		return nil, fmt.Errorf("诊断响应超过大小限制")
	}
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		return nil, fmt.Errorf("诊断响应不是有效 JSON: %w", err)
	}
	return value, nil
}

// Status describes the current collection state for a preset.
func (m *Manager) Status(mock *catalog.Mock) Status {
	state := m.ensure(mock)
	m.mu.Lock()
	defer m.mu.Unlock()
	status := Status{ConsecutiveFailures: state.failures}
	if !state.lastAttempt.IsZero() {
		status.LastAttempt = formatTime(state.lastAttempt)
	}
	if !state.lastSuccess.IsZero() {
		status.LastSuccess = formatTime(state.lastSuccess)
	}
	if !state.fetchedAt.IsZero() {
		status.FetchedAt = formatTime(state.fetchedAt)
	}
	status.LastError = state.lastError
	if value, ok := state.diag["status"].(string); ok {
		status.SourceStatus = value
	}
	if value, ok := state.diag["message"].(string); ok {
		status.SourceMessage = value
	}
	status.State = m.stateOf(mock, state, m.now())
	if status.State == StateMisconfigured || status.State == StateUnmonitored {
		status.Reason = mock.Diag.Reason
	}
	return status
}

func (m *Manager) stateOf(mock *catalog.Mock, state *presetState, now time.Time) string {
	if mock.Diag.State == catalog.DiagUnmonitored {
		return StateUnmonitored
	}
	if mock.Diag.State != catalog.DiagMonitored || mock.Diag.URL == "" {
		return StateMisconfigured
	}
	if state.attempts == 0 {
		return StatePending
	}
	if state.failures >= m.cfg.UnreachableAfter {
		return StateUnreachable
	}
	if state.lastSuccess.IsZero() {
		return StateDegraded
	}
	if now.Sub(state.lastSuccess) > m.cfg.StaleAfter {
		return StateStale
	}
	if state.failures > 0 {
		return StateDegraded
	}
	return StateOK
}

// Diag returns the last successfully collected /v1/diag payload.
func (m *Manager) Diag(mock *catalog.Mock) (map[string]any, time.Time) {
	state := m.ensure(mock)
	m.mu.Lock()
	defer m.mu.Unlock()
	if state == nil {
		return nil, time.Time{}
	}
	return cloneMap(state.diag), state.fetchedAt
}

// DetailResult is the cached response of /v1/detail.
type DetailResult struct {
	Info      map[string]any `json:"info"`
	Diag      map[string]any `json:"diag"`
	FetchedAt string         `json:"fetched_at,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// Detail returns /v1/detail for a preset, coalescing concurrent requests and
// reusing a short-lived cache.
func (m *Manager) Detail(ctx context.Context, mock *catalog.Mock) DetailResult {
	state := m.ensure(mock)
	if mock.Diag.State != catalog.DiagMonitored || mock.Diag.URL == "" {
		return DetailResult{Error: mock.Diag.Reason}
	}
	for {
		m.mu.Lock()
		if m.states[mock.ID] != state {
			m.mu.Unlock()
			return DetailResult{Error: "诊断地址已更新，请刷新"}
		}
		if state.detail != nil && m.detailFresh(state.detail) {
			result := detailToResult(state.detail)
			m.mu.Unlock()
			return result
		}
		if state.detailBusy {
			done := state.detailDone
			m.mu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return DetailResult{Error: ctx.Err().Error()}
			}
		}
		state.detailBusy = true
		state.detailDone = make(chan struct{})
		done := state.detailDone
		baseURL := mock.Diag.URL
		m.mu.Unlock()

		info, diag, err := m.fetchDetail(baseURL)
		now := m.now().UTC()
		m.mu.Lock()
		state.detailBusy = false
		state.detailDone = nil
		close(done)
		if m.states[mock.ID] == state {
			entry := &detailEntry{info: info, diag: diag, fetchedAt: now}
			if err != nil {
				entry.err = err.Error()
				if state.detail != nil {
					// Keep the last good detail visible, clearly marked.
					entry.info = state.detail.info
					entry.diag = state.detail.diag
				}
			} else {
				state.lastSuccess = now
				state.failures = 0
				state.lastError = ""
				state.diag = mergeDiag(state.diag, diag)
				state.fetchedAt = now
			}
			state.detail = entry
			result := detailToResult(entry)
			m.mu.Unlock()
			return result
		}
		m.mu.Unlock()
		return DetailResult{Error: "诊断地址已更新，请刷新"}
	}
}

func (m *Manager) fetchDetail(baseURL string) (map[string]any, map[string]any, error) {
	value, err := m.getJSON(baseURL + "/v1/detail")
	if err != nil {
		return nil, nil, err
	}
	info, _ := value["info"].(map[string]any)
	diag, _ := value["diag"].(map[string]any)
	if info == nil && diag == nil {
		return nil, nil, fmt.Errorf("/v1/detail 缺少 info/diag")
	}
	return info, diag, nil
}

func (m *Manager) detailFresh(entry *detailEntry) bool {
	if entry == nil {
		return false
	}
	ttl := m.cfg.DetailTTL
	if entry.err != "" {
		ttl = 2 * time.Second
	}
	return m.now().Sub(entry.fetchedAt) < ttl
}

func detailToResult(entry *detailEntry) DetailResult {
	result := DetailResult{
		Info:  cloneMap(entry.info),
		Diag:  cloneMap(entry.diag),
		Error: entry.err,
	}
	if !entry.fetchedAt.IsZero() {
		result.FetchedAt = formatTime(entry.fetchedAt)
	}
	return result
}

func mergeDiag(current, next map[string]any) map[string]any {
	if current == nil {
		return cloneMap(next)
	}
	merged := cloneMap(current)
	for key, value := range next {
		merged[key] = value
	}
	return merged
}

func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}
