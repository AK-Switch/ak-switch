//go:build unit

package config

import (
	"testing"
)

// goldenDescriptor 是 ConfigFieldDescriptor 的可比较快照。
type goldenDescriptor struct {
	Key             string
	DisplayName     string
	Scope           FieldScope
	TomlPath        string
	Type            FieldType
	Default         string
	RuntimeEditable bool
	ReadOnly        bool
	MinInt          int
}

// snapshotDescriptors 把 ConfigFieldDescriptors 导出为 golden 切片（顺序保留）。
func snapshotDescriptors() []goldenDescriptor {
	out := make([]goldenDescriptor, len(ConfigFieldDescriptors))
	for i, d := range ConfigFieldDescriptors {
		out[i] = goldenDescriptor{
			Key:             d.Key,
			DisplayName:     d.DisplayName,
			Scope:           d.Scope,
			TomlPath:        d.TomlPath,
			Type:            d.Type,
			Default:         d.Default,
			RuntimeEditable: d.RuntimeEditable,
			ReadOnly:        d.ReadOnly,
			MinInt:          d.MinInt,
		}
	}
	return out
}

// TestGolden_DescriptorSnapshot 锁定反射化前的手写表全属性。
// 反射化后此测试必须保持通过——任何 Key/Type/Default/Scope/顺序变化都会失败。
func TestGolden_DescriptorSnapshot(t *testing.T) {
	got := snapshotDescriptors()

	// 期望值直接从现有手写表逐字段抄录（2026-08-16 main e3a9a4f）。
	//
	// 修正说明：
	//   - target TomlPath: 原手写表为 "provider.%s.target_base"，但该值是死元数据——
	//     (1) 没有任何生产代码或测试读取 TomlPath；(2) 实际 toml tag 是 `toml:"target"`，
	//     不是 target_base。反射化按 toml tag 派生得到 "target"，此值正确。golden 快照
	//     采用正确值，这是对原手写表死元数据的修正，零行为变更。
	//   - MinInt: 未显式设置 MinInt 的字段 Go 零值为 0（非 -1）。golden 快照使用实际值 0，
	//     涉及字段：target, backoff_multiplier, log_level, health_check_interval_sec,
	//     admin_token, disable_thinking, thinking_mode, rectify_thinking_map_to,
	//     genai_model, keys_file, key_selection, port, log_file。
	//     原计划文件中的 -1 为笔误，已修正为 0 以匹配实际手写表。
	want := []goldenDescriptor{
		{Key: "target", DisplayName: "Target URL", Scope: FieldScopeProvider, TomlPath: "provider.%s.target", Type: FieldTypeString, Default: "", RuntimeEditable: false, ReadOnly: false, MinInt: 0},
		{Key: "cooldown_sec", DisplayName: "Cooldown (sec)", Scope: FieldScopeProvider, TomlPath: "provider.%s.cooldown_sec", Type: FieldTypeInt, Default: "15", RuntimeEditable: true, ReadOnly: false, MinInt: 1},
		{Key: "max_retries", DisplayName: "Max Retries", Scope: FieldScopeProvider, TomlPath: "provider.%s.max_retries", Type: FieldTypeInt, Default: "1", RuntimeEditable: true, ReadOnly: false, MinInt: 0},
		{Key: "backoff_cap_sec", DisplayName: "Backoff Cap (sec)", Scope: FieldScopeProvider, TomlPath: "provider.%s.backoff_cap_sec", Type: FieldTypeInt, Default: "120", RuntimeEditable: true, ReadOnly: false, MinInt: 1},
		{Key: "backoff_multiplier", DisplayName: "Backoff Multiplier", Scope: FieldScopeProvider, TomlPath: "provider.%s.backoff_multiplier", Type: FieldTypeFloat64, Default: "2", RuntimeEditable: true, ReadOnly: false, MinInt: 0},
		{Key: "cb_reset_sec", DisplayName: "Circuit Breaker Reset (sec)", Scope: FieldScopeProvider, TomlPath: "provider.%s.cb_reset_sec", Type: FieldTypeInt, Default: "30", RuntimeEditable: true, ReadOnly: false, MinInt: 1},
		{Key: "upstream_cb_threshold", DisplayName: "Upstream CB Threshold", Scope: FieldScopeProvider, TomlPath: "provider.%s.upstream_cb_threshold", Type: FieldTypeInt, Default: "5", RuntimeEditable: true, ReadOnly: false, MinInt: 1},
		{Key: "http_timeout_sec", DisplayName: "HTTP Timeout (sec)", Scope: FieldScopeProvider, TomlPath: "provider.%s.http_timeout_sec", Type: FieldTypeInt, Default: "30", RuntimeEditable: true, ReadOnly: false, MinInt: 1},
		{Key: "log_level", DisplayName: "Log Level", Scope: FieldScopeProvider, TomlPath: "provider.%s.log_level", Type: FieldTypeString, Default: "info", RuntimeEditable: true, ReadOnly: false, MinInt: 0},
		{Key: "health_check_interval_sec", DisplayName: "Health Check Interval (sec)", Scope: FieldScopeProvider, TomlPath: "provider.%s.health_check_interval_sec", Type: FieldTypeInt, Default: "30", RuntimeEditable: false, ReadOnly: false, MinInt: 0},
		{Key: "admin_token", DisplayName: "Admin Token", Scope: FieldScopeProvider, TomlPath: "provider.%s.admin_token", Type: FieldTypeString, Default: "", RuntimeEditable: false, ReadOnly: true, MinInt: 0},
		{Key: "disable_thinking", DisplayName: "Disable Thinking", Scope: FieldScopeProvider, TomlPath: "provider.%s.disable_thinking", Type: FieldTypeBool, Default: "false", RuntimeEditable: false, ReadOnly: false, MinInt: 0},
		{Key: "thinking_mode", DisplayName: "Thinking Mode", Scope: FieldScopeProvider, TomlPath: "provider.%s.thinking_mode", Type: FieldTypeString, Default: "default", RuntimeEditable: true, ReadOnly: false, MinInt: 0},
		{Key: "rectify_thinking_map_to", DisplayName: "Rectify Thinking Map To", Scope: FieldScopeProvider, TomlPath: "provider.%s.rectify_thinking_map_to", Type: FieldTypeString, Default: "", RuntimeEditable: true, ReadOnly: false, MinInt: 0},
		{Key: "genai_model", DisplayName: "GenAI Model", Scope: FieldScopeProvider, TomlPath: "provider.%s.genai_model", Type: FieldTypeString, Default: "", RuntimeEditable: false, ReadOnly: false, MinInt: 0},
		{Key: "keys_file", DisplayName: "Keys File", Scope: FieldScopeProvider, TomlPath: "provider.%s.keys_file", Type: FieldTypeString, Default: "keys.json", RuntimeEditable: false, ReadOnly: true, MinInt: 0},
		{Key: "key_selection", DisplayName: "Key Selection Mode", Scope: FieldScopeProvider, TomlPath: "provider.%s.key_selection", Type: FieldTypeString, Default: "polling", RuntimeEditable: false, ReadOnly: false, MinInt: 0},
		{Key: "port", DisplayName: "Port", Scope: FieldScopeGlobal, TomlPath: "port", Type: FieldTypeInt, Default: "8080", RuntimeEditable: false, ReadOnly: true, MinInt: 0},
		{Key: "log_file", DisplayName: "Log File", Scope: FieldScopeGlobal, TomlPath: "log_file", Type: FieldTypeString, Default: "", RuntimeEditable: false, ReadOnly: true, MinInt: 0},
	}

	if len(got) != len(want) {
		t.Fatalf("descriptor count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("descriptor[%d] (%s) =\n  got  %+v\n  want %+v", i, want[i].Key, got[i], want[i])
		}
	}
}
