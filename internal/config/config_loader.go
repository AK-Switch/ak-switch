package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// deprecatedKeys maps removed TOML field names to their human-readable descriptions.
var deprecatedKeys = map[string]string{
	"genai": "use `target` instead (GenaiBase was removed)",
}

// warnDeprecatedKeys scans raw TOML text for keys that no longer exist in the
// Config struct and logs a warning with a migration hint. Uses a line-by-line
// scan to avoid false positives in values or comments.
func warnDeprecatedKeys(data []byte) {
	for lineNum, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "[") {
			continue
		}
		for key, hint := range deprecatedKeys {
			if strings.HasPrefix(trimmed, key) {
				rest := trimmed[len(key):]
				if rest == "" || rest[0] == '=' || rest[0] == ' ' || rest[0] == '	' {
					slog.Warn("config.toml contains deprecated field",
						"field", key, "line", lineNum+1, "hint", hint)
				}
			}
		}
	}
}

// XDGConfigPath 返回 ~/.akswitch/config.toml 配置路径。
// 可通过 ConfigDir 变量（Go 层面）或 AKSWITCH_CONFIG_DIR 环境变量覆盖。
var ConfigDir string

func XDGConfigPath() (string, error) {
	if ConfigDir != "" {
		return filepath.Join(ConfigDir, "config.toml"), nil
	}
	if configDir := os.Getenv("AKSWITCH_CONFIG_DIR"); configDir != "" {
		return filepath.Join(configDir, "config.toml"), nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取用户家目录失败: %w", err)
	}
	return filepath.Join(homeDir, ".akswitch", "config.toml"), nil
}

// LoadAllTomlProviders 读取 TOML 配置文件中的所有 [provider.*] 段，
// 每个段转换为独立的 *Config 实例，返回 provider 名到 Config 的映射。
// 文件必须存在且格式合法；格式错误或缺少 [provider] 段返回 error。
func LoadAllTomlProviders(path string) (map[string]*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &ConfigError{
			Category: "system",
			Message:  fmt.Sprintf("系统错误: 读取 TOML 配置文件 %q 失败: %v", path, err),
		}
	}
	var tc TomlConfig
	if err := toml.Unmarshal(data, &tc); err != nil {
		return nil, &ConfigError{
			Category: "config",
			Message:  fmt.Sprintf("配置错误: TOML 解析失败: %v", err),
		}
	}
	warnDeprecatedKeys(data)
	DefaultProviderName = tc.DefaultProvider
	if len(tc.Provider) == 0 {
		return nil, &ConfigError{
			Category: "config",
			Message:  "配置错误: TOML 配置缺少 [provider] 段",
		}
	}
	result := make(map[string]*Config, len(tc.Provider))
	port := tc.Port
	if port == 0 {
		port = DefaultProviderConfig().Port
	}
	host := tc.Host
	if host == "" {
		host = DefaultProviderConfig().Host
	}
	defaultCfg := tc.Default
	if defaultCfg == nil {
		if p, ok := tc.Provider["default"]; ok {
			defaultCfg = p
			delete(tc.Provider, "default")
		}
	}
	for name, p := range tc.Provider {
		if p == nil {
			if defaultCfg != nil {
				p = defaultCfg.DeepCopy()
			} else {
				p = &Config{ProviderConfig: *DefaultProviderConfig()}
			}
		} else if defaultCfg != nil {
			p = mergeWithDefaults(defaultCfg, p)
		}
		p.mergeDefaults()
		p.Port = port
		// Top-level host used as fallback when provider-level host is empty
		if p.Host == "" {
			p.Host = host
		}
		// Top-level log fields override per-provider log fields
		if tc.LogFile != "" {
			p.LogFile = tc.LogFile
		}
		if tc.LogMaxSize > 0 {
			p.LogMaxSize = tc.LogMaxSize
		}
		if tc.LogMaxAge > 0 {
			p.LogMaxAge = tc.LogMaxAge
		}
		result[name] = p
	}
	// Warn about provider names containing uppercase letters.
	// Provider names are used in route paths, log labels, and metrics;
	// lowercase kebab-case is the recommended convention.
	for name := range tc.Provider {
		if strings.ToLower(name) != name {
			slog.Warn("provider name contains uppercase letters; consider using lowercase kebab-case",
				"provider", name)
		}
	}
	return result, nil
}

// FindServerHost finds the first non-empty host from TOML providers.
// Returns empty string if no host is configured or if the TOML file cannot be loaded.
func FindServerHost(xdgPath string) string {
	providers, err := LoadAllTomlProviders(xdgPath)
	if err != nil {
		return ""
	}
	for _, cfg := range providers {
		if cfg.Host != "" {
			return cfg.Host
		}
	}
	return ""
}

// FindServerPort finds the first non-zero port from TOML providers.
// Returns 0 if no port is configured or if the TOML file cannot be loaded.
func FindServerPort(xdgPath string) int {
	providers, err := LoadAllTomlProviders(xdgPath)
	if err != nil {
		return 0
	}
	for _, cfg := range providers {
		if cfg.Port > 0 {
			return cfg.Port
		}
	}
	return 0
}
