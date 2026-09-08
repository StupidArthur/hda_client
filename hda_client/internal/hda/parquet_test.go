package hda

import (
	"path/filepath"
	"testing"
	"time"
)

func TestParquetRoundTripAndPaging(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.parquet")
	base := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	n, err := WriteParquet(path, []TagResult{
		{Tag: "M0001.VALUE", Points: []DataPoint{{Time: base, Value: 1, Quality: "Good", HasValue: true}, {Time: base.Add(time.Second), Value: 2, Quality: "Good", HasValue: true}}},
		{Tag: "M0002.VALUE", Points: []DataPoint{{Time: base, Value: 3, Quality: "Uncertain", HasValue: true}}},
	})
	if err != nil || n != 3 {
		t.Fatalf("WriteParquet = %d, %v", n, err)
	}
	page, err := ReadParquetPage(path, "M0001.VALUE", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Rows) != 1 || page.Rows[0].Value != 2 {
		t.Fatalf("unexpected page: %#v", page)
	}
	if page.Rows[0].Timestamp != base.Add(time.Second).Format(time.RFC3339Nano) {
		t.Fatalf("timestamp = %q, want RFC3339Nano", page.Rows[0].Timestamp)
	}
	if len(page.Nodes) != 2 {
		t.Fatalf("nodes = %#v", page.Nodes)
	}
	summary, err := ReadParquetSummary(path)
	if err != nil || summary.Records != 3 || summary.Good != 2 || summary.Uncertain != 1 || len(summary.Nodes) != 2 {
		t.Fatalf("unexpected summary: %#v, %v", summary, err)
	}
	trend, err := ReadParquetTrend(path, []string{"M0001.VALUE"}, 100)
	if err != nil || len(trend) != 1 || len(trend[0].Points) != 2 {
		t.Fatalf("unexpected trend: %#v, %v", trend, err)
	}
	multi, err := ReadParquetNodesPage(path, []string{"M0001.VALUE", "M0002.VALUE"}, 0, 10)
	if err != nil || multi.Total != 3 {
		t.Fatalf("unexpected multi-node page: %#v, %v", multi, err)
	}
}
