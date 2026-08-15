package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"akswitch/internal/config"
	"akswitch/internal/keypool"
	"akswitch/internal/logentry"

	"github.com/spf13/cobra"
)

// KeyMutation represents the operation to perform on a key.
type KeyMutation int

const (
	KeyEnable KeyMutation = iota
	KeyDisable
)

// updateKey performs a KeyMutation on a provider's key at the given index.
// It handles the full load-validate-modify-save-reload cycle.
func updateKey(provider string, idx int, op KeyMutation) error {
	store, err := keypool.LoadKeys(provider)
	if err != nil {
		return fmt.Errorf("failed to load keys for %q: %w", provider, err)
	}
	if store == nil {
		return fmt.Errorf("no keys found for provider %q", provider)
	}

	if idx < 0 || idx >= len(store.Keys) {
		return fmt.Errorf("index %d out of range: provider %q has %d keys (valid: 0-%d)",
			idx, provider, len(store.Keys), len(store.Keys)-1)
	}

	// Capture entry for display before mutation
	entry := store.Keys[idx]
	desc := logentry.MaskKey(entry.Key)
	if entry.Name != "" {
		desc += fmt.Sprintf(" (name: %s)", entry.Name)
	}

	switch op {
	case KeyEnable:
		store.Keys[idx].Disabled = false
	case KeyDisable:
		store.Keys[idx].Disabled = true
	}

	if err := keypool.SaveKeys(provider, store); err != nil {
		return fmt.Errorf("failed to save keys for %q: %w", provider, err)
	}

	switch op {
	case KeyEnable:
		fmt.Printf("Enabled key [%d] %s for provider %q\n", idx, desc, provider)
	case KeyDisable:
		fmt.Printf("Disabled key [%d] %s for provider %q\n", idx, desc, provider)
	}

	triggerReload()
	return nil
}

// addKeyIndexFlags registers the standard --by-name flag on a command
// that accepts a key index or name. Using this factory function ensures
// all key-index commands consistently support --by-name.
func addKeyIndexFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("by-name", false, "Look up key by name instead of index")
}

func init() {
	rootCmd.AddCommand(keyCmd)
	keyCmd.AddCommand(keyAddCmd)
	keyCmd.AddCommand(keyListCmd)
	keyCmd.AddCommand(keyRemoveCmd)
	keyCmd.AddCommand(keyDisableCmd)
	keyCmd.AddCommand(keyEnableCmd)
	keyCmd.AddCommand(keyUpdateCmd)
	keyCmd.AddCommand(keyImportCmd)
	keyCmd.AddCommand(keyCooldownCmd)
	keyImportCmd.Flags().StringP("file", "f", "", "Import keys from a JSON file")
	keyImportCmd.Flags().StringP("name", "n", "", "Display name for imported keys")
	keyImportCmd.Flags().Bool("insecure-storage", false, "Store keys in plaintext (WARNING: not encrypted)")
	keyImportCmd.Flags().StringP("target", "t", "", "Upstream target base URL (required with --create when provider is missing)")
	keyImportCmd.Flags().Bool("create", false, "Auto-create the provider if it doesn't exist")

	keyUpdateCmd.Flags().StringP("name", "n", "", "New display name for the key")
	addKeyIndexFlags(keyRemoveCmd)
	addKeyIndexFlags(keyDisableCmd)
	addKeyIndexFlags(keyEnableCmd)
	addKeyIndexFlags(keyUpdateCmd)

	keyAddCmd.Flags().StringP("name", "n", "", "Display name for the key")
	keyAddCmd.Flags().Bool("insecure-storage", false, "Store keys in plaintext (WARNING: not encrypted)")
	keyListCmd.Flags().Bool("runtime", false, "Query live status from running server (shows cooldown, RPM)")
	addKeyIndexFlags(keyCooldownCmd)
	keyCmd.AddCommand(keyExportCmd)
	keyExportCmd.Flags().StringP("output", "o", "", "Write to file instead of stdout")
	keyCmd.AddCommand(keyUpstreamCBResetCmd)
	keyCmd.AddCommand(keyRestoreCmd)
	keyCmd.AddCommand(keyPurgeCmd)
	keyListCmd.Flags().Bool("all", false, "Show all keys including deleted ones")
	addKeyIndexFlags(keyRestoreCmd)
}

var keyCmd = &cobra.Command{
	Use:   "key",
	Short: "Manage API keys",
	Long:  `Add, list, remove, update, disable, enable, export, restore, and purge API keys for a provider.`,
}

