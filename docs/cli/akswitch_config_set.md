## akswitch config set

Set a runtime parameter

### Synopsis

Change a runtime-configurable parameter immediately.

	Use --persist to also write the change to the config file.

	Valid keys: http_timeout_sec, max_retries, cooldown_sec, backoff_cap_sec,
	backoff_multiplier, cb_reset_sec, upstream_cb_threshold, log_level

	Examples:
	  akswitch config set http_timeout_sec 60
	  akswitch config set max_retries 5 --persist
	  akswitch config set log_level debug sensenova

```
akswitch config set <key> <value> [provider] [flags]
```

### Options

```
  -h, --help      help for set
      --persist   Persist the change to the config file
```

### SEE ALSO

* [akswitch config](akswitch_config.md)	 - Manage configuration

