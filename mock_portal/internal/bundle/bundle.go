package bundle

import (
	"archive/zip"
	"bytes"
	"embed"
	"io/fs"
	"sort"
	"time"
)

//go:embed all:files
var embedded embed.FS

// Item describes one file inside the standardization bundle.
type Item struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

const ArchiveName = "hda-data-import-skill.zip"

// Items lists the bundle contents in a stable order.
func Items() ([]Item, error) {
	walk, err := fs.Sub(embedded, "files")
	if err != nil {
		return nil, err
	}
	items := []Item{}
	err = fs.WalkDir(walk, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		items = append(items, Item{Path: path, Size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items, nil
}

// WriteZip streams the bundle as a zip archive. The bundle is fixed at build
// time; no user input is part of the archive.
func WriteZip(out *zip.Writer) error {
	walk, err := fs.Sub(embedded, "files")
	if err != nil {
		return err
	}
	items, err := Items()
	if err != nil {
		return err
	}
	modified := time.Now()
	for _, item := range items {
		data, err := fs.ReadFile(walk, item.Path)
		if err != nil {
			return err
		}
		header := &zip.FileHeader{Name: item.Path, Method: zip.Deflate, Modified: modified}
		header.SetMode(0o644)
		writer, err := out.CreateHeader(header)
		if err != nil {
			return err
		}
		if _, err := writer.Write(data); err != nil {
			return err
		}
	}
	return nil
}

// Bytes renders the archive in memory; used by tests and small deployments.
func Bytes() ([]byte, error) {
	buf := &bytes.Buffer{}
	writer := zip.NewWriter(buf)
	if err := WriteZip(writer); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
