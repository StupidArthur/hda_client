package main

import (
	"context"
	"io"
	"log"
	"os"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the sole Wails binding. Its exported methods are intentionally thin:
// all process and dataset rules live in RuntimeController.
type App struct {
	ctx        context.Context
	controller *RuntimeController
	logs       *logBuffer
}

type logBuffer struct {
	mu    sync.RWMutex
	lines []string
}

const maxUILogLines = 500

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines = append(b.lines, string(p))
	if len(b.lines) > maxUILogLines {
		b.lines = append([]string(nil), b.lines[len(b.lines)-maxUILogLines:]...)
	}
	return len(p), nil
}

func (b *logBuffer) Lines() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return append([]string(nil), b.lines...)
}

func NewApp() *App {
	logs := &logBuffer{}
	log.SetOutput(io.MultiWriter(os.Stderr, logs))
	return &App{controller: NewRuntimeController(), logs: logs}
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

func (a *App) shutdown(ctx context.Context) {
	a.controller.StopAndWait(30 * time.Second)
}

func (a *App) PickPresetDirectory() string {
	path, err := wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{Title: "选择包含 config.yaml、hda、da 的预置目录"})
	if err != nil {
		return ""
	}
	return path
}

func (a *App) InspectPreset(root string) PresetSummary { return a.controller.InspectPreset(root) }
func (a *App) StartPreset(root string) string {
	if err := a.controller.Start(root); err != nil {
		return err.Error()
	}
	return ""
}
func (a *App) StopService()                      { a.controller.Stop() }
func (a *App) GetRuntimeStatus() RuntimeSnapshot { return a.controller.Snapshot() }
func (a *App) GetTagSnapshots(query, filter string) []TagSnapshot {
	return a.controller.TagSnapshots(query, filter)
}
func (a *App) GetLogs() []string  { return a.logs.Lines() }
func (a *App) GetVersion() string { return version }
