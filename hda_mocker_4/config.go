package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Endpoint    string `yaml:"endpoint"`
		NS          int    `yaml:"ns"`
		Namespace   string `yaml:"namespace"`
		MaxPageSize int    `yaml:"max_page_size"`
	} `yaml:"server"`
	Playback struct {
		Files map[string]struct {
			PeriodMS int `yaml:"period_ms"`
		} `yaml:"files"`
	} `yaml:"playback"`
	History struct {
		RetentionDays int `yaml:"retention_days"`
	} `yaml:"history"`
}

func (c Config) playbackPeriod(name string) (time.Duration, bool) {
	f, ok := c.Playback.Files[name]
	return time.Duration(f.PeriodMS) * time.Millisecond, ok
}

func loadConfig(path string) (Config, string, error) {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, "", err
	}
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err = dec.Decode(&c); err != nil {
		return c, "", fmt.Errorf("parse config: %w", err)
	}
	if c.Server.Endpoint == "" || c.Server.Namespace == "" {
		return c, "", fmt.Errorf("server.endpoint and server.namespace are required")
	}
	if c.Server.NS < 1 || c.Server.NS > 65535 {
		return c, "", fmt.Errorf("server.ns must be between 1 and 65535")
	}
	if c.Server.MaxPageSize <= 0 {
		return c, "", fmt.Errorf("server.max_page_size must be > 0")
	}
	for name, f := range c.Playback.Files {
		if strings.TrimSpace(name) == "" || filepath.Base(name) != name {
			return c, "", fmt.Errorf("playback.files keys must be DA parquet file names")
		}
		if f.PeriodMS <= 0 {
			return c, "", fmt.Errorf("playback.files.%s.period_ms must be > 0", name)
		}
	}
	if c.History.RetentionDays < 0 {
		return c, "", fmt.Errorf("history.retention_days must be >= 0")
	}
	return c, filepath.Dir(path), nil
}
