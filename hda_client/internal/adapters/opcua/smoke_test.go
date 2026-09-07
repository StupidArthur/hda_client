package opcua

import (
	"context"
	"testing"
	"time"

	"hda_client/internal/hda"
)

// TestReadRawRemote 连真实 HDA 服务器验证读取链路(离线环境会 skip)。
func TestReadRawRemote(t *testing.T) {
	const url = "opc.tcp://10.30.144.70:18950/"
	ctx := context.Background()
	c, err := NewClient(ctx, url)
	if err != nil {
		t.Skipf("服务器不可达: %v", err)
	}
	defer c.Close()

	end := time.Now().UTC()
	start := end.Add(-time.Minute)
	pts, err := c.ReadRaw(ctx, "ns=1;s=M0001.VALUE", start, end)
	if err != nil {
		t.Fatalf("ReadRaw: %v", err)
	}
	if len(pts) == 0 {
		t.Fatal("未取到数据")
	}
	t.Logf("M0001.VALUE: %d 点, 首条 %v = %v [%s]", len(pts), pts[0].Time, pts[0].Value, pts[0].Quality)

	// 引擎级验证: 单位号分段+并发
	svc := hda.NewService(func(factoryCtx context.Context, u string) (hda.HistoryClient, error) { return NewClient(factoryCtx, u) }, nil)
	if err := svc.Connect(ctx, url); err != nil {
		t.Fatalf("connect: %v", err)
	}
	res, err := svc.RunQuery(ctx, hda.QueryConfig{
		URL: url, NS: 1, Tags: []string{"M0001.VALUE", "M0002.VALUE"},
		EndTime:     time.Now().Format("2006-01-02T15:04:05"),
		DurationSec: 600, Concurrency: 8,
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
