## akswitch key list

List API keys for a provider

### Synopsis

Display all API keys for the specified provider with their index,
masked value, status, and optional name.

Example output:
  Keys for provider "nvidia":
    [0] sk-****xx  (active)
    [1] sk-****yy  [disabled]
    [2] sk-****zz  (active)  name: my-key

```
akswitch key list <provider> [flags]
```

### Options

```
  -h, --help   help for list
```

### SEE ALSO

* [akswitch key](akswitch_key.md)	 - Manage API keys

