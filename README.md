# ⚡ Alvus

> **~5 MB binary. Zero dependencies. Zero 429s.**
> A lightweight Go proxy that silently absorbs rate limit errors and keeps your AI agent running.

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat-square&logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg?style=flat-square)](LICENSE)
[![Zero Dependencies](https://img.shields.io/badge/dependencies-zero-brightgreen?style=flat-square)]()
[![Works with OpenClaw](https://img.shields.io/badge/works%20with-OpenClaw-orange?style=flat-square)]()
[![Works with Cline](https://img.shields.io/badge/works%20with-Cline-blueviolet?style=flat-square)]()
[![Works with Cursor](https://img.shields.io/badge/works%20with-Cursor-blue?style=flat-square)]()
[![Works with Claude Code](https://img.shields.io/badge/works%20with-Claude%20Code-d97757?style=flat-square)](#claude-code-experimental)

---

## The Problem

You're in the middle of an agentic session — OpenClaw is halfway through a task, Cline is on a roll, your agent is _doing things_ — and then:

```
Error: 429 Too Many Requests
```

The loop breaks. Context is lost. You're staring at a spinner.

If you use free-tier providers like **NVIDIA NIM**, this happens constantly. Free keys cap around 40 RPM. One productive session burns through that in seconds.

## The Solution

Alvus sits between your agent and the upstream API. You give it a pool of keys. It handles everything else — round-robin distribution, per-key cooldowns, automatic retries, streaming passthrough. Your agent never sees a 429.

```
Any OpenAI-compatible agent or IDE
              │
              ▼
   ┌─────────────────────┐
   │        Alvus        │  ← localhost:3000
   │                     │
   │  [key1] ✅ ready    │
   │  [key2] ✅ ready    │  ──→  NVIDIA NIM / any OpenAI-compatible API
   │  [key3] ❄️ cooling  │
   └─────────────────────┘
```

3 keys × 40 RPM = 120+ effective RPM. The math is simple. The setup is simpler.

> **Idle RAM usage: ~2 MB.** Alvus is a single static binary with no runtime. It won't compete with your models for memory.

---

## Works With Everything

If it speaks OpenAI-compatible API, it works with Alvus.

| Tool                                             | Type              | Setup                               |
| ------------------------------------------------ | ----------------- | ----------------------------------- |
| [OpenClaw](https://github.com/openclaw/openclaw) | AI agent          | Set base URL in provider config     |
| [PicoClaw](https://github.com/sipeed/picoclaw)   | Lightweight agent | Set `api_base` in config.json       |
| [Nanobot](https://github.com/HKUDS/nanobot)      | Lightweight agent | Set `api_base` in config.yaml       |
| [Cline](https://github.com/cline/cline)          | VS Code agent     | OpenAI Compatible provider          |
| [Cursor](https://cursor.sh)                      | IDE               | Base URL override in settings       |
| [Aider](https://aider.chat)                      | CLI agent         | `--openai-api-base` flag            |
| Any OpenAI-compatible client                     | —                 | Point at `http://localhost:3000/v1` |

---

## Features

|                                    |                                                                             |
| ---------------------------------- | --------------------------------------------------------------------------- |
| 🔑 **Key pool**                    | Multiple keys, one endpoint. Distribute load transparently                  |
| 🔄 **Round-robin**                 | Even distribution across all healthy keys                                   |
| 🚫 **Silent retry on 429/502/503** | Failed key enters cooldown, request retries instantly with the next         |
| ⏱️ **Retry-After support**         | Respects upstream `Retry-After` headers — no blind fixed waits              |
| 🔑 **Auto-disable on 401/403**     | Invalid or revoked keys are permanently removed from the pool               |
| 📡 **Streaming passthrough**       | SSE and chunked responses piped with zero buffering overhead                |
| ❤️ **Health endpoint**             | `GET /health` shows live key status, cooldown timers, and requests/minute   |
| 🖥️ **Interactive Dashboard**      | `GET /dashboard` — Premium Glassmorphism Dark UI for real-time monitoring   |
| ⚡ **Live Activity Logs**          | Searchable, 1000-entry memory cache to track all request activity          |
| 🔧 **Dynamic Configuration**      | Update keys and base URLs directly from the dashboard; writes to `.env`     |
| 🤖 **Claude Code mode**            | Translates Anthropic Messages API ⇄ OpenAI so Claude Code runs on any backend |
| 🪶 **Zero dependencies**           | Pure Go stdlib. One binary                                                  |
| 🔧 **`.env` support**              | Built-in parser — no `godotenv`, no extras                                  |
| 🖥️ **Runs anywhere**               | linux/amd64, arm64, arm, **386** — including Pi Zero and older x86 hardware |
| 💾 **~2 MB idle RAM**              | Static binary, no runtime, won't compete with your models for memory        |

---

## Quickstart

### 1. Get the binary

**Build from source** (requires Go 1.21+):

```bash
git clone https://github.com/YOUR_USERNAME/alvus.git
cd alvus
go build -o alvus *.go
```

**Cross-compile for a remote server** (e.g. Raspberry Pi Zero, 32-bit x86):

```bash
# Pi Zero / older ARM
GOOS=linux GOARCH=arm CGO_ENABLED=0 go build -o alvus *.go

# 32-bit x86 (Atom, old netbooks, salvaged hardware)
GOOS=linux GOARCH=386 CGO_ENABLED=0 go build -o alvus *.go
```

The binary is fully static — drop it on the machine and run it. No runtime, no dependencies, no install step.

**Download a prebuilt release:**

Go to [Releases](../../releases) and grab the binary for your platform.

---

### 2. Configure

Create `.env` in the same directory as the binary:

```env
# Your API keys, comma-separated
API_KEYS=nvapi-xxxxxxxxxxxx,nvapi-yyyyyyyyyyyy,nvapi-zzzzzzzzzzzz

# Port to listen on (default: 3000)
PORT=3000

# Upstream API base URL (default: NVIDIA NIM)
TARGET_BASE_URL=https://integrate.api.nvidia.com/v1

# Seconds to cool down a key after a 429, 502, or 503 (default: 60)
COOLDOWN_SEC=60
```

Real environment variables take precedence over `.env` — useful for systemd or containers.

---

### 3. Run

```bash
./alvus
```

```
⚡ Alvus started on :3000
   Target  : https://integrate.api.nvidia.com/v1
   Keys    : 3 loaded
   Cooldown: 60s per key on 429/502/503
```

---

### 4. Point your agent at it

#### OpenClaw

```json
{
  "models": {
    "providers": {
      "nim": {
        "baseUrl": "http://localhost:3000/v1",
        "apiKey": "sk-proxy-dummy"
      }
    },
    "defaults": {
      "provider": "nim",
      "model": "deepseek-ai/deepseek-r1"
    }
  }
}
```

#### PicoClaw / Nanobot

```json
{
  "model_name": "deepseek-r1",
  "model": "openai/deepseek-ai/deepseek-r1",
  "api_base": "http://localhost:3000/v1",
  "api_keys": ["sk-proxy-dummy"]
}
```

#### Cline (VS Code)

| Setting      | Value                           |
| ------------ | ------------------------------- |
| API Provider | `OpenAI Compatible`             |
| Base URL     | `http://localhost:3000/v1`      |
| API Key      | `sk-proxy-dummy` _(any string)_ |
| Model ID     | `deepseek-ai/deepseek-r1`       |

#### Cursor

Settings → Models → set base URL to `http://localhost:3000/v1`, any dummy key.

#### Aider

```bash
aider --openai-api-base http://localhost:3000/v1 --openai-api-key sk-dummy
```

---

## How It Works

```
1. Request arrives from your agent or IDE
2. Body is buffered (needed for retry replay)
3. Round-robin picks the next available key
4. Request forwarded upstream with that key injected
   │
   ├── ✅ 2xx/3xx → request count incremented, headers + body streamed back, done
   ├── ❄️ 429/502/503 → key enters cooldown, retry with next key
   ├── 🔑 401/403 → key permanently removed from pool
   └── ⚠️ other 4xx/5xx → passed through as-is
```

Your agent sees a clean stream or a final error. Never a 429.

---

## Key Status

```bash
curl http://localhost:3000/health
```

```json
{
  "status": "ok",
  "keys": 3,
  "details": [
    {
      "index": 0,
      "key": "nvapi-xxxxxxxxxxxx",
      "status": "ready",
      "requests_per_minute": 15,
      "last_used": "2023-11-15T14:30:00Z",
      "cooldown_until": "2023-11-15T14:29:00Z"
    },
    {
      "index": 1,
      "key": "nvapi-yyyyyyyyyyyy",
      "status": "cooling(42s)",
      "requests_per_minute": 40,
      "last_used": "2023-11-15T14:31:00Z",
      "cooldown_until": "2023-11-15T14:32:00Z"
    }
  ]
}
```

---

## Claude Code (experimental)

Alvus can also masquerade as the **Anthropic API**, letting [Claude Code](https://claude.com/claude-code) run against any OpenAI-compatible backend (NVIDIA NIM, OpenRouter, Groq, …). Alvus translates the Anthropic Messages API to OpenAI Chat Completions in both directions — request, response, and streaming.

Point Claude Code at Alvus with three environment variables:

```bash
export ANTHROPIC_BASE_URL=http://localhost:3000
export ANTHROPIC_AUTH_TOKEN=sk-dummy          # ignored — Alvus injects a pooled key
export ANTHROPIC_MODEL=deepseek-ai/deepseek-r1 # the upstream model to use

claude
```

Alternatively, set `OVERRIDE_MODEL` in Alvus's `.env` to force a model regardless of what the client requests:

```env
OVERRIDE_MODEL=deepseek-ai/deepseek-r1
```

**Translation covers:** system prompts, multi-turn messages, tool definitions and tool calls (`tool_use` ⇄ `tool_calls`), images, streaming SSE, and `stop_reason` mapping. The zero-dependency, pure-stdlib promise holds — it's all `encoding/json`.

> ⚠️ **Expectation check.** Claude Code is tuned hard for Claude models. Driving it on other models through a translation shim works, but tool-use reliability and edit formatting can be rough depending on the backend. Treat this as "it runs," not "it replaces your Claude subscription."

---

## Other Providers

`TARGET_BASE_URL` is all you need to change:

```env
# OpenRouter
TARGET_BASE_URL=https://openrouter.ai/api/v1

# Together AI
TARGET_BASE_URL=https://api.together.xyz/v1

# Groq
TARGET_BASE_URL=https://api.groq.com/openai/v1

# Any other OpenAI-compatible endpoint
TARGET_BASE_URL=https://your-provider.com/v1
```

---

## Running as a Service (systemd)

```ini
[Unit]
Description=Alvus
After=network.target

[Service]
ExecStart=/usr/local/bin/alvus
WorkingDirectory=/etc/alvus
Restart=on-failure
RestartSec=5
# Graceful shutdown on stop/restart
KillSignal=SIGTERM
TimeoutStopSec=10

[Install]
WantedBy=multi-user.target
```

Put your `.env` in `/etc/alvus/`. Reload and start:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now alvus
```

Alvus handles `SIGINT` and `SIGTERM` gracefully, allowing in-flight requests to complete before shutting down (with a 5-second timeout).

---

## FAQ

**Do I need Go installed to run this?**
No. Download a prebuilt binary from [Releases](../../releases).

**Are my keys safe?**
Keys live in `.env` on your machine and are only ever sent to the upstream provider. Alvus logs key indices, never key values.

**What if ALL keys are cooling?**
Alvus waits for the soonest key to become available and retries, up to 10 times. If everything stays exhausted, it returns `503`. In practice, with 3 keys and a 60s window this is very hard to trigger.

**Can I reload keys without restarting?**
Yes! Alvus now supports hot-reloading when the `.env` file changes. Simply edit your `.env` file and Alvus will automatically detect the changes and reload the configuration within 1 second. No restart needed.

**Does it work on a Raspberry Pi Zero / 32-bit hardware?**
Yes. Prebuilt binaries include `linux/arm` and `linux/386`. The binary is fully static — no runtime needed.

**How much memory does it use?**
Around 2 MB at idle. It's a single static Go binary with no runtime overhead — you won't notice it sitting next to a running model.

---

## Roadmap

- [x] Hot-reload when .env changes (no restart needed)
- [x] Per-key request counters and detailed status in `/health`
- [x] Web dashboard (opt-in, zero-dep binary stays the same)

---

## Contributing

PRs welcome. This project is **pure Go stdlib with zero external dependencies** — keep it that way. The core proxy lives in `main.go`; the Claude Code translation layer is isolated in `anthropic.go`. If a feature needs an import beyond stdlib, it doesn't belong here. Open an issue first and we'll figure out the right shape for it.

---

## License

MIT.

---

_Built at 2am when an OpenClaw task hit its fifth 429 in a row._
