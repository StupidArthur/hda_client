package bindings

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"hda_client/internal/hda"
)

// QueryBinding 查询发起/取消/进度事件。
type QueryBinding struct {
	ctx     context.Context
	service *hda.Service
	cancel  context.CancelFunc
	mu      sync.Mutex
	active  bool
}

func NewQueryBinding(service *hda.Service) *QueryBinding {
	return &QueryBinding{service: service}
}

// Startup 注入 runtime context, 由 app 启动时调用。
func (b *QueryBinding) Startup(ctx context.Context) {
	b.ctx = ctx
}

// StartQuery 后台启动一次查询, 进度经事件 hda:progress 推送, 完成推 hda:done, 失败推 hda:error。
func (b *QueryBinding) StartQuery(cfg hda.QueryConfig) (string, error) {
	b.mu.Lock()
	if b.active {
		b.mu.Unlock()
		return "", hda.ErrBusy{}
	}
	parent := b.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	b.cancel = cancel
	b.active = true
	b.mu.Unlock()

	go func() {
		defer func() {
			b.mu.Lock()
			b.active = false
			b.cancel = nil
			b.mu.Unlock()
		}()
		results, err := b.service.RunQuery(ctx, cfg, func(p hda.QueryProgress) {
			if b.ctx != nil {
				runtime.EventsEmit(b.ctx, "hda:progress", p)
			}
		})
		if b.ctx == nil {
			return
		}
		if err != nil {
			if ctx.Err() != nil {
				runtime.EventsEmit(b.ctx, "hda:done", map[string]interface{}{"canceled": true})
			} else {
				runtime.EventsEmit(b.ctx, "hda:error", err.Error())
			}
			return
		}
		runtime.EventsEmit(b.ctx, "hda:done", results)
	}()

	return "running", nil
}

// CancelQuery 取消当前查询。
func (b *QueryBinding) CancelQuery() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cancel != nil {
		b.cancel()
	}
}

// 默认输出位置策略: exe 同级 data 目录 + 秒级时间戳文件名,
// 不指定输出时每次查询生成新文件, 避免覆盖历史结果。
const (
	defaultDataDirName       = "data"
	defaultParquetPrefix     = "history_"
	defaultParquetTimeLayout = "20060102_150405_000"
)

func exeDir() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	return "."
}

func defaultParquetPath() string {
	name := defaultParquetPrefix + time.Now().Format(defaultParquetTimeLayout) + ".parquet"
	return filepath.Join(exeDir(), defaultDataDirName, name)
}

// resolveOutputPath 前端展示的默认输出是相对路径(.\data\history_xxx.parquet),
// 统一以 exe 所在目录为基准解析, 避免受启动时工作目录影响。
func resolveOutputPath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(exeDir(), path)
}

// StartParquetQuery is the desktop-oriented query path. It emits the same
// lightweight progress event as StartQuery but never emits history points to
// JavaScript; only a ParquetResult is emitted when the file is complete.
// An empty path falls back to the default data directory location.
func (b *QueryBinding) StartParquetQuery(cfg hda.QueryConfig, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = defaultParquetPath()
	}
	path = resolveOutputPath(path)
	if filepath.Ext(path) == "" {
		path += ".parquet"
	}
	b.mu.Lock()
	if b.active {
		b.mu.Unlock()
		return "", hda.ErrBusy{}
	}
	parent := b.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	b.cancel, b.active = cancel, true
	b.mu.Unlock()

	go func() {
		defer func() {
			b.mu.Lock()
			b.active, b.cancel = false, nil
			b.mu.Unlock()
		}()
		started := time.Now()
		writer, err := hda.NewParquetWriter(path)
		if err != nil {
			if b.ctx != nil {
				runtime.EventsEmit(b.ctx, "hda:error", fmt.Sprintf("创建导出文件失败: %v", err))
			}
			return
		}
		defer writer.Abort()
		var good, bad, uncertain, noValue int64
		nodes, _, err := b.service.RunQueryEach(ctx, cfg, func(p hda.QueryProgress) {
			if b.ctx != nil {
				runtime.EventsEmit(b.ctx, "hda:progress", p)
			}
		}, func(result hda.TagResult) error {
			for _, point := range result.Points {
				if !point.HasValue {
					noValue++
				}
				switch {
				case strings.HasPrefix(point.Quality, "Good"):
					good++
				case strings.HasPrefix(point.Quality, "Uncertain"):
					uncertain++
				default:
					bad++
				}
			}
			return writer.Write(ctx, result)
		})
		if err == nil {
			var records int64
			records, err = writer.Close()
			if err == nil {
				elapsed := time.Since(started)
				result := hda.ParquetResult{Path: path, Nodes: nodes, Records: records, Good: good, Bad: bad, Uncertain: uncertain, NoValue: noValue, ElapsedMS: elapsed.Milliseconds()}
				if elapsed > 0 {
					result.Rate = float64(records) / elapsed.Seconds()
				}
				if b.ctx != nil {
					runtime.EventsEmit(b.ctx, "hda:parquet:done", result)
				}
				return
			}
		}
		if b.ctx == nil {
			return
		}
		if ctx.Err() != nil {
			runtime.EventsEmit(b.ctx, "hda:parquet:done", map[string]bool{"canceled": true})
		} else {
			runtime.EventsEmit(b.ctx, "hda:error", fmt.Sprintf("查询或导出失败: %v", err))
		}
	}()
	return "running", nil
}