var keyAddCmd = &cobra.Command{
	Use:   "add <provider> <key>",
	Short: "Add a new API key for a provider",
	Long: `Add a new API key to the key store for the specified provider.

The key is added to the system keyring (or encrypted file fallback).
If the store does not exist, it is created.
Use --insecure-storage to store keys in plaintext (CI/disposable environments).

Example:
  akswitch key add nvidia sk-xxxxxxxxxxxxxxxx
  akswitch key add nvidia sk-xxxxxxxxxxxxxxxx --name my-key
  akswitch key add nvidia sk-xxxxxxxxxxxxxxxx --insecure-storage`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := args[0]
		apiKey := args[1]
		name, _ := cmd.Flags().GetString("name")

		insecure, _ := cmd.Flags().GetBool("insecure-storage")
		if insecure {
			fmt.Fprintln(os.Stderr, "WARNING: API keys will be stored in plaintext (not encrypted).")
			fmt.Fprintln(os.Stderr, "Use this only in CI or environments without a system keyring.")
			fmt.Fprintln(os.Stderr, "Do not use this on a shared machine.")
		}

		store, err := keypool.LoadKeys(provider)
		if err != nil {
			return fmt.Errorf("failed to load keys for %q: %w", provider, err)
		}
		if store == nil {
			store = &keypool.KeyStore{Keys: []keypool.KeyEntry{}}
		}

		store.Keys = append(store.Keys, keypool.KeyEntry{
			Key:  apiKey,
			Name: name,
		})

		if insecure {
			if err := keypool.SaveKeysInsecure(provider, store); err != nil {
				return fmt.Errorf("failed to save keys for %q: %w", provider, err)
			}
		} else {
			if err := keypool.SaveKeys(provider, store); err != nil {
				return fmt.Errorf("failed to save keys for %q: %w", provider, err)
			}
		}

		fmt.Printf("Key added to provider %q (total: %d keys)\n", provider, len(store.Keys))
		triggerReload()
		return nil
	},
}
var keyImportCmd = &cobra.Command{
	Use:   "import <provider> [keys...]",
	Short: "Import API keys from a file, stdin, or command line (with dedup and auto-numbering)",
	Long: `Import one or more API keys for the specified provider.

Keys can be provided as command-line arguments, from a JSON file, or from stdin.

JSON file format:
  ["key1", "key2", "key3"]
  or
  [{"key": "key1", "name": "name1"}, {"key": "key2"}]

	JSONL file format (one JSON object per line):
	  {"key": "sk-xxx", "name": "my-key"}
	  {"api_key": "sk-xxx", "api_key_name": "my-key"}
	  {"api_key_plain": "sk-xxx"}

Examples:
  akswitch key import nvidia sk-1 sk-2 sk-3
  akswitch key import nvidia --file keys.json
  cat keys.json | akswitch key import nvidia
  akswitch key import nvidia --file credentials.jsonl
  cat keys.jsonl | akswitch key import nvidia

	CSV format:
	  The --file flag also accepts CSV files. The first row may be a header
	  with columns "key_name" and "api_key" (or "api_key_account"). Without
	  a header, the file must have one column (API key only) or two columns
	  (key name, API key).

	  Examples:
	    akswitch key import nvidia --file keys.csv
	    akswitch key import nvidia --file keys.csv --name "batch"`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := args[0]
		keyArgs := args[1:]

		filePath, _ := cmd.Flags().GetString("file")
		name, _ := cmd.Flags().GetString("name")
		insecure, _ := cmd.Flags().GetBool("insecure-storage")

		// Parse input source
		var entries []keypool.KeyEntry
		if filePath != "" {
			data, err := os.ReadFile(filePath)
			if err != nil {
				return fmt.Errorf("failed to read %q: %w", filePath, err)
			}
			entries, err = parseKeyEntries(data)
			if err != nil {
				return fmt.Errorf("failed to parse %q: %w", filePath, err)
			}
		} else if len(keyArgs) > 0 {
			for _, k := range keyArgs {
				entries = append(entries, keypool.KeyEntry{Key: k, Name: name})
			}
		} else {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("failed to read stdin: %w", err)
			}
			if len(data) == 0 {
				return fmt.Errorf("no input provided: specify keys as arguments, use --file, or pipe to stdin")
			}
			entries, err = parseKeyEntries(data)
			if err != nil {
				return fmt.Errorf("failed to parse stdin: %w", err)
			}
			if name != "" {
				for i := range entries {
					if entries[i].Name == "" {
						entries[i].Name = name
					}
				}
			}
		}

		if len(entries) == 0 {
			return fmt.Errorf("no keys to import")
		}

		if insecure {
			fmt.Fprintln(os.Stderr, "WARNING: API keys will be stored in plaintext (not encrypted).")
			fmt.Fprintln(os.Stderr, "Use this only in CI or environments without a system keyring.")
			fmt.Fprintln(os.Stderr, "Do not use this on a shared machine.")
		}

		create, _ := cmd.Flags().GetBool("create")
		target, _ := cmd.Flags().GetString("target")

		if create {
			xdgPath, err := config.XDGConfigPath()
			if err != nil {
				return fmt.Errorf("failed to determine config path: %w", err)
			}
			tc, err := config.LoadTomlConfig(xdgPath)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to load config: %w", err)
			}
			providerExists := false
			if tc != nil {
				_, providerExists = tc.Provider[provider]
			}
			if !providerExists {
				if target == "" {
					return fmt.Errorf("--target is required when --create creates a new provider")
				}
				// Add provider to config
				if tc == nil {
					tc = &config.TomlConfig{Provider: make(map[string]*config.Config)}
				} else if tc.Provider == nil {
					tc.Provider = make(map[string]*config.Config)
				}
				tc.Provider[provider] = &config.Config{ProviderConfig: config.ProviderConfig{TargetBase: target}}
				if err := config.SaveTomlConfig(tc, xdgPath); err != nil {
					return fmt.Errorf("failed to save config with new provider: %w", err)
				}
				fmt.Printf("Created provider %q with target %q\n", provider, target)
			}
		}

		store, err := keypool.LoadKeys(provider)
		if err != nil {
			return fmt.Errorf("failed to load keys for %q: %w", provider, err)
		}
		if store == nil {
			store = &keypool.KeyStore{Keys: []keypool.KeyEntry{}}
		}

		// Auto-number duplicate names before dedup
		entries = autoNumberNames(entries, store)

		// Dedup against existing keys
		newEntries, skipped := dedupEntries(entries, store)
		store.Keys = append(store.Keys, newEntries...)

		if insecure {
			if err := keypool.SaveKeysInsecure(provider, store); err != nil {
				return fmt.Errorf("failed to save keys for %q: %w", provider, err)
			}
		} else {
			if err := keypool.SaveKeys(provider, store); err != nil {
				return fmt.Errorf("failed to save keys for %q: %w", provider, err)
			}
		}

		// Build stats output
		added := len(newEntries)
		total := len(store.Keys)
		names := make([]string, 0, added)
		for _, e := range newEntries {
			if e.Name != "" {
				names = append(names, e.Name)
			}
		}
		nameList := ""
		if len(names) > 0 {
			nameList = fmt.Sprintf(" (%s)", strings.Join(names, ", "))
		}
		fmt.Printf("Imported %d key(s) to provider %q\n", len(entries), provider)
		fmt.Printf("  ✅ Added: %d%s\n", added, nameList)
		if skipped > 0 {
			fmt.Printf("  ⏭️  Skipped: %d (already exists)\n", skipped)
		}
		fmt.Printf("  Total: %d keys\n", total)
		triggerReload()
		return nil
	},
}

