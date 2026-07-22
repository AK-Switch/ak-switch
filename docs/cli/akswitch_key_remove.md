## akswitch key remove

Remove an API key by index or name

### Synopsis

Remove an API key from the provider's key store at the specified index or matching name.

	The index corresponds to the key's position as shown in 'akswitch key list'.
	Use --by-name to look up a key by its display name instead.
	This operation cannot be undone.

	Examples:
	  akswitch key remove nvidia 0
	  akswitch key remove nvidia my-key --by-name

```
akswitch key remove <provider> <index> [flags]
```

### Options

```
      --by-name   Look up key by name instead of index
  -h, --help      help for remove
```

### SEE ALSO

* [akswitch key](akswitch_key.md)	 - Manage API keys

