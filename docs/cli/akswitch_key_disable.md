## akswitch key disable

Disable an API key by index or name

### Synopsis

Mark an API key as disabled at the specified index or matching name.

	Disabled keys are not used for new requests but remain in the key store.
	Use --by-name to look up a key by its display name instead.
	Use 'akswitch key remove' to permanently remove a key.

	Examples:
	  akswitch key disable nvidia 1
	  akswitch key disable nvidia my-key --by-name

```
akswitch key disable <provider> <index> [flags]
```

### Options

```
      --by-name   Look up key by name instead of index
  -h, --help      help for disable
```

### SEE ALSO

* [akswitch key](akswitch_key.md)	 - Manage API keys

