package storage

import (
	"encoding/json"
	"os"
	"path/filepath"

	"hda_client/internal/hda"
)

// JSONConfig 把最近一次查询配置持久化到 JSON 文件。
type JSONConfig struct {
	path string
}

func NewJSONConfig(path string) *JSONConfig {
	return &JSONConfig{path: path}
}

func (j *JSONConfig) Save(cfg hda.QueryConfig) error {
	if err := os.MkdirAll(filepath.Dir(j.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(j.path, data, 0o644)
}

func (j *JSONConfig) Load() (hda.QueryConfig, bool, error) {
	data, err := os.ReadFile(j.path)
	if err != nil {
		if os.IsNotExist(err) {
			return hda.QueryConfig{}, false, nil
		}
		return hda.QueryConfig{}, false, err
	}
	var cfg hda.QueryConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return hda.QueryConfig{}, false, err
	}
	return cfg, true, nil
}

