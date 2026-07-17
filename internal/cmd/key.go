package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"

	"akswitch/internal/keypool"
	"akswitch/internal/utils"

	"github.com/spf13/cobra"
)

// KeyMutation represents the operation to perform on a key.
type KeyMutation int

const (
	KeyEnable  KeyMutation = iota
	KeyDisable
	KeyRemove
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
	desc := utils.MaskKey(entry.Key)
	if entry.Name != "" {
		desc += fmt.Sprintf(" (name: %s)", entry.Name)
	}

	switch op {
	case KeyEnable:
		store.Keys[idx].Disabled = false
	case KeyDisable:
		store.Keys[idx].Disabled = true
	case KeyRemove:
		store.Keys = append(store.Keys[:idx], store.Keys[idx+1:]...)
	}

	if err := keypool.SaveKeys(provider, store); err != nil {
		return fmt.Errorf("failed to save keys for %q: %w", provider, err)
	}

	switch op {
	case KeyEnable:
		fmt.Printf("Enabled key [%d] %s for provider %q\n", idx, desc, provider)
	case KeyDisable:
		fmt.Printf("Disabled key [%d] %s for provider %q\n", idx, desc, provider)
	case KeyRemove:
		fmt.Printf("Removed key [%d] %s from provider %q (remaining: %d keys)\n", idx, desc, provider, len(store.Keys))
	}

	triggerReload()
	return nil
}

func init() {
	rootCmd.AddCommand(keyCmd)
	keyCmd.AddCommand(keyAddCmd)
	keyCmd.AddCommand(keyListCmd)
	keyCmd.AddCommand(keyRemoveCmd)
	keyCmd.AddCommand(keyDisableCmd)
	keyCmd.AddCommand(keyEnableCmd)
	keyCmd.AddCommand(keyImportCmd)
	keyImportCmd.Flags().StringP("file", "f", "", "Import keys from a JSON file")
	keyImportCmd.Flags().StringP("name", "n", "", "Display name for imported keys")
	keyImportCmd.Flags().Bool("insecure-storage", false, "Store keys in plaintext (WARNING: not encrypted)")

	keyAddCmd.Flags().StringP("name", "n", "", "Display name for the key")
	keyAddCmd.Flags().Bool("insecure-storage", false, "Store keys in plaintext (WARNING: not encrypted)")
}

var keyCmd = &cobra.Command{
	Use:   "key",
	Short: "Manage API keys",
	Long:  `Add, list, remove, disable, and enable API keys for a provider.`,
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
	Short: "Import API keys from a file, stdin, or command line",
	Long: `Import one or more API keys for the specified provider.

Keys can be provided as command-line arguments, from a JSON file, or from stdin.

JSON file format:
  ["key1", "key2", "key3"]
  or
  [{"key": "key1", "name": "name1"}, {"key": "key2"}]

Examples:
  akswitch key import nvidia sk-1 sk-2 sk-3
  akswitch key import nvidia --file keys.json
  cat keys.json | akswitch key import nvidia`,
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

		store, err := keypool.LoadKeys(provider)
		if err != nil {
			return fmt.Errorf("failed to load keys for %q: %w", provider, err)
		}
		if store == nil {
			store = &keypool.KeyStore{Keys: []keypool.KeyEntry{}}
		}

		store.Keys = append(store.Keys, entries...)

		if insecure {
			if err := keypool.SaveKeysInsecure(provider, store); err != nil {
				return fmt.Errorf("failed to save keys for %q: %w", provider, err)
			}
		} else {
			if err := keypool.SaveKeys(provider, store); err != nil {
				return fmt.Errorf("failed to save keys for %q: %w", provider, err)
			}
		}

					fmt.Printf("Imported %d key(s) to provider %q (total: %d keys)\n", len(entries), provider, len(store.Keys))
		triggerReload()
		return nil
	},
}
var keyListCmd = &cobra.Command{
	Use:   "list <provider>",
	Short: "List API keys for a provider",
	Long: `Display all API keys for the specified provider with their index,
masked value, status, and optional name.

Example output:
  Keys for provider "nvidia":
    [0] sk-****xx  (active)
    [1] sk-****yy  [disabled]
    [2] sk-****zz  (active)  name: my-key`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := args[0]

		store, err := keypool.LoadKeys(provider)
		if err != nil {
			return fmt.Errorf("failed to load keys for %q: %w", provider, err)
		}

		if store == nil || len(store.Keys) == 0 {
			fmt.Printf("No keys found for provider %q\n", provider)
			return nil
		}

		fmt.Printf("Keys for provider %q:\n", provider)
		for i, entry := range store.Keys {
			status := "active"
			if entry.Disabled {
				status = "disabled"
			}
			line := fmt.Sprintf("  [%d] %s  (%s)", i, utils.MaskKey(entry.Key), status)
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
	Short: "Remove an API key by index",
	Long: `Remove an API key from the provider's key store at the specified index.

The index corresponds to the key's position as shown in 'akswitch key list'.
This operation cannot be undone.

Example:
  akswitch key remove nvidia 0`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		idx, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("invalid index %q: must be a non-negative integer", args[1])
		}
		return updateKey(args[0], idx, KeyRemove)
	},
}

var keyDisableCmd = &cobra.Command{
	Use:   "disable <provider> <index>",
	Short: "Disable an API key by index",
	Long: `Mark an API key as disabled at the specified index.

Disabled keys are not used for new requests but remain in the key store.
Use 'akswitch key remove' to permanently remove a key.

Example:
  akswitch key disable nvidia 1`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		idx, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("invalid index %q: must be a non-negative integer", args[1])
		}
		return updateKey(args[0], idx, KeyDisable)
	},
}

var keyEnableCmd = &cobra.Command{
	Use:   "enable <provider> <index>",
	Short: "Enable an API key by index",
	Long: `Re-enable a previously disabled API key at the specified index.

The key will be used again for new requests.  The operation triggers a
reload so the server picks up the change.

Example:
  akswitch key enable nvidia 1`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		idx, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("invalid index %q: must be a non-negative integer", args[1])
		}
		return updateKey(args[0], idx, KeyEnable)
	},
}

// parseKeyEntries parses JSON key data into KeyEntry slices.
// Supports: ["key1", "key2"] or [{"key": "key1", "name": "n1"}, ...]
func parseKeyEntries(data []byte) ([]keypool.KeyEntry, error) {
	// Try array of strings first
	var keys []string
	if err := json.Unmarshal(data, &keys); err == nil {
		entries := make([]keypool.KeyEntry, len(keys))
		for i, k := range keys {
			entries[i] = keypool.KeyEntry{Key: k}
		}
		return entries, nil
	}

	// Try array of objects
	var entries []keypool.KeyEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("expected JSON array of strings or key objects: %w", err)
	}
	return entries, nil
}
