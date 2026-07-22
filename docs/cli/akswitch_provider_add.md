## akswitch provider add

Add a new provider

### Synopsis

Add a new provider to the TOML configuration.

The --target flag is required. --port is required for the first provider;
subsequent providers reuse the existing port and --port can be omitted.

Example:
  akswitch provider add nvidia --target https://integrate.api.nvidia.com/v1 --port 3002
  akswitch provider add sensenova --target https://api.sensenova.com/v1

```
akswitch provider add <name> [flags]
```

### Options

```
  -c, --cooldown-sec int   Cooldown seconds after rate-limit (default 60)
      --default            Set this provider as the default
  -g, --genai string       GenAI base URL (optional)
  -h, --help               help for add
  -r, --max-retries int    Max retry attempts for upstream (default 3)
  -p, --port int           HTTP listen port (required for first provider)
  -t, --target string      Upstream target URL (required)
```

### SEE ALSO

* [akswitch provider](akswitch_provider.md)	 - Manage providers

