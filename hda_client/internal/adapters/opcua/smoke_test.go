package opcua

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"hda_client/internal/hda"
)

// TestReadRawRemote 连真实 HDA 服务器验证读取链路(离线环境会 skip)。
func TestReadRawRemote(t *testing.T) {
	url := os.Getenv("HDA_TEST_URL")
	if url == "" {
		t.Skip("set HDA_TEST_URL to run the OPC UA HDA integration test")
	}
	ns := uint16(2)
	if raw := os.Getenv("HDA_TEST_NS"); raw != "" {
		value, err := strconv.ParseUint(raw, 10, 16)
		if err != nil {
			t.Fatalf("invalid HDA_TEST_NS %q: %v", raw, err)
		}
		ns = uint16(value)
	}
	ctx := context.Background()
	c, err := NewClient(ctx, url)
	if err != nil {
		t.Skipf("服务器不可达: %v", err)
	}
	defer c.Close()

	end := time.Now().UTC().Truncate(time.Second)
	start := end.Add(-time.Hour)
	pts, err := c.ReadRaw(ctx, hda.BuildNodeID(ns, "dynamic_0001"), start, end)
	if err != nil {
		t.Fatalf("ReadRaw: %v", err)
	}
	if len(pts) == 0 {
		t.Fatal("未取到数据")
	}
	if len(pts) != 3601 {
		t.Fatalf("expected 3601 paged points, got %d", len(pts))
	}
	for i := 1; i < len(pts); i++ {
		if pts[i].Time.Before(pts[i-1].Time) {
			t.Fatalf("points are out of order at index %d", i)
		}
	}
	t.Logf("dynamic_0001: %d points, first %v = %v [%s]", len(pts), pts[0].Time, pts[0].Value, pts[0].Quality)

	// 引擎级验证: 单位号串行分页 + 多位号并发。
	svc := hda.NewService(func(factoryCtx context.Context, u string) (hda.HistoryClient, error) { return NewClient(factoryCtx, u) }, nil)
	res, err := svc.RunQuery(ctx, hda.QueryConfig{
		URL: url, NS: ns, Tags: []string{"dynamic_0001", "dynamic_0002"},
		EndTime:     time.Now().Format("2006-01-02T15:04:05"),
		DurationSec: 3600, Concurrency: 2,
	}, func(p hda.QueryProgress) { t.Logf("进度 %d/%d records=%d", p.Done, p.Total, p.Records) })
	if err != nil {
		t.Fatalf("RunQuery: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("期望 2 个位号结果, 得到 %d", len(res))
	}
	for _, r := range res {
		t.Logf("tag=%s points=%d", r.Tag, len(r.Points))
	}
}
