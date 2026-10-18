package catalog

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var (
	ErrFileNotFound = errors.New("文件不存在")
	ErrNotAllowed   = errors.New("文件不在允许发布的范围")
)

// File is a downloadable artifact advertised for one preset.
type File struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Category   string    `json:"category"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
	path       string
}

const maxSummaryBytes = 5 << 20

// ListFiles enumerates the whitelisted files of a preset: first-level parquet
// files under hda/ and da/, plus config.yaml and summary.md.
func (m *Mock) ListFiles() []File {
	files := []File{}
	for _, pair := range []struct {
		category string
		dir      string
	}{
		{CategoryHDA, filepath.Join(m.Dir, "hda")},
		{CategoryDA, filepath.Join(m.Dir, "da")},
	} {
		entries, err := os.ReadDir(pair.dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(entry.Name()), ".parquet") {
				continue
			}
			full := filepath.Join(pair.dir, entry.Name())
			info, err := entry.Info()
			if err != nil || info.Size() == 0 {
				continue
			}
			files = append(files, File{
				ID:         encodeFileID(pair.category, entry.Name()),
				Name:       entry.Name(),
				Category:   pair.category,
				Size:       info.Size(),
				ModifiedAt: info.ModTime().UTC(),
				path:       full,
			})
		}
	}
	for _, pair := range []struct {
		category string
		path     string
		name     string
	}{
		{CategoryConfig, m.ConfigPath, "config.yaml"},
		{CategorySummary, m.SummaryPath, "summary.md"},
	} {
		info, err := os.Stat(pair.path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, File{
			ID:         encodeFileID(pair.category, pair.name),
			Name:       pair.name,
			Category:   pair.category,
			Size:       info.Size(),
			ModifiedAt: info.ModTime().UTC(),
			path:       pair.path,
		})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Category != files[j].Category {
			return files[i].Category < files[j].Category
		}
		return files[i].Name < files[j].Name
	})
	return files
}

func encodeFileID(category, name string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(category + "/" + name))
}

// OpenFile resolves a file ID against the current whitelist. IDs are opaque and
// cannot address arbitrary paths.
func (m *Mock) OpenFile(id string) (*os.File, File, error) {
	for _, candidate := range m.ListFiles() {
		if candidate.ID != id {
			continue
		}
		if err := ensureInside(m.Dir, candidate.path); err != nil {
			return nil, File{}, err
		}
		f, err := os.Open(candidate.path)
		if err != nil {
			return nil, File{}, err
		}
		return f, candidate, nil
	}
	return nil, File{}, ErrFileNotFound
}

func ensureInside(root, target string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	resolvedRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil {
		return ErrNotAllowed
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return ErrNotAllowed
	}
	return nil
}

// Summary returns the raw Markdown document and its metadata.
func (m *Mock) Summary() (string, time.Time, error) {
	if err := ensureInside(m.Dir, m.SummaryPath); err != nil {
		if errors.Is(err, ErrNotAllowed) {
			return "", time.Time{}, ErrFileNotFound
		}
	}
	info, err := os.Stat(m.SummaryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", time.Time{}, ErrFileNotFound
		}
		return "", time.Time{}, err
	}
	if !info.Mode().IsRegular() {
		return "", time.Time{}, ErrFileNotFound
	}
	if info.Size() > maxSummaryBytes {
		return "", time.Time{}, fmt.Errorf("summary.md 超过 %d 字节", maxSummaryBytes)
	}
	f, err := os.Open(m.SummaryPath)
	if err != nil {
		return "", time.Time{}, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxSummaryBytes))
	if err != nil {
		return "", time.Time{}, err
	}
	return string(raw), info.ModTime().UTC(), nil
}
