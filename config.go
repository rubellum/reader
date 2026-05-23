package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config はコマンドライン引数と同じ設定を JSON で指定するための構造体。
// CLI で明示指定された値が優先され、未指定時にこの値が適用される。
type Config struct {
	Host      *string  `json:"host,omitempty"`
	Port      *int     `json:"port,omitempty"`
	Include   []string `json:"include,omitempty"`
	Exclude   []string `json:"exclude,omitempty"`
	Read      *string  `json:"read,omitempty"`
	ReadR     *string  `json:"read-r,omitempty"`
	Write     *string  `json:"write,omitempty"`
	WriteR    *string  `json:"write-r,omitempty"`
	Archive   *string  `json:"archive,omitempty"`
	Verbosity *int     `json:"verbosity,omitempty"`
	Dir       *string  `json:"dir,omitempty"`
}

// loadConfig は指定パスから JSON 設定を読み込む。
func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("設定ファイル %s の解析に失敗: %w", path, err)
	}
	return &cfg, nil
}
