package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gopcua/opcua/ua"
)

func TestQualityState(t *testing.T) {
	tests := []struct {
		status uint32
		want   string
	}{
		{uint32(ua.StatusOK), "良好"},
		{uint32(ua.StatusUncertain), "不确定"},
		{uint32(ua.StatusBadNoCommunication), "异常"},
		{uint32(ua.StatusBadWaitingForInitialData), "等待数据"},
	}
	for _, test := range tests {
		if got := qualityState(test.status); got != test.want {
			t.Errorf("qualityState(%08X) = %q, want %q", test.status, got, test.want)
		}
	}
}

func TestStartConfigUsesConfigParentAsPresetRoot(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "case-a.yaml")
	controller := NewRuntimeController()
	if err := controller.StartConfig(configPath); err != nil {
		t.Fatal(err)
	}
	if got := controller.Snapshot().PresetRoot; got != dir {
		t.Fatalf("preset root = %q, want %q", got, dir)
	}
	controller.StopAndWait(2 * time.Second)
}