var keyUpdateCmd = &cobra.Command{
	Use:   "update <provider> <index> [key]",
	Short: "Update an API key at the specified index",
	Long: `Replace an existing API key at the specified index with a new key value,
or rename it without changing the value.

The key's position, disabled state, and circuit breaker state are preserved.
Only --name without [key] renames the key without changing its value.

Examples:
  akswitch key update sensenova 0 sk-xxxxxxxxxxxxxxxx
  akswitch key update sensenova 0 --name d1-2
  akswitch key update sensenova d1-2 sk-xxxxxxxxxxxxxxxx --by-name`,
	Args: cobra.RangeArgs(2, 3),
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := args[0]
		store, err := keypool.LoadKeys(provider)
		if err != nil {
			return fmt.Errorf("failed to load keys for %q: %w", provider, err)
		}
		if store == nil {
			return fmt.Errorf("no keys found for provider %q", provider)
		}

		var idx int
		if byName, _ := cmd.Flags().GetBool("by-name"); byName {
			idx, err = findKeyIndexByName(store, args[1])
			if err != nil {
				return err
			}
		} else {
			idx, err = strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("invalid index %q: must be a non-negative integer", args[1])
			}
		}

		if idx < 0 || idx >= len(store.Keys) {
			return fmt.Errorf("index %d out of range: provider %q has %d keys (valid: 0-%d)",
				idx, provider, len(store.Keys), len(store.Keys)-1)
		}

		// Handle optional key value
		if len(args) == 3 {
			newKey := args[2]
			oldMasked := logentry.MaskKey(store.Keys[idx].Key)
			store.Keys[idx].Key = newKey
			fmt.Printf("Updated key [%d] %s -> %s for provider %q\n",
				idx, oldMasked, logentry.MaskKey(newKey), provider)
		}

		// Handle --name (always works, with or without key)
		if cmd.Flags().Changed("name") {
			newName, _ := cmd.Flags().GetString("name")
			oldName := store.Keys[idx].Name
			store.Keys[idx].Name = newName
			fmt.Printf("Renamed key [%d] from %q to %q for provider %q\n",
				idx, oldName, newName, provider)
		}

		// No changes at all? Error
		if len(args) == 2 && !cmd.Flags().Changed("name") {
			return fmt.Errorf("nothing to update: provide a new key value or --name")
		}

		if err := keypool.SaveKeys(provider, store); err != nil {
			return fmt.Errorf("failed to save keys for %q: %w", provider, err)
		}

		triggerReload()
		return nil
	},
}

