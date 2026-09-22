package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const (
	logFileName    = "hda_mocker_4.log"
	stderrFileName = "hda_mocker_4.stderr.log"
	maxLogSize     = int64(20 * 1024 * 1024)
	maxLogBackups  = 5
)

var (
	logOutputMu sync.RWMutex
	logOutput   io.Writer = os.Stderr
)

type rollingLogWriter struct {
	mu      sync.Mutex
	path    string
	maxSize int64
	backups int
	file    *os.File
	size    int64
}

func newRollingLogWriter(path string, maxSize int64, backups int) (*rollingLogWriter, error) {
	if maxSize <= 0 || backups < 1 {
		return nil, fmt.Errorf("invalid log rotation settings")
	}
	w := &rollingLogWriter{path: path, maxSize: maxSize, backups: backups}
	if err := w.open(); err != nil {
		return nil, err
	}
	if w.size >= w.maxSize {
		if err := w.rotate(); err != nil {
			w.file.Close()
			return nil, err
		}
	}
	return w, nil
}

func (w *rollingLogWriter) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.file, w.size = f, info.Size()
	return nil
}

func (w *rollingLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return 0, os.ErrClosed
	}
	if w.size > 0 && w.size+int64(len(p)) > w.maxSize {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rollingLogWriter) rotate() error {
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return err
		}
		w.file = nil
	}
	_ = os.Remove(fmt.Sprintf("%s.%d", w.path, w.backups))
	for i := w.backups - 1; i >= 1; i-- {
		oldPath := fmt.Sprintf("%s.%d", w.path, i)
		newPath := fmt.Sprintf("%s.%d", w.path, i+1)
		if err := os.Rename(oldPath, newPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Rename(w.path, w.path+".1"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return w.open()
}

func (w *rollingLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

type applicationLogs struct {
	rolling *rollingLogWriter
	stderr  *os.File
}

func initializeApplicationLogs() (*applicationLogs, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(filepath.Dir(exe), "logs")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	rolling, err := newRollingLogWriter(filepath.Join(dir, logFileName), maxLogSize, maxLogBackups)
	if err != nil {
		return nil, fmt.Errorf("open application log: %w", err)
	}
	stderr, err := redirectProcessStderr(filepath.Join(dir, stderrFileName))
	if err != nil {
		rolling.Close()
		return nil, fmt.Errorf("open stderr log: %w", err)
	}
	logOutputMu.Lock()
	logOutput = rolling
	log.SetOutput(rolling)
	logOutputMu.Unlock()
	return &applicationLogs{rolling: rolling, stderr: stderr}, nil
}

func applicationLogOutput() io.Writer {
	logOutputMu.RLock()
	defer logOutputMu.RUnlock()
	return logOutput
}

func (l *applicationLogs) Close() {
	if l == nil {
		return
	}
	if l.rolling != nil {
		_ = l.rolling.Close()
	}
	if l.stderr != nil {
		_ = l.stderr.Close()
	}
}
