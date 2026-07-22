## akswitch start

Start the API key rotation proxy server

### Synopsis

Loads TOML configuration, initializes the key pool, and starts the HTTP proxy server on a single port with path-based provider routing.

```
akswitch start [flags]
```

### Options

```
      --all                 Start all providers (default: first provider alphabetically, or error if none configured)
      --dev                 Start in development mode with auto-incrementing port
  -h, --help                help for start
      --log-format string   Log output format: default or compact (default "compact")
      --provider string     Only start the specified provider
```

### SEE ALSO

* [akswitch](akswitch.md)	 - API Key rotation proxy for AI providers

