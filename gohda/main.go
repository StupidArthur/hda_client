package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
)

const (
	url         = "opc.tcp://10.30.144.70:18950/"
	nNodes      = 1000
	spanMin     = 40
	segMin      = 15
	concurrency = 64
)

func main() {
	ctx := context.Background()
	c, err := opcua.NewClient(url, opcua.SecurityMode(ua.MessageSecurityModeNone))
	if err != nil {
		log.Fatal(err)
	}
	if err := c.Connect(ctx); err != nil {
		log.Fatal(err)
	}
	defer c.Close(ctx)

	end := time.Now().UTC()
	start := end.Add(-time.Duration(spanMin) * time.Minute)
	seg := time.Duration(segMin) * time.Minute

	type job struct {
		id string
		s  time.Time
		e  time.Time
	}
	var jobs []job
	for i := 1; i <= nNodes; i++ {
		nid := fmt.Sprintf("ns=1;s=M%04d.VALUE", i)
		for t := start; t.Before(end); t = t.Add(seg) {
			e := t.Add(seg)
			if e.After(end) {
				e = end
			}
			jobs = append(jobs, job{nid, t, e})
		}
	}
	fmt.Printf("单连接并发=%d, 请求数=%d\n", concurrency, len(jobs))

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	var total, real atomic.Int64
	t0 := time.Now()

	for _, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(j job) {
			defer wg.Done()
			defer func() { <-sem }()
			id, err := ua.ParseNodeID(j.id)
			if err != nil {
				return
			}
			resp, err := c.HistoryReadRawModified(ctx,
				[]*ua.HistoryReadValueID{{NodeID: id, DataEncoding: &ua.QualifiedName{}}},
				&ua.ReadRawModifiedDetails{
					IsReadModified:   false,
					StartTime:        j.s,
					EndTime:          j.e,
					NumValuesPerNode: 5000,
					ReturnBounds:     false,
				})
			if err != nil {
				return
			}
			for _, r := range resp.Results {
				if r.StatusCode != ua.StatusOK {
					continue
				}
				hd, ok := r.HistoryData.Value.(*ua.HistoryData)
				if !ok || hd == nil {
					continue
				}
				total.Add(int64(len(hd.DataValues)))
				for _, dv := range hd.DataValues {
					if dv.Status == ua.StatusOK {
						real.Add(1)
					}
				}
			}
		}(j)
	}
	wg.Wait()
	dt := time.Since(t0)
	fmt.Printf("总耗时: %.2fs\n", dt.Seconds())
	fmt.Printf("总条数: %d (真实 %d)\n", total.Load(), real.Load())
	fmt.Printf("吞吐: %.0f 条/s\n", float64(real.Load())/dt.Seconds())
}
