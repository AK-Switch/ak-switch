//go:build unit

package cli

import "testing"

func TestConfigInitCmd_Flags(t *testing.T) {
	flags := []string{"path"}
	for _, f := range flags {
		t.Run(f, func(t *testing.T) {
			if configInitCmd.Flags().Lookup(f) == nil {
				t.Fatalf("expected --%s flag on config init command", f)
			}
		})
	}
}

func TestConfigListCmd_Exists(t *testing.T) {
	if configListCmd == nil {
		t.Fatal("configListCmd is nil")
	}
	if configListCmd.Use != "list [provider]" {
		t.Errorf("expected Use 'list [provider]', got %q", configListCmd.Use)
	}
}

func TestConfigGetCmd_Exists(t *testing.T) {
	if configGetCmd == nil {
		t.Fatal("configGetCmd is nil")
	}
	if configGetCmd.Use != "get <key> [provider]" {
		t.Errorf("expected Use 'get <key> [provider]', got %q", configGetCmd.Use)
	}
}

func TestConfigSetCmd_Exists(t *testing.T) {
	if configSetCmd == nil {
		t.Fatal("configSetCmd is nil")
	}
	if configSetCmd.Use != "set <key> <value> [provider]" {
		t.Errorf("expected Use 'set <key> <value> [provider]', got %q", configSetCmd.Use)
	}
}

func TestConfigSetCmd_HasPersistFlag(t *testing.T) {
	if configSetCmd.Flags().Lookup("persist") == nil {
		t.Fatal("expected --persist flag on config set command")
	}
}