func (b *QueryBinding) ReadParquetPage(path, node string, offset, limit int) (hda.ParquetPage, error) {
	return hda.ReadParquetPage(resolveOutputPath(strings.TrimSpace(path)), node, offset, limit)
}

func (b *QueryBinding) ReadParquetNodesPage(path string, nodes []string, offset, limit int) (hda.ParquetPage, error) {
	return hda.ReadParquetNodesPage(resolveOutputPath(strings.TrimSpace(path)), nodes, offset, limit)
}

func (b *QueryBinding) ReadParquetSummary(path string) (hda.ParquetSummary, error) {
	return hda.ReadParquetSummary(resolveOutputPath(strings.TrimSpace(path)))
}

func (b *QueryBinding) ReadParquetSelectedSummary(path string, nodes []string) (hda.ParquetSummary, error) {
	return hda.ReadParquetSelectedSummary(resolveOutputPath(strings.TrimSpace(path)), nodes)
}

func (b *QueryBinding) ReadParquetTrend(path string, nodes []string, maxPoints int) ([]hda.TrendSeries, error) {
	return hda.ReadParquetTrend(resolveOutputPath(strings.TrimSpace(path)), nodes, maxPoints)
}

func (b *QueryBinding) ReadParquetAnomalyPage(path, kind, nodeSearch string, offset, limit int) (hda.AnomalyPage, error) {
	return hda.ReadParquetAnomalyPage(resolveOutputPath(strings.TrimSpace(path)), kind, nodeSearch, offset, limit)
}

func (b *QueryBinding) ExpandTagExpression(source string) ([]string, error) {
	return hda.ExpandTagExpression(source)
}

func (b *QueryBinding) PreviewTagExpression(source string) (hda.TagExpressionPreview, error) {
	return hda.PreviewTagExpression(source)
}

// ParseTagCSV accepts file text from the WebView and applies Go's standard CSV
// parser so BOMs, header rows, and quoted commas behave exactly as documented.
func (b *QueryBinding) ParseTagCSV(content string) ([]string, error) {
	return hda.ParseCSVTags(strings.NewReader(content))
}

func (b *QueryBinding) ChooseParquetOutput() (string, error) {
	if b.ctx == nil {
		return "", fmt.Errorf("应用尚未启动")
	}
	// 手动选择时也定位到 data 目录并预填时间戳文件名, 与默认存储约定一致
	dir := filepath.Join(exeDir(), defaultDataDirName)
	_ = os.MkdirAll(dir, 0o755)
	return runtime.SaveFileDialog(b.ctx, runtime.SaveDialogOptions{
		Title:            "导出 Parquet",
		DefaultDirectory: dir,
		DefaultFilename:  defaultParquetPrefix + time.Now().Format(defaultParquetTimeLayout) + ".parquet",
		Filters:          []runtime.FileFilter{{DisplayName: "Parquet", Pattern: "*.parquet"}},
	})
}

func (b *QueryBinding) ChooseParquetFile() (string, error) {
	if b.ctx == nil {
		return "", fmt.Errorf("应用尚未启动")
	}
	dir := filepath.Join(exeDir(), defaultDataDirName)
	_ = os.MkdirAll(dir, 0o755)
	return runtime.OpenFileDialog(b.ctx, runtime.OpenDialogOptions{
		Title: "选择 Parquet 文件", DefaultDirectory: dir, Filters: []runtime.FileFilter{{DisplayName: "Parquet", Pattern: "*.parquet"}},
	})
}
