package config

import "testing"

func TestThatFails(t *testing.T) {
t.Fatalf("故意失败：验证 CI 门控是否跳过 review")
}
