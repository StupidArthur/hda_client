package main

// This file owns the mocker's process lifetime. Both the CLI and the desktop
// application use RuntimeController, keeping one startup order and one set of
// transaction guarantees for HDA import, OPC UA startup and DA playback.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gopcua/opcua/ua"
)

const (
	phaseIdle       = "idle"
	phaseValidating = "validating"
	phaseImporting  = "importing"
	phaseStarting   = "starting"
	phaseRunning    = "running"
	phaseStopping   = "stopping"
	phaseFailed     = "failed"
)

// RuntimeSnapshot contains only values which are safe to poll from the GUI.
// It deliberately has no database handles or goroutine channels.
type RuntimeSnapshot struct {
	Phase          string              `json:"phase"`
	Message        string              `json:"message"`
	PresetRoot     string              `json:"presetRoot"`
	Endpoint       string              `json:"endpoint"`
	StartedAt      time.Time           `json:"startedAt"`
	HDAFilesDone   int                 `json:"hdaFilesDone"`
	HDAFilesTotal  int                 `json:"hdaFilesTotal"`
	HDASkipped     int                 `json:"hdaSkipped"`
	HDASamples     int64               `json:"hdaSamples"`
	HDAElapsedMS   int64               `json:"hdaElapsedMs"`
	ImportFile     string              `json:"importFile"`
	TagCount       int                 `json:"tagCount"`
	GoodCount      int                 `json:"goodCount"`
	UncertainCount int                 `json:"uncertainCount"`
	BadCount       int                 `json:"badCount"`
	WaitingCount   int                 `json:"waitingCount"`
	DATagCount     int                 `json:"daTagCount"`
	Playback       []PlaybackFileState `json:"playback"`
}

