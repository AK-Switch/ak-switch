## akswitch key update

Update an API key at the specified index

### Synopsis

Replace an existing API key at the specified index with a new key value.

The key's position, disabled state, and circuit breaker state are preserved.
Use --name to optionally rename the key.

Examples:
  akswitch key update sensenova 0 sk-xxxxxxxxxxxxxxxx
  akswitch key update sensenova 0 sk-xxxxxxxxxxxxxxxx --name d1-2
  akswitch key update sensenova d1-2 sk-xxxxxxxxxxxxxxxx --by-name

```
akswitch key update <provider> <index> <key> [flags]
```

### Options

```
      --by-name       Look up key by name instead of index
  -h, --help          help for update
  -n, --name string   New display name for the key
```

### SEE ALSO

* [akswitch key](akswitch_key.md)	 - Manage API keys

