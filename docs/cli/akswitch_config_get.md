## akswitch config get

Get a runtime parameter

### Synopsis

Display the current value of a single runtime-configurable parameter.

	Valid keys: http_timeout_sec, max_retries, cooldown_sec, backoff_cap_sec,
	backoff_multiplier, cb_reset_sec, upstream_cb_threshold, log_level

	Examples:
	  akswitch config get http_timeout_sec
	  akswitch config get max_retries sensenova

```
akswitch config get <key> [provider] [flags]
```

### Options

```
  -h, --help   help for get
```

### SEE ALSO

* [akswitch config](akswitch_config.md)	 - Manage configuration