var keyCooldownCmd = &cobra.Command{
	Use:   "cooldown <provider> <index>",
	Short: "Force a key into cooldown",
	Long: `Force an API key into cooldown for the configured cooldown duration.
Use --by-name to look up a key by its display name instead.

Calls the running server's management API.

Examples:
  akswitch key cooldown nvidia 1
  akswitch key cooldown nvidia my-key --by-name`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := args[0]
		idx, err := resolveKeyIndex(cmd, args)
		if err != nil {
			return err
		}

		// Try runtime API
		runtimeErr := callKeyRuntimeAPI(provider, idx, "cooldown")
		if runtimeErr == nil {
			return nil
		}

		return fmt.Errorf("server not running — start akswitch to use runtime cooldown: %w", runtimeErr)
	},
}

var keyListCmd = &cobra.Command{
	Use:   "list <provider>",
	Short: "List API keys for a provider",
	Long: `Display all API keys for the specified provider with their index,
masked value, status, and optional name.

Use --runtime to query the running server for live status including cooldown info.

Example output:
  Keys for provider "nvidia":
    [0] sk-****xx  (active)
    [1] sk-****yy  (disabled)
    [2] sk-****zz  (active)  name: my-key`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := args[0]
		runtime, _ := cmd.Flags().GetBool("runtime")

		if runtime {
			return keyListRuntime(provider)
		}

		store, err := keypool.LoadKeys(provider)
		if err != nil {
			return fmt.Errorf("failed to load keys for %q: %w", provider, err)
		}

		if store == nil || len(store.Keys) == 0 {
			fmt.Printf("No keys found for provider %q\n", provider)
			return nil
		}

		showAll, _ := cmd.Flags().GetBool("all")
		fmt.Printf("Keys for provider %q:\n", provider)
		for i, entry := range store.Keys {
			if entry.Deleted && !showAll {
				continue
			}
			status := "active"
			if entry.Disabled {
				status = "disabled"
			}
			if entry.Deleted {
				status = "deleted"
			}
			line := fmt.Sprintf("  [%d] %s  (%s)", i, logentry.MaskKey(entry.Key), status)
			if entry.Name != "" {
				line += fmt.Sprintf("  name: %s", entry.Name)
			}
			fmt.Println(line)
		}

		return nil
	},
}

var keyRemoveCmd = &cobra.Command{
	Use:   "remove <provider> <index>",
	Short: "Remove (soft delete) an API key by index or name",
	Long: `Mark an API key as deleted at the specified index or matching name.

	The index corresponds to the key's position as shown in 'akswitch key list'.
	Use --by-name to look up a key by its display name instead.
	Deleted keys are hidden from 'key list' but can be restored with 'key restore'.
	Use 'key purge' to permanently delete all marked keys.

	Examples:
	  akswitch key remove nvidia 0
	  akswitch key remove nvidia my-key --by-name`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		idx, err := resolveKeyIndex(cmd, args)
		if err != nil {
			return err
		}
		store, err := keypool.LoadKeys(args[0])
		if err != nil {
			return fmt.Errorf("failed to load keys for %q: %w", args[0], err)
		}
		if store == nil || idx >= len(store.Keys) {
			return fmt.Errorf("index %d out of range", idx)
		}
		store.Keys[idx].Deleted = true
		if err := keypool.SaveKeys(args[0], store); err != nil {
			return fmt.Errorf("failed to save keys: %w", err)
		}
		desc := logentry.MaskKey(store.Keys[idx].Key)
		if store.Keys[idx].Name != "" {
			desc += fmt.Sprintf(" (name: %s)", store.Keys[idx].Name)
		}
		fmt.Printf("Deleted key [%d] %s for provider %q (use 'key restore' to undo)\n", idx, desc, args[0])
		triggerReload()
		return nil
	},
}

