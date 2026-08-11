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

func TestConfigInitCmd_Exists(t *testing.T) {
	if configInitCmd == nil {
		t.Fatal("configInitCmd is nil")
	}
	if configInitCmd.Use != "init" {
		t.Errorf("expected Use 'init', got %q", configInitCmd.Use)
	}
}

func TestConfigViewCmd_Exists(t *testing.T) {
	if configViewCmd == nil {
		t.Fatal("configViewCmd is nil")
	}
	if configViewCmd.Use != "view" {
		t.Errorf("expected Use 'view', got %q", configViewCmd.Use)
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

func TestConfigListCmd_HasAllFlag(t *testing.T) {
	if configListCmd.Flags().Lookup("all") == nil {
		t.Fatal("expected --all flag on config list command")
	}
}

func TestConfigGetCmd_HasAllFlag(t *testing.T) {
	if configGetCmd.Flags().Lookup("all") == nil {
		t.Fatal("expected --all flag on config get command")
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

func TestConfigSetCmd_HasAllFlag(t *testing.T) {
	if configSetCmd.Flags().Lookup("all") == nil {
		t.Fatal("expected --all flag on config set command")
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
