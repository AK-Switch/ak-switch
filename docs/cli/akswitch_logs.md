## akswitch logs

Show request logs

### Synopsis

Display recent request logs from the running akswitch server.

```
akswitch logs [flags]
```

### Options

```
      --compact        Use compact format (TTFB, total time, body sizes)
  -h, --help           help for logs
      --last int       Show only the last N entries (0 = all)
      --since string   Show entries after this timestamp (RFC3339, e.g. 2026-07-14T00:00:00Z)
      --verbose        Show full request details (method, URL, body size)
```

### SEE ALSO

* [akswitch](akswitch.md)	 - API Key rotation proxy for AI providers

