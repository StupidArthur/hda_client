package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// File categories exposed by the portal.
const (
	CategoryHDA     = "hda"
	CategoryDA      = "da"
	CategoryConfig  = "config"
	CategorySummary = "summary"
)

// PlaybackFile mirrors playback.files entries without reinterpreting them.
type PlaybackFile struct {
	Name     string `json:"name"`
	PeriodMS int    `json:"period_ms"`
}

// Mock is one discovered preset directory.
type Mock struct {
	ID        string `json:"id"`
	Dir       string `json:"-"`
	Namespace string `json:"namespace"`
	NS        *int   `json:"ns,omitempty"`

	UA   Endpoint   `json:"ua"`
	Diag DiagTarget `json:"diag"`

	ConfigValid   bool   `json:"config_valid"`
	ConfigError   string `json:"config_error,omitempty"`
	ConfigModTime string `json:"config_modified_at,omitempty"`
	ConfigPath    string `json:"-"`

	SummaryExists  bool           `json:"summary_exists"`
	SummaryModTime string         `json:"summary_modified_at,omitempty"`
	SummaryPath    string         `json:"-"`
	SummarySize    int64          `json:"summary_size,omitempty"`
	Playback       []PlaybackFile `json:"playback"`
	HDACount       int            `json:"hda_count"`
	DACount        int            `json:"da_count"`
	RetentionDays  int            `json:"retention_days"`

	rawConfig string
}

// ConfigText returns the original config.yaml content.
func (m *Mock) ConfigText() string { return m.rawConfig }

// ConfigurationIssue returns a human-readable configuration problem, if any.
// It never invents a value for a missing setting.
func (m *Mock) ConfigurationIssue() string {
	if !m.ConfigValid {
		return m.ConfigError
	}
	if m.UA.Error != "" {
		return m.UA.Error
	}
	if m.Diag.Reason != "" {
		return m.Diag.Reason
	}
	return ""
}

// Snapshot is a read-only view of the preset root at a point in time.
type Snapshot struct {
	Root      string    `json:"root"`
	ScannedAt time.Time `json:"scanned_at"`
	Stale     bool      `json:"stale"`
	Error     string    `json:"error,omitempty"`
	Mocks     []*Mock   `json:"mocks"`
}

func (s *Snapshot) Find(id string) *Mock {
	if s == nil {
		return nil
	}
	for _, m := range s.Mocks {
		if m.ID == id {
			return m
		}
	}
	return nil
}

// Catalog discovers presets on demand and keeps the last good listing when the
// root becomes unreadable.
type Catalog struct {
	root    string
	exclude map[string]bool

	mu       sync.Mutex
	snapshot *Snapshot
	lastScan time.Time
	minAge   time.Duration
}

func New(root string, exclude []string) (*Catalog, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("presets_dir 未配置")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("presets_dir 无效: %w", err)
	}
	set := make(map[string]bool, len(exclude))
	for _, id := range exclude {
		if id = strings.TrimSpace(id); id != "" {
			set[id] = true
		}
	}
	return &Catalog{root: abs, exclude: set, minAge: 2 * time.Second}, nil
}

func (c *Catalog) Root() string { return c.root }

// Snapshot returns the current listing. When force is true file metadata is
// re-read immediately; otherwise a very short cache avoids rescanning on every
// request.
func (c *Catalog) Snapshot(force bool) *Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !force && c.snapshot != nil && time.Since(c.lastScan) < c.minAge {
		return c.snapshot
	}
	c.lastScan = time.Now()
	snap, err := c.scan()
	if err != nil {
		if c.snapshot != nil {
			stale := *c.snapshot
			stale.Stale = true
			stale.Error = err.Error()
			c.snapshot = &stale
			return &stale
		}
		c.snapshot = &Snapshot{
			Root:  c.root,
			Stale: true,
			Error: err.Error(),
			Mocks: []*Mock{},
		}
		return c.snapshot
	}
	c.snapshot = snap
	return snap
}

func (c *Catalog) scan() (*Snapshot, error) {
	entries, err := os.ReadDir(c.root)
	if err != nil {
		return nil, fmt.Errorf("读取 presets 根目录失败: %w", err)
	}
	snap := &Snapshot{Root: c.root, ScannedAt: time.Now().UTC(), Mocks: []*Mock{}}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if c.exclude[name] {
			continue
		}
		dir := filepath.Join(c.root, name)
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		configPath := filepath.Join(dir, "config.yaml")
		configInfo, err := os.Stat(configPath)
		if err != nil || !configInfo.Mode().IsRegular() {
			// Directories without config.yaml are plain material, not presets.
			continue
		}
		mock := &Mock{
			ID:            name,
			Dir:           dir,
			ConfigPath:    configPath,
			ConfigModTime: configInfo.ModTime().UTC().Format(time.RFC3339Nano),
			SummaryPath:   filepath.Join(dir, "summary.md"),
			Playback:      []PlaybackFile{},
		}
		if raw, err := os.ReadFile(configPath); err != nil {
			mock.ConfigError = fmt.Sprintf("读取 config.yaml 失败: %v", err)
		} else {
			mock.rawConfig = string(raw)
			cfg, parseErr := parseConfig(mock.rawConfig)
			if parseErr != nil {
				mock.ConfigError = parseErr.Error()
			} else {
				mock.ConfigValid = true
				mock.UA = parseEndpoint(cfg.Server.Endpoint)
				mock.Diag = deriveDiagTarget(cfg.Diagnostic.Listen, mock.UA)
				mock.Namespace = strings.TrimSpace(cfg.Server.Namespace)
				mock.NS = cfg.Server.NS
				mock.RetentionDays = cfg.History.RetentionDays
				for fileName, playback := range cfg.Playback.Files {
					mock.Playback = append(mock.Playback, PlaybackFile{Name: fileName, PeriodMS: playback.PeriodMS})
				}
				sort.Slice(mock.Playback, func(i, j int) bool { return mock.Playback[i].Name < mock.Playback[j].Name })
			}
		}
		if !mock.ConfigValid {
			// Keep a discoverable entry even when the config is broken, but do
			// not invent an address from a file we could not parse.
			if mock.UA.Raw == "" {
				mock.UA.Error = "配置解析失败，无法读取 server.endpoint"
			}
			mock.Diag.State = DiagInvalid
			mock.Diag.Reason = "配置解析失败，无法读取 diagnostic.listen"
		}
		if summaryInfo, err := os.Stat(mock.SummaryPath); err == nil && summaryInfo.Mode().IsRegular() {
			mock.SummaryExists = true
			mock.SummaryModTime = summaryInfo.ModTime().UTC().Format(time.RFC3339Nano)
			mock.SummarySize = summaryInfo.Size()
		}
		mock.HDACount = countParquet(filepath.Join(dir, "hda"))
		mock.DACount = countParquet(filepath.Join(dir, "da"))
		snap.Mocks = append(snap.Mocks, mock)
	}
	sort.Slice(snap.Mocks, func(i, j int) bool { return snap.Mocks[i].ID < snap.Mocks[j].ID })
	return snap, nil
}

func countParquet(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(entry.Name()), ".parquet") {
			count++
		}
	}
	return count
}
