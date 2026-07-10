# opencode-feishu-channel

[opencode](https://opencode.ai) AI 与[飞书](https://www.feishu.cn)即时通讯之间的桥梁。

通过 WebSocket 接收飞书群聊/私聊消息，转发至 opencode 进行 AI 处理，并将回复发回飞书。

## 特性

- WebSocket 长连接（官方 SDK）
- 多频道支持 — 不同飞书群可映射到不同的 opencode 上下文
- 优雅重启 — Nginx 风格 `SIGUSR1` / `SIGTERM` 两阶段关闭
- 崩溃恢复 — 待投递持久化 + 轮询重试
- 图片 OCR — 自动通过 tesseract 识别图片文字后再发送给 AI
- 会话持久化 — 会话跨重启保持
- 极低资源占用 — 约 8MB RSS

## 飞书群命令

在接入的飞书群中直接发送以下指令：

| 命令 | 说明 |
|------|------|
| `/session` 或 `/status` | 查看当前会话 ID |
| `/sessions` 或 `/list` | 列出服务器上所有活跃会话 |
| `/stop` 或 `/cancel` | 取消当前正在进行的 AI 请求 |
| `/clear` 或 `/new` 或 `/reset` | 清除当前会话，开始新对话 |
| `/help` | 显示命令帮助 |

## 前置依赖

- Go 1.26+
- 运行中的 [opencode](https://opencode.ai) 服务（API 可访问）
- [飞书应用](https://open.feishu.cn/app) 需开通权限：
  - `im:message`
  - `im:resource`（用于图片下载）
  - 事件订阅：`im.message.receive_v1`
- tesseract-ocr（可选，用于图片 OCR）

## 快速开始

```bash
# 编译
git clone <your-repo> opencode-feishu-channel
cd opencode-feishu-channel
go build -o opencode-feishu-channel .

# 创建配置
cp config.example.json config.json
# 编辑 config.json 填入飞书应用凭据

# 运行
./opencode-feishu-channel --config config.json
```

## 配置说明

| 字段 | 说明 |
|------|------|
| `feishu.appId` | 飞书应用 App ID |
| `feishu.appSecret` | 飞书应用 App Secret |
| `opencode.url` | opencode 服务地址 |
| `opencode.modelProvider` | 模型供应商（如 `opencode`） |
| `opencode.modelId` | 模型 ID（如 `deepseek-v4-flash-free`） |
| `paths.sessionDb` | 会话持久化文件路径 |
| `paths.logFile` | 日志文件路径 |
| `paths.pendingDb` | 待投递消息持久化路径 |
| `bridge.requestTimeoutMs` | AI 请求超时（默认 3600000） |
| `bridge.messageExpiryMs` | 消息去重过期时间（默认 300000） |

### 频道配置

`channels` 段将飞书群聊 Chat ID 映射为命名频道：

```json
"channels": {
  "oc_xxxxxxxxxx": {
    "name": "日常助手",
    "project": "/path/to/workspace"
  }
}
```

`name` 用作 AI 上下文提示，`project` 为工作区路径。Chat ID 可在群内发消息后从事件 payload 的 `chat_id` 字段获取。

## 信号处理

| 信号 | 行为 |
|------|------|
| `SIGUSR1` | 优雅排空 — 停止接收新事件，等待所有进行中的 AI 请求完成（无超时），然后退出 |
| `SIGTERM` | 优雅关闭 — 同 SIGUSR1，但受 `requestTimeoutMs` 超时限制 |
| `SIGINT` | 同 SIGTERM |
| SIGKILL / 崩溃 | 重启时检查待投递文件，通过轮询 opencode 获取未完成的回复并转发 |

设计用于 Nginx 式两阶段重启：向旧进程发送 SIGUSR1，等待其退出，再启动新进程。

## Systemd 服务示例

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

## 进行中请求的安全保障

服务收到 SIGUSR1 或 SIGTERM 时：

1. 停止接收新飞书事件
2. 对每个进行中的 AI 请求等待 HTTP 响应
3. 回复发回飞书后进程才退出
4. 如果进程崩溃或被 SIGKILL，待投递文件持久化未完成的请求 ID
5. 下次启动时，服务轮询 opencode 获取最后一条 assistant 消息并转发

## 许可证

MIT
