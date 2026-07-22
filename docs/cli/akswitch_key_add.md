## akswitch key add

Add a new API key for a provider

### Synopsis

Add a new API key to the key store for the specified provider.

The key is added to the system keyring (or encrypted file fallback).
If the store does not exist, it is created.
Use --insecure-storage to store keys in plaintext (CI/disposable environments).

Example:
  akswitch key add nvidia sk-xxxxxxxxxxxxxxxx
  akswitch key add nvidia sk-xxxxxxxxxxxxxxxx --name my-key
  akswitch key add nvidia sk-xxxxxxxxxxxxxxxx --insecure-storage

```
akswitch key add <provider> <key> [flags]
```

### Options

```
  -h, --help               help for add
      --insecure-storage   Store keys in plaintext (WARNING: not encrypted)
  -n, --name string        Display name for the key
```

### SEE ALSO

* [akswitch key](akswitch_key.md)	 - Manage API keys

