package portalconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen         string   `yaml:"listen"`
	PresetsDir     string   `yaml:"presets_dir"`
	ExcludePresets []string `yaml:"exclude_presets"`
}

// Load reads the portal configuration. The portal config intentionally has no
// per-preset addresses: those stay in each preset's config.yaml.
func Load(path string) (Config, error) {
	cfg := Config{
		Listen:         "0.0.0.0:8080",
		ExcludePresets: []string{"demo"},
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("读取门户配置 %s 失败: %w", path, err)
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("解析门户配置 %s 失败: %w", path, err)
	}
	cfg.Listen = strings.TrimSpace(cfg.Listen)
	if cfg.Listen == "" {
		cfg.Listen = "0.0.0.0:8080"
	}
	cfg.PresetsDir = strings.TrimSpace(cfg.PresetsDir)
	if cfg.PresetsDir == "" {
		return cfg, fmt.Errorf("门户配置缺少 presets_dir")
	}
	if !filepath.IsAbs(cfg.PresetsDir) {
		cfg.PresetsDir = filepath.Join(filepath.Dir(path), cfg.PresetsDir)
	}
	cfg.PresetsDir = filepath.Clean(cfg.PresetsDir)
	return cfg, nil
}
