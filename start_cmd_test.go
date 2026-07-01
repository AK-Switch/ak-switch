package main

import (
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"alvus/internal/cmd"
)

// ── Test: alvus start (TOML 模式，完整启动) ─────────────────
//
// 子进程模式，完整模拟用户操作链：
//
//	provider add → key add → alvus start
//
// 验证：服务器启动后 health endpoint 可达。
// 这是关键路径测试——如果此测试通过，说明：
//   - TOML 配置加载正确（DetectConfigSource → LoadAllTomlProviders）
//   - 加密存储 Key 加载正确（loadKeysForProvider 的 fallback 路径）
//   - Validate 在 Key 加载后执行（顺序正确）
//   - InstanceManager 端口绑定正确
//   - HTTP handler 注册正确
func TestStartCmd_TOMLMode(t *testing.T) {
	if os.Getenv("ALVUS_TEST_START_CHILD") == "1" {
		os.Args = []string{"alvus", "start", "--local"}
		cmd.Execute("")
		return
	}

	// ── 主进程 ──
	resetConfigEnv()
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	// provider add 会自动创建 config.toml（不需要先 config init）
	runCommand(t, "alvus", "provider", "add", "testp",
		"--target", "http://localhost:18999/v1",
		"--genai", "http://localhost:18999",
		"--port", "19301",
	)
	runCommand(t, "alvus", "key", "add", "testp", "sk-test-key-12345")

	// 获取测试二进制路径（在 runCommand 修改 os.Args 后仍可用）
	testExe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable failed: %v", err)
	}

	// 启动子进程（运行同一测试函数，走 ALVUS_TEST_START_CHILD 分支）
	cmd := exec.Command(testExe, "-test.run=^TestStartCmd_TOMLMode$")
	cmd.Env = append(os.Environ(),
		"ALVUS_TEST_START_CHILD=1",
		"XDG_CONFIG_HOME="+tmpDir,
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("subprocess start failed: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// 轮询等待服务器就绪（最多 2 秒）
	var healthOK bool
	for i := 0; i < 20; i++ {
		resp, err := http.Get("http://127.0.0.1:19301/health")
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 {
				healthOK = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !healthOK {
		t.Fatal("server health check never returned 200 within 2s — possible startup flow bug")
	}
}

// ── Test: alvus start — 缺少 Key 时的错误处理 ──────────────
func TestStartCmd_NoKeys(t *testing.T) {
	if os.Getenv("ALVUS_TEST_START_CHILD") == "1" {
		os.Args = []string{"alvus", "start", "--local"}
		cmd.Execute("")
		return
	}

	resetConfigEnv()
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	runCommand(t, "alvus", "provider", "add", "nokey",
		"--target", "http://localhost:18999/v1",
		"--genai", "http://localhost:18999",
		"--port", "19302",
	)
	// 故意不加 Key

	testExe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable failed: %v", err)
	}

	cmd := exec.Command(testExe, "-test.run=^TestStartCmd_NoKeys$")
	cmd.Env = append(os.Environ(),
		"ALVUS_TEST_START_CHILD=1",
		"XDG_CONFIG_HOME="+tmpDir,
	)
	out, err := cmd.CombinedOutput()
	output := string(out)

	if err == nil {
		t.Fatal("expected error for missing keys, got exit code 0")
	}
	if !strings.Contains(output, "no instances configured") &&
		!strings.Contains(output, "no API keys") {
		t.Errorf("expected error about missing keys in output, got:\n%s", output)
	}
}

// ── Test: alvus start — 端口冲突时的错误处理 ──────────────
func TestStartCmd_PortConflict(t *testing.T) {
	if os.Getenv("ALVUS_TEST_START_CHILD") == "1" {
		os.Args = []string{"alvus", "start", "--local"}
		cmd.Execute("")
		return
	}

	resetConfigEnv()
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	// 先占一个端口
	ln, err := net.Listen("tcp", "127.0.0.1:19303")
	if err != nil {
		t.Fatalf("failed to pre-bind port 19303: %v", err)
	}
	defer ln.Close()

	// 第一个 provider 使用被占用的端口
	runCommand(t, "alvus", "provider", "add", "conflict",
		"--target", "http://localhost:18999/v1",
		"--genai", "http://localhost:18999",
		"--port", "19303",
	)
	runCommand(t, "alvus", "key", "add", "conflict", "sk-test-key-999")

	// 第二个 provider 使用可用端口，验证它不受影响
	runCommand(t, "alvus", "provider", "add", "okay",
		"--target", "http://localhost:18999/v1",
		"--genai", "http://localhost:18999",
		"--port", "19304",
	)
	runCommand(t, "alvus", "key", "add", "okay", "sk-test-key-888")

	testExe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable failed: %v", err)
	}

	cmd := exec.Command(testExe, "-test.run=^TestStartCmd_PortConflict$")
	cmd.Env = append(os.Environ(),
		"ALVUS_TEST_START_CHILD=1",
		"XDG_CONFIG_HOME="+tmpDir,
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("subprocess start failed: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// 等待服务器就绪
	time.Sleep(500 * time.Millisecond)

	// 验证可用端口上的 provider 能响应
	resp, err := http.Get("http://127.0.0.1:19304/health")
	if err != nil {
		t.Fatalf("healthy provider not reachable: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("healthy provider returned %d, want 200", resp.StatusCode)
	}
}