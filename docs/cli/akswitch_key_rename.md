## akswitch key rename

Rename an API key

### Synopsis

Change the display name of an API key at the specified index or matching name.

By default, the second argument is treated as an index.
Use --by-name to treat it as a name to match.

Examples:
  akswitch key rename sensenova 0 d1-2
  akswitch key rename sensenova d1-2 d1-3 --by-name

```
akswitch key rename <provider> <index> <new-name> [flags]
```

### Options

```
      --by-name   Look up key by name instead of index
  -h, --help      help for rename
```

### SEE ALSO

* [akswitch key](akswitch_key.md)	 - Manage API keys

