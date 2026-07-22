## akswitch provider list

List all providers

### Synopsis

Display all configured providers and their settings from config.toml.

Example output:
  Providers (from /home/user/.config/akswitch/config.toml):
    NAME        TARGET                                            PORT
    nvidia      https://integrate.api.nvidia.com/v1               3002
    sensenova   https://api.sensenova.com/v1                      3001

```
akswitch provider list [flags]
```

### Options

```
  -h, --help   help for list
```

### SEE ALSO

* [akswitch provider](akswitch_provider.md)	 - Manage providers

