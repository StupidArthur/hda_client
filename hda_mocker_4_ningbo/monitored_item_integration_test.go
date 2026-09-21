package main

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
)

func TestDeleteUnknownMonitoredItemDoesNotCrashServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := openStore(filepath.Join(t.TempDir(), "history.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var cfg Config
	cfg.Server.Endpoint = fmt.Sprintf("opc.tcp://127.0.0.1:%d", port)
	cfg.Server.NS = 3
	cfg.Server.Namespace = "urn:test:delete-monitored-item"
	mocker, err := newServer(cfg, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = mocker.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer mocker.close()

	client, err := opcua.NewClient(cfg.Server.Endpoint,
		opcua.SecurityMode(ua.MessageSecurityModeNone),
		opcua.SecurityPolicy(ua.SecurityPolicyURINone),
		opcua.AuthAnonymous(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer client.Close(ctx)

	sub, err := client.Subscribe(ctx, &opcua.SubscriptionParameters{Interval: 100 * time.Millisecond}, make(chan *opcua.PublishNotificationData, 1))
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Cancel(ctx)

	res, err := sub.Unmonitor(ctx, 0xdeadbeef)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 1 || res.Results[0] != ua.StatusBadMonitoredItemIDInvalid {
		t.Fatalf("unexpected results: %#v", res.Results)
	}
}
