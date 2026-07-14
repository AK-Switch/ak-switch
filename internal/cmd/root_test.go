//go:build unit

package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"akswitch/internal/config"
	"path/filepath"
)

func TestDetectServerPort_Default(t *testing.T) {
	tmpDir := t.TempDir()
	old := config.ConfigDir
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = old })

	port := detectServerPort()
	if port != adminPort {
		t.Errorf("detectServerPort() = %d, want %d (adminPort)", port, adminPort)
	}
}

func TestDetectServerPort_FromConfigFile(t *testing.T) {
	tmpDir := t.TempDir()

	tomlPath := filepath.Join(tmpDir, "config.toml")
	content := `port = 9090

[provider]
[provider.test]
target = "http://example.com"
genai = "http://example.com"
`
	if err := os.WriteFile(tomlPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config.toml: %v", err)
	}

	old := config.ConfigDir
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = old })

	port := detectServerPort()
	if port != 9090 {
		t.Errorf("detectServerPort() = %d, want 9090", port)
	}
}

func TestVersionCommand(t *testing.T) {
	// Verify default version variable
	if Version != "dev" {
		t.Errorf("Version = %q, want %q", Version, "dev")
	}
	if rootCmd.Version != "dev" {
		t.Errorf("rootCmd.Version = %q, want %q", rootCmd.Version, "dev")
	}

	// Test version command output by capturing stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = w

	rootCmd.SetArgs([]string{"version"})
	if err := rootCmd.Execute(); err != nil {
		w.Close()
		os.Stdout = oldStdout
		t.Fatal(err)
	}

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	r.Close()

	output := strings.TrimSpace(buf.String())
	expected := "akswitch version dev"
	if output != expected {
		t.Errorf("version output = %q, want %q", output, expected)
	}
}
