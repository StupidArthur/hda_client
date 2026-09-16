package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config deliberately mirrors hda_mocker_2.  The optional continuation_point_mode
// is a server-test setting: rotating is the normal OPC UA behaviour, while stable
// deliberately returns the same opaque value as its cursor advances.
type Config struct {
	Server struct {
		Host         string `yaml:"host"`
		Port         int    `yaml:"port"`
		NamespaceURI string `yaml:"namespace_uri"`
	} `yaml:"server"`
	History struct {
		Interval              string `yaml:"interval"`
		Duration              string `yaml:"duration"`
		DefaultHDALength      string `yaml:"default_hda_length"`
		NumValuesPerNode      uint32 `yaml:"num_values_per_node"`
		ReadTimeout           string `yaml:"read_timeout"`
		ContinuationPointMode string `yaml:"continuation_point_mode"`
		ContinuationPointTTL  string `yaml:"continuation_point_ttl"`
	} `yaml:"history"`
	PresetNodes string `yaml:"preset_nodes"`
}

type PresetNodes struct {
	TypeNodes struct {
		Groups int `yaml:"groups"`
	} `yaml:"type_nodes"`
	DynamicNodes struct {
		Count int `yaml:"count"`
	} `yaml:"dynamic_nodes"`
	StaticNodes struct {
		Count int `yaml:"count"`
	} `yaml:"static_nodes"`
	BadRealtimeNodes struct {
		Count        int    `yaml:"count"`
		GoodDuration string `yaml:"good_duration"`
		BadDuration  string `yaml:"bad_duration"`
	} `yaml:"bad_realtime_nodes"`
}

type Settings struct {
	Host, NamespaceURI, CPMode                       string
	Port                                             int
	Interval, HistoryLength                          time.Duration
	QueryDuration, ReadTimeout, ContinuationPointTTL time.Duration
	PageCap                                          uint32
	TypeGroups, DynamicCount, StaticCount, BadCount  int
	GoodDuration, BadDuration                        time.Duration
}

func parseDuration(v string) (time.Duration, error) {
	v = strings.TrimSpace(strings.ToLower(v))
	if len(v) < 2 {
		return 0, fmt.Errorf("invalid duration %q", v)
	}
	unit := v[len(v)-1]
	number := v[:len(v)-1]
	n, err := strconv.ParseInt(number, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid duration %q", v)
	}
	var multiplier time.Duration
	switch unit {
	case 's':
		multiplier = time.Second
	case 'm':
		multiplier = time.Minute
	case 'h':
		multiplier = time.Hour
	case 'd':
		multiplier = 24 * time.Hour
	case 'y':
		multiplier = 365 * 24 * time.Hour
	default:
		return 0, fmt.Errorf("invalid duration %q (use 30s/9m/24h/7d/1y)", v)
	}
	if n > int64((1<<63-1)/int64(multiplier)) {
		return 0, fmt.Errorf("duration %q is too large", v)
	}
	return time.Duration(n) * multiplier, nil
}

func executableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

func resolveConfig(arg string) (string, error) {
	if arg == "" {
		arg = "default"
	}
	if info, err := os.Stat(arg); err == nil && !info.IsDir() {
		return filepath.Abs(arg)
	}
	path := filepath.Join(executableDir(), "presets", arg, "config.yaml")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	// `go run . default` is convenient during development.
	path = filepath.Join("presets", arg, "config.yaml")
	if _, err := os.Stat(path); err == nil {
		return filepath.Abs(path)
	}
	return "", fmt.Errorf("config %q not found; pass a YAML path or one of presets/default, simple, basf_long", arg)
}

func loadSettings(arg string) (Settings, string, error) {
	path, err := resolveConfig(arg)
	if err != nil {
		return Settings{}, "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, "", err
	}
	var c Config
	if err := decodeStrictYAML(b, &c); err != nil {
		return Settings{}, "", fmt.Errorf("parse %s: %w", path, err)
	}
	if c.PresetNodes == "" {
		c.PresetNodes = "preset_nodes.yaml"
	}
	presetPath := filepath.Join(filepath.Dir(path), c.PresetNodes)
	b, err = os.ReadFile(presetPath)
	if err != nil {
		return Settings{}, "", fmt.Errorf("read preset nodes: %w", err)
	}
	var p PresetNodes
	if err := decodeStrictYAML(b, &p); err != nil {
		return Settings{}, "", fmt.Errorf("parse preset nodes: %w", err)
	}
	interval, err := parseDuration(defaultString(c.History.Interval, "1s"))
	if err != nil {
		return Settings{}, "", err
	}
	historyLength, err := parseDuration(defaultString(c.History.DefaultHDALength, "7d"))
	if err != nil {
		return Settings{}, "", err
	}
	queryDuration, err := parseDuration(defaultString(c.History.Duration, "24h"))
	if err != nil {
		return Settings{}, "", err
	}
	readTimeout, err := parseDuration(defaultString(c.History.ReadTimeout, "20s"))
	if err != nil {
		return Settings{}, "", err
	}
	cpTTL, err := parseDuration(defaultString(c.History.ContinuationPointTTL, "5m"))
	if err != nil {
		return Settings{}, "", err
	}
	good, err := parseDuration(defaultString(p.BadRealtimeNodes.GoodDuration, "9m"))
	if err != nil {
		return Settings{}, "", err
	}
	bad, err := parseDuration(defaultString(p.BadRealtimeNodes.BadDuration, "1m"))
	if err != nil {
		return Settings{}, "", err
	}
	mode := defaultString(c.History.ContinuationPointMode, "rotating")
	if mode != "rotating" && mode != "stable" {
		return Settings{}, "", fmt.Errorf("history.continuation_point_mode must be rotating or stable")
	}
	if c.Server.Port == 0 {
		c.Server.Port = 48630
	}
	if c.Server.Host == "" {
		c.Server.Host = "0.0.0.0"
	}
	if c.Server.NamespaceURI == "" {
		c.Server.NamespaceURI = "urn:hda-mocker-3"
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 || c.History.NumValuesPerNode == 0 {
		return Settings{}, "", fmt.Errorf("server.port must be 1..65535 and num_values_per_node must be positive")
	}
	if historyLength < queryDuration {
		return Settings{}, "", fmt.Errorf("default_hda_length cannot be shorter than duration")
	}
	for _, v := range []int{p.TypeNodes.Groups, p.DynamicNodes.Count, p.StaticNodes.Count, p.BadRealtimeNodes.Count} {
		if v < 0 {
			return Settings{}, "", fmt.Errorf("node counts must not be negative")
		}
	}
	return Settings{Host: c.Server.Host, Port: c.Server.Port, NamespaceURI: c.Server.NamespaceURI, CPMode: mode, Interval: interval, HistoryLength: historyLength, QueryDuration: queryDuration, ReadTimeout: readTimeout, ContinuationPointTTL: cpTTL, PageCap: c.History.NumValuesPerNode, TypeGroups: p.TypeNodes.Groups, DynamicCount: p.DynamicNodes.Count, StaticCount: p.StaticNodes.Count, BadCount: p.BadRealtimeNodes.Count, GoodDuration: good, BadDuration: bad}, path, nil
}

func decodeStrictYAML(data []byte, dst any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple YAML documents are not supported")
		}
		return err
	}
	return nil
}

func defaultString(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
