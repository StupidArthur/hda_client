package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(j.path), ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o644); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, j.path); err != nil {
		return fmt.Errorf("替换配置文件失败: %w", err)
	}
	committed = true
	return nil
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
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return hda.QueryConfig{}, false, err
	}
	if err := ensureEOF(dec); err != nil {
		return hda.QueryConfig{}, false, err
	}
	return cfg, true, nil
}

func ensureEOF(dec *json.Decoder) error {
	var extra interface{}
	if err := dec.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("配置文件只能包含一个 JSON 对象")
}
