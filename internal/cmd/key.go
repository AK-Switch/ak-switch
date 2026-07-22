package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

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
	keyCmd.AddCommand(keyRenameCmd)
	keyCmd.AddCommand(keyImportCmd)
	keyImportCmd.Flags().StringP("file", "f", "", "Import keys from a JSON file")
	keyImportCmd.Flags().StringP("name", "n", "", "Display name for imported keys")
	keyImportCmd.Flags().Bool("insecure-storage", false, "Store keys in plaintext (WARNING: not encrypted)")

	keyUpdateCmd.Flags().StringP("name", "n", "", "New display name for the key")
	addKeyIndexFlags(keyRemoveCmd)
	addKeyIndexFlags(keyDisableCmd)
	addKeyIndexFlags(keyEnableCmd)
	addKeyIndexFlags(keyUpdateCmd)
	addKeyIndexFlags(keyRenameCmd)

	keyAddCmd.Flags().StringP("name", "n", "", "Display name for the key")
	keyAddCmd.Flags().Bool("insecure-storage", false, "Store keys in plaintext (WARNING: not encrypted)")
}

var keyCmd = &cobra.Command{
	Use:   "key",
	Short: "Manage API keys",
	Long:  `Add, list, remove, update, rename, disable, and enable API keys for a provider.`,
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
  cat keys.jsonl | akswitch key import nvidia`,
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

		// Auto-number duplicate names before dedup
			entries = autoNumberNames(entries)

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
	Use:   "update <provider> <index> <key>",
	Short: "Update an API key at the specified index",
	Long: `Replace an existing API key at the specified index with a new key value.

The key's position, disabled state, and circuit breaker state are preserved.
Use --name to optionally rename the key.

Examples:
  akswitch key update sensenova 0 sk-xxxxxxxxxxxxxxxx
  akswitch key update sensenova 0 sk-xxxxxxxxxxxxxxxx --name d1-2
  akswitch key update sensenova d1-2 sk-xxxxxxxxxxxxxxxx --by-name`,
	Args: cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := args[0]
		newKey := args[2]

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

		oldMasked := utils.MaskKey(store.Keys[idx].Key)
		store.Keys[idx].Key = newKey

		if cmd.Flags().Changed("name") {
			newName, _ := cmd.Flags().GetString("name")
			store.Keys[idx].Name = newName
		}

		if err := keypool.SaveKeys(provider, store); err != nil {
			return fmt.Errorf("failed to save keys for %q: %w", provider, err)
		}

		fmt.Printf("Updated key [%d] %s -> %s for provider %q\n",
			idx, oldMasked, utils.MaskKey(newKey), provider)
		triggerReload()
		return nil
	},
}

var keyRenameCmd = &cobra.Command{
	Use:   "rename <provider> <index> <new-name>",
	Short: "Rename an API key",
	Long: `Change the display name of an API key at the specified index or matching name.

By default, the second argument is treated as an index.
Use --by-name to treat it as a name to match.

Examples:
  akswitch key rename sensenova 0 d1-2
  akswitch key rename sensenova d1-2 d1-3 --by-name`,
	Args: cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := args[0]
		newName := args[2]

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

		oldName := store.Keys[idx].Name
		store.Keys[idx].Name = newName

		if err := keypool.SaveKeys(provider, store); err != nil {
			return fmt.Errorf("failed to save keys for %q: %w", provider, err)
		}

		fmt.Printf("Renamed key [%d] from %q to %q for provider %q\n",
			idx, oldName, newName, provider)
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
	Short: "Remove an API key by index or name",
	Long: `Remove an API key from the provider's key store at the specified index or matching name.

	The index corresponds to the key's position as shown in 'akswitch key list'.
	Use --by-name to look up a key by its display name instead.
	This operation cannot be undone.

	Examples:
	  akswitch key remove nvidia 0
	  akswitch key remove nvidia my-key --by-name`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		idx, err := resolveKeyIndex(cmd, args)
		if err != nil {
			return err
		}
		return updateKey(args[0], idx, KeyRemove)
	},
}

var keyDisableCmd = &cobra.Command{
	Use:   "disable <provider> <index>",
	Short: "Disable an API key by index or name",
	Long: `Mark an API key as disabled at the specified index or matching name.

	Disabled keys are not used for new requests but remain in the key store.
	Use --by-name to look up a key by its display name instead.
	Use 'akswitch key remove' to permanently remove a key.

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
func autoNumberNames(entries []keypool.KeyEntry) []keypool.KeyEntry {
	nameCount := make(map[string]int)
	for _, e := range entries {
		if e.Name == "" {
			continue
		}
		nameCount[e.Name]++
	}
	// Only add suffix if the name appears more than once
	nameIndex := make(map[string]int)
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

	return nil, fmt.Errorf("expected JSON array of strings, key objects, or JSONL format")
}