var keyDisableCmd = &cobra.Command{
	Use:   "disable <provider> <index>",
	Short: "Disable an API key by index or name",
	Long: `Mark an API key as disabled at the specified index or matching name.

	Disabled keys are not used for new requests but remain in the key store.
	Use --by-name to look up a key by its display name instead.
	Use 'akswitch key remove' to soft-delete a key (recoverable via 'key restore').
	Use 'akswitch key purge' to permanently remove deleted keys.

	Examples:
	  akswitch key disable nvidia 1
	  akswitch key disable nvidia my-key --by-name`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		idx, err := resolveKeyIndex(cmd, args)
		if err != nil {
			return err
		}
		return updateKey(args[0], idx, KeyDisable)
	},
}

var keyEnableCmd = &cobra.Command{
	Use:   "enable <provider> <index>",
	Short: "Enable an API key by index or name",
	Long: `Re-enable a previously disabled API key at the specified index or matching name.

	The key will be used again for new requests.  The operation triggers a
	reload so the server picks up the change.
	Use --by-name to look up a key by its display name instead.

	Examples:
	  akswitch key enable nvidia 1
	  akswitch key enable nvidia my-key --by-name`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		idx, err := resolveKeyIndex(cmd, args)
		if err != nil {
			return err
		}
		return updateKey(args[0], idx, KeyEnable)
	},
}

