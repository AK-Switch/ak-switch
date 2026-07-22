## akswitch provider remove

Remove a provider

### Synopsis

Remove a provider from the TOML configuration.

This only removes the provider configuration; any associated keys file
is NOT deleted. Use 'akswitch key remove' to manage individual keys.

Example:
  akswitch provider remove nvidia

```
akswitch provider remove <name> [flags]
```

### Options

```
  -h, --help   help for remove
```

### SEE ALSO

* [akswitch provider](akswitch_provider.md)	 - Manage providers

