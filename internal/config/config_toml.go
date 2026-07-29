package config

import (
	"bytes"
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
)

// ---- TOML 配置支持 ----

// TomlConfig 对应整个 config.toml 文件结构。
// 顶层字段作为全局默认值：每个 [provider.*] 段未显式设置的字段自动继承全局值。
type TomlConfig struct {
	// Global defaults — inherited by all providers unless overridden in [provider.X]
	Port                   int     `toml:"port,omitempty"`
	Host                   string  `toml:"host,omitempty"`
	LogFile                string  `toml:"log_file,omitempty"`
	LogMaxSize             int     `toml:"log_max_size,omitempty"`
	LogMaxAge              int     `toml:"log_max_age,omitempty"`
	DefaultProvider        string  `toml:"default_provider,omitempty"`
	MaxRetries             int     `toml:"max_retries,omitempty"`
	CooldownSec            int     `toml:"cooldown_sec,omitempty"`
	HTTPTimeoutSec         int     `toml:"http_timeout_sec,omitempty"`
	LogLevel               string  `toml:"log_level,omitempty"`
	BackoffCapSec          int     `toml:"backoff_cap_sec,omitempty"`
	BackoffMultiplier      float64 `toml:"backoff_multiplier,omitempty"`
	CBResetSec             int     `toml:"cb_reset_sec,omitempty"`
	UpstreamCBThreshold    int     `toml:"upstream_cb_threshold,omitempty"`
	HealthCheckIntervalSec int     `toml:"health_check_interval_sec,omitempty"`

	Provider map[string]*Config `toml:"provider"`
}

// DefaultProviderName 保存从 TOML 配置中读取的默认 provider 名称。
// 由 LoadAllTomlProviders 设置，被 start 命令用于确定要启动的 provider。
var DefaultProviderName string

// LoadTomlConfig 读取 TOML 配置文件并返回完整的 TomlConfig（包含所有 provider）。
// 文件不存在时返回原始错误（可通过 os.IsNotExist 检查）。
func LoadTomlConfig(path string) (*TomlConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tc TomlConfig
	if err := toml.Unmarshal(data, &tc); err != nil {
		return nil, err
	}
	if tc.Provider == nil {
		tc.Provider = make(map[string]*Config)
	}
	return &tc, nil
}

// SaveTomlConfig 将完整 TomlConfig 写入 TOML 文件。覆盖已存在的文件。
func SaveTomlConfig(tc *TomlConfig, path string) error {
	var buf bytes.Buffer
	encoder := toml.NewEncoder(&buf)
	if err := encoder.Encode(tc); err != nil {
		return fmt.Errorf("TOML 编码失败: %w", err)
	}
	return os.WriteFile(path, buf.Bytes(), 0644)
}