// resetUpstreamCB sends a POST to the running server to reset the upstream
// circuit breaker for the specified provider.
func resetUpstreamCB(provider string) error {
	client, err := NewAdminClient(5*time.Second, provider)
	if err != nil {
		return err
	}
	path := "/api/stats/reset-upstream-cb?provider=" + url.QueryEscape(provider)
	resp, err := client.Post(path, "application/json", nil)
	if err != nil {
		return fmt.Errorf("server not reachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("auth failed (HTTP %d): check X-Admin-Token in server config", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

var keyUpstreamCBResetCmd = &cobra.Command{
	Use:   "upstream-cb-reset <provider>",
	Short: "Reset the upstream circuit breaker for a provider",
	Long: `Force-close the upstream circuit breaker for a provider.

The upstream circuit breaker opens after repeated 502/503 responses.
This command resets it so the provider can resume sending requests
immediately without waiting for the recovery timeout.

Examples:
  akswitch key upstream-cb-reset nvidia
  akswitch key upstream-cb-reset sensenova`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(os.Stderr, "WARNING: 'key upstream-cb-reset' is deprecated, use 'provider upstream-cb-reset' instead")
		return resetUpstreamCB(args[0])
	},
}

var keyRestoreCmd = &cobra.Command{
	Use:   "restore <provider> <index>",
	Short: "Restore a previously deleted API key",
	Long: `Restore a soft-deleted API key. The key is no longer deleted and
	appears in key list again. Use 'key enable' separately if it was disabled.
Use --by-name to look up a key by its display name.
Use 'key list --all' to see deleted keys.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		idx, err := resolveKeyIndex(cmd, args)
		if err != nil {
			return err
		}
		store, err := keypool.LoadKeys(args[0])
		if err != nil {
			return fmt.Errorf("failed to load keys for %q: %w", args[0], err)
		}
		if store == nil || idx >= len(store.Keys) {
			return fmt.Errorf("index %d out of range", idx)
		}
		if !store.Keys[idx].Deleted {
			return fmt.Errorf("key [%d] is not deleted", idx)
		}
		store.Keys[idx].Deleted = false
		if err := keypool.SaveKeys(args[0], store); err != nil {
			return fmt.Errorf("failed to save keys: %w", err)
		}
		desc := logentry.MaskKey(store.Keys[idx].Key)
		if store.Keys[idx].Name != "" {
			desc += fmt.Sprintf(" (name: %s)", store.Keys[idx].Name)
		}
		fmt.Printf("Restored key [%d] %s for provider %q\n", idx, desc, args[0])
		triggerReload()
		return nil
	},
}

var keyPurgeCmd = &cobra.Command{
	Use:   "purge <provider>",
	Short: "Permanently remove all deleted keys",
	Long:  `Remove all soft-deleted API keys permanently. This cannot be undone.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := args[0]
		store, err := keypool.LoadKeys(provider)
		if err != nil {
			return fmt.Errorf("failed to load keys for %q: %w", provider, err)
		}
		if store == nil {
			return fmt.Errorf("no keys found for provider %q", provider)
		}

		var remaining []keypool.KeyEntry
		purged := 0
		for _, entry := range store.Keys {
			if entry.Deleted {
				purged++
				continue
			}
			remaining = append(remaining, entry)
		}
		if purged == 0 {
			fmt.Printf("No deleted keys to purge for provider %q\n", provider)
			return nil
		}

		store.Keys = remaining
		if err := keypool.SaveKeys(provider, store); err != nil {
			return fmt.Errorf("failed to save keys: %w", err)
		}
		fmt.Printf("Purged %d deleted key(s) from provider %q (remaining: %d)\n", purged, provider, len(remaining))
		triggerReload()
		return nil
	},
}

var keyExportCmd = &cobra.Command{
	Use:   "export <provider>",
	Short: "Export API keys to stdout or a file",
	Long: `Export all API keys for a provider as JSON.

By default, prints to stdout. Use --output to write to a file.
Keys are decrypted automatically (supports encrypted storage).

Examples:
  akswitch key export nvidia
  akswitch key export nvidia --output keys.json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := args[0]
		outputPath, _ := cmd.Flags().GetString("output")

		store, err := keypool.LoadKeys(provider)
		if err != nil {
			return fmt.Errorf("failed to load keys for %q: %w", provider, err)
		}
		if store == nil || len(store.Keys) == 0 {
			return fmt.Errorf("no keys found for provider %q", provider)
		}

		data, err := json.MarshalIndent(store, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to serialize keys: %w", err)
		}

		if outputPath != "" {
			// Check if file exists
			if _, err := os.Stat(outputPath); err == nil {
				fmt.Fprintf(os.Stderr, "WARNING: %s already exists, overwriting\n", outputPath)
			}
			if err := os.WriteFile(outputPath, data, 0600); err != nil {
				return fmt.Errorf("failed to write %q: %w", outputPath, err)
			}
			fmt.Printf("Exported %d keys for provider %q to %s\n", len(store.Keys), provider, outputPath)
		} else {
			fmt.Println(string(data))
		}
		return nil
	},
}

// resolveKeyIndex resolves a key index from command arguments.
// If --by-name is set, looks up the index by name; otherwise parses it as an integer.
func resolveKeyIndex(cmd *cobra.Command, args []string) (int, error) {
	if byName, _ := cmd.Flags().GetBool("by-name"); byName {
		store, err := keypool.LoadKeys(args[0])
		if err != nil {
			return 0, fmt.Errorf("failed to load keys for %q: %w", args[0], err)
		}
		if store == nil {
			return 0, fmt.Errorf("no keys found for provider %q", args[0])
		}
		return findKeyIndexByName(store, args[1])
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return 0, fmt.Errorf("invalid index %q: must be a non-negative integer", args[1])
	}
	return idx, nil
}

// findKeyIndexByName searches a KeyStore for a key with the given name.
// Returns an error if the name is not found or if multiple keys share the name.
func findKeyIndexByName(store *keypool.KeyStore, name string) (int, error) {
	matches := []int{}
	for i, entry := range store.Keys {
		if entry.Name == name {
			matches = append(matches, i)
		}
	}
	if len(matches) == 0 {
		return 0, fmt.Errorf("no key found with name %q", name)
	}
	if len(matches) > 1 {
		return 0, fmt.Errorf("multiple keys (%d) found with name %q, use index instead", len(matches), name)
	}
	return matches[0], nil
}

// loadAdminToken loads an admin token from the TOML config file.
// If provider is non-empty, looks up that specific provider's token.
// If provider is empty, returns the first non-empty token from any provider.
func loadAdminToken(provider string) (string, error) {
	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		return "", err
	}
	tc, err := config.LoadTomlConfig(xdgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if provider != "" {
		if p, ok := tc.Provider[provider]; ok {
			return p.AdminToken, nil
		}
		return "", nil
	}
	for _, p := range tc.Provider {
		if p.AdminToken != "" {
			return p.AdminToken, nil
		}
	}
	return "", nil
}

// callKeyRuntimeAPI sends a key operation request to the running server.
// Supported operations: "cooldown", "enable", "disable".
// Returns nil on success, error if server is unreachable or API returns an error.
func callKeyRuntimeAPI(provider string, idx int, operation string) error {
	client, err := NewAdminClient(5*time.Second, provider)
	if err != nil {
		return fmt.Errorf("server not reachable: %w", err)
	}

	path := fmt.Sprintf("/api/keys/%d/%s?provider=%s", idx, operation, url.QueryEscape(provider))
	var reqBody io.Reader
	if operation == "cooldown" {
		reqBody = strings.NewReader(`{}`)
	}
	method := http.MethodPost
	if operation == "cooldown" {
		method = http.MethodPut
	}
	req, err := http.NewRequest(method, client.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("server not reachable: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("server not reachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("auth failed (HTTP %d): check X-Admin-Token in server config", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// keyListRuntime queries the running server's /api/keys endpoint for live status.
func keyListRuntime(provider string) error {
	client, err := NewAdminClient(5*time.Second, provider)
	if err != nil {
		return fmt.Errorf("server not reachable: %w", err)
	}

	path := "/api/keys?provider=" + url.QueryEscape(provider)
	resp, err := client.Get(path)
	if err != nil {
		return fmt.Errorf("server not reachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("auth failed (HTTP %d): check X-Admin-Token in server config", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var keys []map[string]interface{}
	if err := json.Unmarshal(body, &keys); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if len(keys) == 0 {
		fmt.Printf("No keys found for provider %q\n", provider)
		return nil
	}

	fmt.Printf("Keys for provider %q (runtime):\n", provider)
	for _, k := range keys {
		idxFloat, ok := k["index"].(float64)
		if !ok {
			continue
		}
		idx := int(idxFloat) - 1
		masked, _ := k["key"].(string)
		status, _ := k["status"].(string)
		rpmFloat, ok := k["requests_1m"].(float64)
		if !ok {
			continue
		}
		name, _ := k["name"].(string)

		line := fmt.Sprintf("  [%d] %s  (%s, %d rpm)", idx, masked, status, int(rpmFloat))
		if name != "" {
			line += fmt.Sprintf("  name: %s", name)
		}
		fmt.Println(line)
	}
	return nil
}

// parseJSONL parses newline-delimited JSON (JSONL) data into KeyEntry slices.
// Each line must be a JSON object with "key" (or "api_key" / "api_key_plain") and
// optionally "name" (or "api_key_name").
// Supports: {"key": "sk-xxx", "name": "my-key"} or {"api_key": "sk-xxx", "api_key_name": "my-key"}
func parseJSONL(data []byte) ([]keypool.KeyEntry, error) {
	lines := strings.Split(string(data), "\n")
	var entries []keypool.KeyEntry
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Try {"key": "xxx", "name": "yyy"} format
		var entry keypool.KeyEntry
		if err := json.Unmarshal([]byte(line), &entry); err == nil && entry.Key != "" {
			entries = append(entries, entry)
			continue
		}
		// Try {"api_key": "xxx", "api_key_name": "yyy"} format
		var jsonlEntry struct {
			Key    string `json:"api_key"`
			KeyAlt string `json:"api_key_plain"`
			Name   string `json:"api_key_name"`
		}
		if err := json.Unmarshal([]byte(line), &jsonlEntry); err != nil {
			return nil, fmt.Errorf("failed to parse JSONL line: %w", err)
		}
		key := jsonlEntry.Key
		if key == "" {
			key = jsonlEntry.KeyAlt
		}
		if key == "" {
			return nil, fmt.Errorf("JSONL line missing key field (no 'key', 'api_key', or 'api_key_plain'): %s", line)
		}
		entries = append(entries, keypool.KeyEntry{Key: key, Name: jsonlEntry.Name})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no valid key entries found in JSONL data")
	}
	return entries, nil
}

// dedupEntries filters out entries whose keys already exist in the store.
// Returns the deduplicated entries and the count of skipped duplicates.
func dedupEntries(entries []keypool.KeyEntry, store *keypool.KeyStore) ([]keypool.KeyEntry, int) {
	existing := make(map[string]bool, len(store.Keys))
	for _, e := range store.Keys {
		existing[e.Key] = true
	}
	var newEntries []keypool.KeyEntry
	skipped := 0
	for _, e := range entries {
		if existing[e.Key] {
			skipped++
			continue
		}
		newEntries = append(newEntries, e)
		existing[e.Key] = true
	}
	return newEntries, skipped
}

// autoNumberNames appends a sequential suffix (-1, -2, ...) to every named entry
// so that keys sharing the same name get unique numbered names.
// Entries with empty names are left unchanged.
// If store is non-nil, existing numbered entries in the store are used to
// determine the starting suffix, preventing cross-batch name collisions.
func autoNumberNames(entries []keypool.KeyEntry, store *keypool.KeyStore) []keypool.KeyEntry {
	// Scan existing store for the max suffix of each base name
	// e.g., "auto-reg-4" → base="auto-reg", suffix=4
	nameMaxSuffix := make(map[string]int)
	if store != nil {
		for _, e := range store.Keys {
			if e.Name == "" {
				continue
			}
			if idx := strings.LastIndexByte(e.Name, '-'); idx > 0 {
				suffix := e.Name[idx+1:]
				if n, err := strconv.Atoi(suffix); err == nil {
					base := e.Name[:idx]
					if n > nameMaxSuffix[base] {
						nameMaxSuffix[base] = n
					}
				}
			}
		}
	}

	nameCount := make(map[string]int)
	for _, e := range entries {
		if e.Name == "" {
			continue
		}
		nameCount[e.Name]++
	}
	// Only add suffix if the name appears more than once
	nameIndex := make(map[string]int)
	// Initialize from store's max suffix to avoid cross-batch collisions
	for name := range nameCount {
		if nameCount[name] > 1 {
			if maxN, ok := nameMaxSuffix[name]; ok {
				nameIndex[name] = maxN
			}
		}
	}
	for i, e := range entries {
		if e.Name == "" {
			continue
		}
		if nameCount[e.Name] > 1 {
			nameIndex[e.Name]++
			entries[i].Name = fmt.Sprintf("%s-%d", e.Name, nameIndex[e.Name])
		}
	}
	return entries
}

// parseCSV parses CSV data into KeyEntry slices.
// Parsing rules:
//  1. Lines starting with '#' are skipped (comments)
//  2. If the first non-comment line contains known column headers,
//     columns are mapped by header name (case-insensitive)
//  3. If no header is detected, positional inference is used:
//     - 1 column → key
//     - 2 columns → name, key
//     - 3+ columns → error
//  4. Leading/trailing whitespace is stripped from each cell
func parseCSV(data []byte) ([]keypool.KeyEntry, error) {
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("empty CSV data")
	}

	keyColNames := map[string]bool{
		"key": true, "api_key": true, "api key": true,
		"token": true, "secret": true, "apikey": true,
	}
	nameColNames := map[string]bool{
		"name": true, "key_name": true, "account_name": true,
		"username": true, "user": true, "account": true, "备注": true,
	}

	contentStart := 0
	for contentStart < len(lines) {
		trimmed := strings.TrimSpace(lines[contentStart])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			contentStart++
			continue
		}
		break
	}
	if contentStart >= len(lines) {
		return nil, fmt.Errorf("no data found in CSV (all lines are comments or empty)")
	}

	firstLine := strings.TrimSpace(lines[contentStart])
	cols := strings.Split(firstLine, ",")
	for i := range cols {
		cols[i] = strings.TrimSpace(cols[i])
	}

	isHeader := false
	keyCol := -1
	nameCol := -1
	for _, c := range cols {
		lower := strings.ToLower(c)
		if keyColNames[lower] {
			isHeader = true
			break
		}
		if nameColNames[lower] {
			isHeader = true
			break
		}
	}

	dataStart := contentStart
	if isHeader {
		for i, c := range cols {
			lower := strings.ToLower(c)
			if keyColNames[lower] {
				keyCol = i
			}
			if nameColNames[lower] {
				nameCol = i
			}
		}
		if keyCol == -1 {
			return nil, fmt.Errorf("CSV has header but no key column found (known names: key, api_key, token, secret)")
		}
		dataStart = contentStart + 1
	}

	var entries []keypool.KeyEntry
	for i := dataStart; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cells := strings.Split(line, ",")
		for j := range cells {
			cells[j] = strings.TrimSpace(cells[j])
		}

		if isHeader {
			if keyCol >= len(cells) {
				continue
			}
			entry := keypool.KeyEntry{Key: cells[keyCol]}
			if nameCol >= 0 && nameCol < len(cells) {
				entry.Name = cells[nameCol]
			}
			entries = append(entries, entry)
		} else {
			switch len(cells) {
			case 1:
				entries = append(entries, keypool.KeyEntry{Key: cells[0]})
			case 2:
				entries = append(entries, keypool.KeyEntry{Name: cells[0], Key: cells[1]})
			default:
				return nil, fmt.Errorf("line %d: cannot infer CSV column mapping with %d columns (no header detected)", i+1, len(cells))
			}
		}
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("no valid key entries found in CSV data")
	}
	return entries, nil
}

// parseKeyEntries parses key data into KeyEntry slices.
// Supports:
//   - JSON array of strings: ["key1", "key2"]
//   - JSON array of objects: [{"key": "key1", "name": "n1"}, ...]
//   - JSONL (newline-delimited JSON): {"key": "key1", "name": "n1"}\n{"key": "key2"}
func parseKeyEntries(data []byte) ([]keypool.KeyEntry, error) {
	// Try JSON array of strings first
	var keys []string
	if err := json.Unmarshal(data, &keys); err == nil {
		entries := make([]keypool.KeyEntry, len(keys))
		for i, k := range keys {
			entries[i] = keypool.KeyEntry{Key: k}
		}
		return entries, nil
	}

	// Try JSON array of objects
	var entries []keypool.KeyEntry
	if err := json.Unmarshal(data, &entries); err == nil {
		return entries, nil
	}

	// Try JSONL (data starts with '{')
	if len(data) > 0 && data[0] == '{' {
		jsonlEntries, jsonlErr := parseJSONL(data)
		if jsonlErr == nil {
			return jsonlEntries, nil
		}
	}

	// Try CSV
	if len(data) > 0 {
		csvEntries, csvErr := parseCSV(data)
		if csvErr == nil {
			return csvEntries, nil
		}
	}

	return nil, fmt.Errorf("expected JSON array of strings, key objects, JSONL, or CSV format")
}
