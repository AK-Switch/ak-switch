## akswitch key import

Import API keys from a file, stdin, or command line (with dedup and auto-numbering)

### Synopsis

Import one or more API keys for the specified provider.

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

```
akswitch key import <provider> [keys...] [flags]
```

### Options

```
  -f, --file string        Import keys from a JSON file
  -h, --help               help for import
      --insecure-storage   Store keys in plaintext (WARNING: not encrypted)
  -n, --name string        Display name for imported keys
```

### SEE ALSO

* [akswitch key](akswitch_key.md)	 - Manage API keys