type DatasetSummary struct {
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`
	Rows          int64    `json:"rows"`
	Tags          []string `json:"tags"`
	TagCount      int      `json:"tagCount"`
	StatusColumns int      `json:"statusColumns"`
	HDAValueCols  int      `json:"hdaValueColumns"`
	HDAStatusCols int      `json:"hdaStatusColumns"`
	PeriodMS      int64    `json:"periodMs"`
}

type PresetSummary struct {
	Root       string           `json:"root"`
	ConfigPath string           `json:"configPath"`
	ConfigYAML string           `json:"configYaml"`
	Endpoint   string           `json:"endpoint"`
	Namespace  string           `json:"namespace"`
	Files      []DatasetSummary `json:"files"`
	Error      string           `json:"error"`
}

type TagSnapshot struct {
	Name       string    `json:"name"`
	DAValue    *float64  `json:"daValue"`
	DAQuality  uint32    `json:"daQuality"`
	DATime     time.Time `json:"daTime"`
	DAState    string    `json:"daState"`
	HDAValue   *float64  `json:"hdaValue"`
	HDAQuality uint32    `json:"hdaQuality"`
	HDATime    time.Time `json:"hdaTime"`
	HDAState   string    `json:"hdaState"`
}

type runtimeInstance struct {
	store       *Store
	playback    *Playback
	server      *mockServer
	cleanupStop chan struct{}
	closeOnce   sync.Once
}

func (r *runtimeInstance) Close() {
	if r == nil {
		return
	}
	r.closeOnce.Do(func() {
		close(r.cleanupStop)
		if r.server != nil {
			r.server.close()
		}
		if r.store != nil {
			if err := r.store.Close(); err != nil {
				log.Printf("close DuckDB: %v", err)
			}
		}
	})
}

// RuntimeController is the thread-safe entry point for user interfaces.
// Start and Stop return immediately; state is observed through Snapshot.
type RuntimeController struct {
	mu       sync.RWMutex
	snapshot RuntimeSnapshot
	instance *runtimeInstance
	cancel   context.CancelFunc
	done     chan struct{}
}

func NewRuntimeController() *RuntimeController {
	return &RuntimeController{snapshot: RuntimeSnapshot{Phase: phaseIdle, Message: "请选择预置目录"}}
}

func (c *RuntimeController) Snapshot() RuntimeSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := c.snapshot
	out.Playback = append([]PlaybackFileState(nil), c.snapshot.Playback...)
	if c.instance != nil && c.snapshot.Phase == phaseRunning {
		out.Playback = c.instance.playback.Snapshot()
		c.addTagStatistics(&out, c.instance)
	}
	return out
}

func (c *RuntimeController) addTagStatistics(out *RuntimeSnapshot, r *runtimeInstance) {
	tags, err := r.store.tags()
	if err != nil {
		return
	}
	out.TagCount = len(tags)
	out.GoodCount, out.UncertainCount, out.BadCount, out.WaitingCount = 0, 0, 0, 0
	for _, tag := range tags {
		sample, ok := r.playback.Current(tag.Name)
		if !ok && out.DATagCount == 0 {
			sample, err = r.store.latest(tag.Name)
			ok = err == nil && sample != nil
		}
		if !ok {
			out.WaitingCount++
			continue
		}
		switch qualityState(sample.Quality) {
		case "良好":
			out.GoodCount++
		case "不确定":
			out.UncertainCount++
		case "等待数据":
			out.WaitingCount++
		default:
			out.BadCount++
		}
	}
}

func (c *RuntimeController) update(fn func(*RuntimeSnapshot)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn(&c.snapshot)
}

// InspectPreset validates a selected root without creating or changing its
// runtime database. It uses an in-memory DuckDB connection for parquet schema
// checks, making directory selection safe before Start.
func (c *RuntimeController) InspectPreset(root string) PresetSummary {
	c.mu.RLock()
	busy := c.done != nil || (c.snapshot.Phase != phaseIdle && c.snapshot.Phase != phaseFailed)
	c.mu.RUnlock()
	if busy {
		return PresetSummary{Root: root, Error: "服务运行或切换中，停止后才能切换预置目录"}
	}
	return inspectPreset(root)
}

func inspectPreset(root string) PresetSummary {
	root = filepath.Clean(strings.TrimSpace(root))
	result := PresetSummary{Root: root, ConfigPath: filepath.Join(root, "config.yaml")}
	if root == "." || root == "" {
		result.Error = "请选择预置根目录"
		return result
	}
	configYAML, err := os.ReadFile(result.ConfigPath)
	if err != nil {
		result.Error = fmt.Sprintf("读取 config.yaml: %v", err)
		return result
	}
	result.ConfigYAML = string(configYAML)
	cfg, _, err := loadConfig(result.ConfigPath)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Endpoint, result.Namespace = cfg.Server.Endpoint, cfg.Server.Namespace
	store, err := openStore(":memory:")
	if err != nil {
		result.Error = fmt.Sprintf("打开校验数据库: %v", err)
		return result
	}
	defer store.Close()
	hda, da, err := discoverPreset(cfg, root, store)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Files = append(fileSummaries(hda, "HDA"), fileSummaries(da, "DA")...)
	return result
}

func fileSummaries(files []FileInfo, kind string) []DatasetSummary {
	out := make([]DatasetSummary, 0, len(files))
	for _, f := range files {
		out = append(out, DatasetSummary{Name: f.Name, Kind: kind, Rows: f.Rows, Tags: append([]string(nil), f.Tags...), TagCount: len(f.Tags), StatusColumns: len(f.StatusCols), HDAValueCols: len(f.HDAValueCols), HDAStatusCols: len(f.HDAStatusCols), PeriodMS: f.Period.Milliseconds()})
	}
	return out
}

// Start launches the lifecycle asynchronously. A cancellation during import
// is propagated to DuckDB and the file transaction rolls back as one unit.
func (c *RuntimeController) Start(root string) error {
	return c.StartConfig(filepath.Join(root, "config.yaml"))
}

// StartConfig preserves the CLI contract: --config may name any YAML file;
// its containing directory is the preset root.
func (c *RuntimeController) StartConfig(configPath string) error {
	root := filepath.Dir(filepath.Clean(configPath))
	c.mu.Lock()
	if c.done != nil || (c.snapshot.Phase != phaseIdle && c.snapshot.Phase != phaseFailed) {
		c.mu.Unlock()
		return fmt.Errorf("服务当前状态为 %s", c.snapshot.Phase)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.done = make(chan struct{})
	c.instance = nil
	c.snapshot = RuntimeSnapshot{Phase: phaseValidating, Message: "正在读取并校验配置", PresetRoot: root}
	done := c.done
	c.mu.Unlock()
	go c.start(ctx, filepath.Clean(configPath), done)
	return nil
}

func (c *RuntimeController) start(ctx context.Context, configPath string, done chan struct{}) {
	var runtime *runtimeInstance
	handedOff := false
	defer func() {
		if runtime != nil {
			runtime.Close()
		}
		if !handedOff {
			c.completeOperation(done)
		}
	}()
	cfg, root, err := loadConfig(configPath)
	if err != nil {
		c.finishStart(ctx, nil, err)
		return
	}
	runtimeDir := filepath.Join(root, "runtime")
	if err = os.MkdirAll(runtimeDir, 0755); err != nil {
		c.finishStart(ctx, nil, err)
		return
	}
	store, err := openStore(filepath.Join(runtimeDir, "history.duckdb"))
	if err != nil {
		c.finishStart(ctx, nil, fmt.Errorf("打开 DuckDB: %w", err))
		return
	}
	runtime = &runtimeInstance{store: store, cleanupStop: make(chan struct{})}
	hda, da, err := discoverPreset(cfg, root, store)
	if err != nil {
		c.finishStart(ctx, nil, err)
		return
	}
	c.update(func(s *RuntimeSnapshot) {
		s.Phase, s.Message = phaseImporting, "正在预存 HDA 数据"
		s.Endpoint, s.HDAFilesTotal = cfg.Server.Endpoint, len(hda)
	})
	imported, err := store.importFilesContext(ctx, hda, func(progress ImportResult, file FileInfo) {
		c.update(func(s *RuntimeSnapshot) {
			s.HDAFilesDone, s.HDASkipped, s.HDASamples = progress.Files+progress.Skipped, progress.Skipped, progress.Samples
			s.HDAElapsedMS, s.ImportFile = progress.Elapsed.Milliseconds(), file.Name
		})
	})
	if err != nil {
		c.finishStart(ctx, nil, err)
		return
	}
	c.update(func(s *RuntimeSnapshot) {
		s.HDAFilesDone, s.HDASkipped, s.HDASamples = imported.Files+imported.Skipped, imported.Skipped, imported.Samples
		s.HDAElapsedMS, s.ImportFile = imported.Elapsed.Milliseconds(), ""
		s.Phase, s.Message = phaseStarting, "正在注册位号并启动 OPC UA 服务"
	})
	if err = ctx.Err(); err != nil {
		c.finishStart(ctx, nil, err)
		return
	}
	if err = store.registerDA(da); err != nil {
		c.finishStart(ctx, nil, err)
		return
	}
	if err = store.cleanup(cfg.History.RetentionDays); err != nil {
		c.finishStart(ctx, nil, err)
		return
	}
	playback := newPlayback(store, da)
	runtime.playback = playback
	mock, err := newServer(cfg, store, playback)
	if err != nil {
		c.finishStart(ctx, nil, err)
		return
	}
	runtime.server = mock
	if err = playback.Start(); err != nil {
		c.finishStart(ctx, nil, err)
		return
	}
	if err = ctx.Err(); err != nil {
		c.finishStart(ctx, nil, err)
		return
	}
	if err = mock.Start(context.Background()); err != nil {
		c.finishStart(ctx, nil, err)
		return
	}
	go retentionLoop(runtime, cfg.History.RetentionDays)
	c.mu.Lock()
	if ctx.Err() != nil {
		c.mu.Unlock()
		c.finishStart(ctx, nil, ctx.Err())
		return
	}
	c.instance = runtime
	c.cancel = nil
	c.snapshot.Phase, c.snapshot.Message = phaseRunning, "服务运行中"
	c.snapshot.Endpoint, c.snapshot.StartedAt = cfg.Server.Endpoint, time.Now().UTC()
	c.snapshot.Playback = playback.Snapshot()
	c.snapshot.DATagCount = 0
	for _, file := range da {
		c.snapshot.DATagCount += len(file.Tags)
	}
	c.mu.Unlock()
	runtime = nil // controller now owns it.
	handedOff = true
	log.Printf("HDA Mocker 4 version=%s listening endpoint=%s ns=%d namespace=%s", version, cfg.Server.Endpoint, cfg.Server.NS, cfg.Server.Namespace)
}

func retentionLoop(r *runtimeInstance, days int) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if !r.server.history.Active() {
				if err := r.store.cleanup(days); err != nil {
					log.Printf("retention cleanup failed: %v", err)
				}
			}
		case <-r.cleanupStop:
			return
		}
	}
}

func (c *RuntimeController) finishStart(ctx context.Context, ignored *runtimeInstance, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.instance, c.cancel = nil, nil
	if errors.Is(err, context.Canceled) || ctx.Err() != nil {
		c.snapshot.Phase, c.snapshot.Message = phaseIdle, "已停止"
		return
	}
	c.snapshot.Phase, c.snapshot.Message = phaseFailed, err.Error()
}

// Stop is cooperative. For an active parquet import the query is cancelled,
// its transaction is rolled back, and no partially imported file is exposed.
func (c *RuntimeController) Stop() {
	c.mu.Lock()
	phase := c.snapshot.Phase
	if phase == phaseIdle || phase == phaseFailed || phase == phaseStopping {
		c.mu.Unlock()
		return
	}
	c.snapshot.Phase, c.snapshot.Message = phaseStopping, "正在安全停止服务"
	cancel, runtime, done := c.cancel, c.instance, c.done
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if runtime != nil {
		go func() {
			runtime.Close()
			c.mu.Lock()
			if c.instance == runtime {
				c.instance, c.cancel = nil, nil
				c.snapshot.Phase, c.snapshot.Message = phaseIdle, "已停止"
				c.snapshot.Playback = nil
			}
			c.mu.Unlock()
			c.completeOperation(done)
		}()
	}
}

// StopAndWait is used by the CLI and Wails shutdown callback so the process
// does not close DuckDB while a playback or import transaction is active.
func (c *RuntimeController) StopAndWait(timeout time.Duration) {
	c.Stop()
	c.mu.RLock()
	done := c.done
	c.mu.RUnlock()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

func (c *RuntimeController) completeOperation(done chan struct{}) {
	if done == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done == done {
		close(done)
		c.done = nil
	}
}

func (c *RuntimeController) TagSnapshots(query, filter string) []TagSnapshot {
	c.mu.RLock()
	runtime := c.instance
	running := c.snapshot.Phase == phaseRunning
	c.mu.RUnlock()
	if !running || runtime == nil {
		return nil
	}
	tags, err := runtime.store.tags()
	if err != nil {
		return nil
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	out := make([]TagSnapshot, 0, len(tags))
	for _, tag := range tags {
		if needle != "" && !strings.Contains(strings.ToLower(tag.Name), needle) {
			continue
		}
		row := TagSnapshot{Name: tag.Name, DAQuality: uint32(ua.StatusBadWaitingForInitialData), DAState: "等待数据"}
		if da, ok := runtime.playback.Current(tag.Name); ok {
			row.DAValue, row.DAQuality, row.DATime, row.DAState = da.Value, da.Quality, da.TS, qualityState(da.Quality)
		}
		if hda, err := runtime.store.latest(tag.Name); err == nil && hda != nil {
			row.HDAValue, row.HDAQuality, row.HDATime, row.HDAState = hda.Value, hda.Quality, hda.TS, qualityState(hda.Quality)
		} else {
			row.HDAQuality, row.HDAState = uint32(ua.StatusBadWaitingForInitialData), "等待数据"
		}
		if filter != "全部" && filter != "" && row.DAState != filter {
			continue
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func qualityState(status uint32) string {
	if status == uint32(ua.StatusBadWaitingForInitialData) {
		return "等待数据"
	}
	switch status & 0xC0000000 {
	case 0:
		return "良好"
	case 0x40000000:
		return "不确定"
	default:
		return "异常"
	}
}

func discoverPreset(cfg Config, root string, store *Store) ([]FileInfo, []FileInfo, error) {
	hdaPaths, err := listParquet(filepath.Join(root, "hda"))
	if err != nil {
		return nil, nil, err
	}
	daPaths, err := listParquet(filepath.Join(root, "da"))
	if err != nil {
		return nil, nil, err
	}
	if len(hdaPaths) == 0 && len(daPaths) == 0 {
		return nil, nil, fmt.Errorf("预置目录中没有 HDA 或 DA Parquet 文件")
	}
	hda, da := make([]FileInfo, 0, len(hdaPaths)), make([]FileInfo, 0, len(daPaths))
	hdaTags, daTags := map[string]bool{}, map[string]bool{}
	for _, path := range hdaPaths {
		f, err := validateParquet(store.db, path, true)
		if err != nil {
			return nil, nil, err
		}
		for _, tag := range f.Tags {
			if hdaTags[tag] {
				return nil, nil, fmt.Errorf("HDA 目录存在重复位号: %s", tag)
			}
			hdaTags[tag] = true
		}
		hda = append(hda, f)
	}
	for _, path := range daPaths {
		f, err := validateParquet(store.db, path, false)
		if err != nil {
			return nil, nil, err
		}
		period, ok := cfg.playbackPeriod(f.Name)
		if !ok {
			return nil, nil, fmt.Errorf("DA 文件 %s 未配置播放周期", f.Name)
		}
		f.Period = period
		for _, tag := range f.Tags {
			if daTags[tag] {
				return nil, nil, fmt.Errorf("DA 目录存在重复位号: %s", tag)
			}
			daTags[tag] = true
		}
		da = append(da, f)
	}
	if len(hda) > 0 && len(da) > 0 {
		var missingDA, missingHDA []string
		for tag := range hdaTags {
			if !daTags[tag] {
				missingDA = append(missingDA, tag)
			}
		}
		for tag := range daTags {
			if !hdaTags[tag] {
				missingHDA = append(missingHDA, tag)
			}
		}
		sort.Strings(missingDA)
		sort.Strings(missingHDA)
		if len(missingDA) > 0 || len(missingHDA) > 0 {
			return nil, nil, fmt.Errorf("HDA/DA 基础位号集合不一致: DA 缺少=%v, HDA 缺少=%v", missingDA, missingHDA)
		}
	}
	for name := range cfg.Playback.Files {
		found := false
		for _, file := range da {
			if file.Name == name {
				found = true
				break
			}
		}
		if !found {
			return nil, nil, fmt.Errorf("配置中存在未找到的 DA 文件: %s", name)
		}
	}
	return hda, da, nil
}
