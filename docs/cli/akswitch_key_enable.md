## akswitch key enable

Enable an API key by index or name

### Synopsis

Re-enable a previously disabled API key at the specified index or matching name.

	The key will be used again for new requests.  The operation triggers a
	reload so the server picks up the change.
	Use --by-name to look up a key by its display name instead.

	Examples:
	  akswitch key enable nvidia 1
	  akswitch key enable nvidia my-key --by-name

```
akswitch key enable <provider> <index> [flags]
```

### Options

```
      --by-name   Look up key by name instead of index
  -h, --help      help for enable
```

### SEE ALSO

* [akswitch key](akswitch_key.md)	 - Manage API keys

