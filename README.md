# opencode-feishu-channel

<p align="right">
  <a href="README.zh.md">中文</a>
</p>

Bridge between [opencode](https://opencode.ai) AI and [Feishu](https://www.feishu.cn) (Lark) instant messaging.

Receives Feishu group chat / direct messages via WebSocket, forwards them to opencode for AI processing, and sends replies back to Feishu.

## Features

- WebSocket persistent connection to Feishu (official SDK)
- Multi-channel support — different Feishu groups can map to different opencode contexts
- Graceful restart — Nginx-style `SIGUSR1` / `SIGTERM` two-phase shutdown
- Crash recovery — pending delivery persistence with polling retry
- Image OCR — auto-OCR images via tesseract before sending to AI
- Session persistence — conversation sessions survive restarts
- Low resource usage — ~8MB RSS

## Image OCR

Simply **send an image** in any connected Feishu group chat — no special command needed. The service will:

1. Download the image via Feishu API
2. Run `tesseract` for Chinese + English OCR
3. Forward the recognized text to the AI for processing

Sample log output:
```
MSG openId=xxx type=group: (recognized text from image)
```

> Requires `tesseract-ocr` with `chi_sim` and `eng` language packs installed.
> Supported format: PNG (Feishu default).

## Feishu Commands

Type these in any Feishu group chat connected to the bot:

| Command | Description |
|---------|-------------|
| `/session` or `/status` | Show current session ID |
| `/sessions` or `/list` | List all active sessions on the server |
| `/stop` or `/cancel` | Cancel the current in-flight AI request |
| `/clear` or `/new` or `/reset` | Clear current session and start fresh |
| `/help` | Show command help |

## Prerequisites

- Go 1.26+
- A running [opencode](https://opencode.ai) server with API accessible
- A [Feishu app](https://open.feishu.cn/app) with:
  - `im:message` permission
  - `im:resource` permission (for image download)
  - Event subscription: `im.message.receive_v1`
- tesseract-ocr (optional, for image OCR)

## Quick Start

```bash
# Build
git clone <your-repo> opencode-feishu-channel
cd opencode-feishu-channel
go build -o opencode-feishu-channel .

# Create config
cp config.example.json config.json
# Edit config.json with your Feishu app credentials

# Run
./opencode-feishu-channel --config config.json
```

## Configuration

| Section | Field | Description |
|---------|-------|-------------|
| `feishu` | `appId` | Feishu app ID |
| `feishu` | `appSecret` | Feishu app secret |
| `opencode` | `url` | opencode server URL |
| `opencode` | `modelProvider` | Provider ID (e.g. `opencode`) |
| `opencode` | `modelId` | Model ID (e.g. `deepseek-v4-flash-free`) |
| `paths` | `sessionDb` | Path to session persistence file |
| `paths` | `logFile` | Path to log file |
| `paths` | `pendingDb` | Path to pending delivery persistence |
| `bridge` | `requestTimeoutMs` | AI request timeout (default: 3600000) |
| `bridge` | `messageExpiryMs` | Message dedup expiry (default: 300000) |
| `channels` | `<chat_id>` | Map of Feishu chat IDs to channel configs |

### Channels

The `channels` section maps Feishu group chat IDs to named channels:

```json
"channels": {
  "oc_xxxxxxxxxx": {
    "name": "日常助手",
    "project": "/path/to/workspace"
  }
}
```

Each channel has a `name` (used as AI context hint) and a `project` path (for workspace-specific operations).

To find a group's chat ID: send a message in the group, then check the `chat_id` field in the event payload.

## Signal Handling

| Signal | Behavior |
|--------|----------|
| `SIGUSR1` | Graceful drain — stop accepting new events, wait for all in-flight AI requests to complete (no timeout), then exit |
| `SIGTERM` | Graceful shutdown — same as SIGUSR1 but with a configurable timeout (`requestTimeoutMs`) |
| `SIGINT` | Same as SIGTERM |
| SIGKILL / crash | On restart, pending delivery file is checked and uncompleted replies are retried via polling |

Designed for Nginx-style two-phase restart: send SIGUSR1 to old process, wait for it to exit, then start new process.

## Systemd Service

```ini
[Unit]
Description=Opencode Feishu Channel
After=network-online.target opencode-server.service
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/opencode-feishu-channel --config /etc/opencode-feishu-channel/config.json
Restart=always
RestartSec=5
KillMode=process
TimeoutStopSec=4000

[Install]
WantedBy=default.target
```

## In-Flight Request Safety

When the service receives SIGUSR1 or SIGTERM:

1. It stops accepting new Feishu events
2. For each in-flight AI request, it waits for the HTTP response
3. The reply is sent to Feishu before the process exits
4. If the process crashes or is SIGKILLed, the pending delivery file persists the uncompleted request IDs
5. On next startup, the service polls opencode for the last assistant message and forwards it

## License

MIT
