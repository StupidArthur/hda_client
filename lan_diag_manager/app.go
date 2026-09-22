package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type ToolConfig struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Enabled bool   `json:"enabled"`
}

type Config struct {
	PollSeconds    int          `json:"pollSeconds"`
	TimeoutSeconds int          `json:"timeoutSeconds"`
	Tools          []ToolConfig `json:"tools"`
}

type ToolState struct {
	Name      string    `json:"name"`
	Summary   string    `json:"summary"`
	Endpoint  string    `json:"endpoint"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	CheckedAt time.Time `json:"checkedAt"`
	LatencyMS int64     `json:"latencyMs"`
}

type HistoryEvent struct {
	Time     time.Time `json:"time"`
	Endpoint string    `json:"endpoint"`
	Name     string    `json:"name"`
	From     string    `json:"from,omitempty"`
	To       string    `json:"to"`
	Message  string    `json:"message"`
}

type AppState struct {
	Version string      `json:"version"`
	Config  Config      `json:"config"`
	Tools   []ToolState `json:"tools"`
}

type App struct {
	mu          sync.RWMutex
	historyMu   sync.Mutex
	ctx         context.Context
	baseDir     string
	configPath  string
	historyPath string
	config      Config
	states      map[string]ToolState
	client      *DiagnosticClient
	stop        chan struct{}
	done        chan struct{}
}

func NewApp(baseDir string) (*App, error) {
	if baseDir == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		baseDir = filepath.Dir(exe)
	}
	a := &App{baseDir: baseDir, configPath: filepath.Join(baseDir, "lan_diag_manager.json"), historyPath: filepath.Join(baseDir, "lan_diag_history.jsonl"), states: map[string]ToolState{}, stop: make(chan struct{}), done: make(chan struct{})}
	if err := a.loadConfig(); err != nil {
		return nil, err
	}
	a.client = NewDiagnosticClient(time.Duration(a.config.TimeoutSeconds) * time.Second)
	go a.loop()
	return a, nil
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

func (a *App) Close() {
	select {
	case <-a.stop:
		return
	default:
		close(a.stop)
		<-a.done
	}
}

func defaultConfig() Config { return Config{PollSeconds: 10, TimeoutSeconds: 3, Tools: []ToolConfig{}} }

func (a *App) loadConfig() error {
	a.config = defaultConfig()
	b, err := os.ReadFile(a.configPath)
	if os.IsNotExist(err) {
		return a.writeConfig(a.config)
	}
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(b, &a.config); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	normalizeConfig(&a.config)
	return nil
}

func normalizeConfig(c *Config) {
	if c.PollSeconds < 2 {
		c.PollSeconds = 10
	}
	if c.TimeoutSeconds < 1 {
		c.TimeoutSeconds = 3
	}
	for i := range c.Tools {
		c.Tools[i].Host = strings.TrimSpace(c.Tools[i].Host)
		c.Tools[i].Name = strings.TrimSpace(c.Tools[i].Name)
	}
}

func (a *App) writeConfig(c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.configPath, append(b, '\n'), 0644)
}

func (a *App) GetState() AppState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	state := AppState{Version: version, Config: a.config}
	state.Config.Tools = append([]ToolConfig(nil), a.config.Tools...)
	for _, tool := range a.config.Tools {
		if !tool.Enabled {
			continue
		}
		endpoint := toolEndpoint(tool)
		if s, ok := a.states[endpoint]; ok {
			state.Tools = append(state.Tools, s)
		} else {
			state.Tools = append(state.Tools, ToolState{Name: tool.Name, Summary: tool.Summary, Endpoint: endpoint, Status: "pending", Message: "等待首次探查"})
		}
	}
	return state
}

func (a *App) SaveConfig(config Config) error {
	normalizeConfig(&config)
	seen := map[string]bool{}
	for _, tool := range config.Tools {
		if tool.Name == "" || tool.Host == "" || tool.Port < 1 || tool.Port > 65535 {
			return fmt.Errorf("工具名称、主机和有效端口不能为空")
		}
		key := toolEndpoint(tool)
		if seen[key] {
			return fmt.Errorf("诊断地址重复: %s", key)
		}
		seen[key] = true
	}
	if err := a.writeConfig(config); err != nil {
		return err
	}
	a.mu.Lock()
	a.config = config
	a.client = NewDiagnosticClient(time.Duration(config.TimeoutSeconds) * time.Second)
	a.mu.Unlock()
	go a.pollAll()
	return nil
}

func (a *App) PollNow() { go a.pollAll() }

func (a *App) FetchDetail(endpoint string) (map[string]map[string]any, error) {
	a.mu.RLock()
	client := a.client
	a.mu.RUnlock()
	return client.Detail(endpoint)
}

func (a *App) GetHistory(limit int) ([]HistoryEvent, error) {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	b, err := os.ReadFile(a.historyPath)
	if os.IsNotExist(err) {
		return []HistoryEvent{}, nil
	}
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	out := make([]HistoryEvent, 0, min(limit, len(lines)))
	for i := len(lines) - 1; i >= 0 && len(out) < limit; i-- {
		var e HistoryEvent
		if json.Unmarshal([]byte(lines[i]), &e) == nil {
			out = append(out, e)
		}
	}
	return out, nil
}

func (a *App) loop() {
	defer close(a.done)
	a.pollAll()
	for {
		a.mu.RLock()
		seconds := a.config.PollSeconds
		a.mu.RUnlock()
		timer := time.NewTimer(time.Duration(seconds) * time.Second)
		select {
		case <-timer.C:
			a.pollAll()
		case <-a.stop:
			timer.Stop()
			return
		}
	}
}

func (a *App) pollAll() {
	a.mu.RLock()
	tools := append([]ToolConfig(nil), a.config.Tools...)
	client := a.client
	a.mu.RUnlock()
	var wg sync.WaitGroup
	for _, tool := range tools {
		if !tool.Enabled {
			continue
		}
		tool := tool
		wg.Add(1)
		go func() {
			defer wg.Done()
			started := time.Now()
			endpoint := toolEndpoint(tool)
			diag, err := client.Diag(endpoint)
			state := ToolState{Name: tool.Name, Summary: tool.Summary, Endpoint: endpoint, CheckedAt: time.Now(), LatencyMS: time.Since(started).Milliseconds()}
			if err != nil {
				state.Status, state.Message = "offline", err.Error()
			} else {
				state.Status, _ = diag["status"].(string)
				state.Message, _ = diag["message"].(string)
				if state.Status != "ok" && state.Status != "warn" && state.Status != "error" {
					state.Status, state.Message = "error", "无效的诊断响应"
				}
			}
			a.applyState(state)
		}()
	}
	wg.Wait()
}

func (a *App) applyState(state ToolState) {
	a.mu.Lock()
	old, exists := a.states[state.Endpoint]
	a.states[state.Endpoint] = state
	ctx := a.ctx
	a.mu.Unlock()
	if !exists || old.Status != state.Status {
		e := HistoryEvent{Time: state.CheckedAt, Endpoint: state.Endpoint, Name: state.Name, To: state.Status, Message: state.Message}
		if exists {
			e.From = old.Status
		}
		_ = a.appendHistory(e)
		if ctx != nil {
			runtime.EventsEmit(ctx, "status-change", e)
		}
	}
}

func (a *App) appendHistory(event HistoryEvent) error {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	b, _ := json.Marshal(event)
	f, err := os.OpenFile(a.historyPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

func toolEndpoint(t ToolConfig) string { return fmt.Sprintf("%s:%d", t.Host, t.Port) }

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
