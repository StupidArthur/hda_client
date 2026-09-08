package bindings

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestDefaultParquetPath 默认输出必须落在 exe 同级 data 目录下,
// 且文件名带秒级时间戳与 .parquet 扩展名, 保证不指定输出时不会覆盖旧文件。
func TestDefaultParquetPath(t *testing.T) {
	p := defaultParquetPath()
	base := filepath.Base(p)
	if !strings.HasPrefix(base, "history_") || filepath.Ext(p) != ".parquet" {
		t.Fatalf("unexpected default file name %q", p)
	}
	if filepath.Base(filepath.Dir(p)) != "data" {
		t.Fatalf("default path should live in a data directory, got %q", p)
	}
}

// TestResolveOutputPath 相对路径必须以 exe 同级目录为基准, 空路径与绝对路径原样通过。
func TestResolveOutputPath(t *testing.T) {
	if got := resolveOutputPath(""); got != "" {
		t.Fatalf("empty path should pass through, got %q", got)
	}
	abs := filepath.Join(t.TempDir(), "a.parquet")
	if got := resolveOutputPath(abs); got != abs {
		t.Fatalf("absolute path should pass through, got %q", got)
	}
	rel := filepath.Join(".", "data", "history_20260907_120000.parquet")
	got := resolveOutputPath(rel)
	if !filepath.IsAbs(got) || filepath.Base(filepath.Dir(got)) != "data" {
		t.Fatalf("relative path should resolve under exe data dir, got %q", got)
	}
}
