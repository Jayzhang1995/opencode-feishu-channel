# opencode-feishu-channel — 项目知识

## 项目概要

opencode AI ↔ 飞书即时通讯桥梁。Go 编写，官方飞书 SDK + WebSocket 长连接。

## 飞书群命令

| 命令 | 说明 |
|------|------|
| `/session` / `/status` | 查看当前会话 |
| `/sessions` / `/list` | 列出所有会话 |
| `/stop` / `/cancel` | 取消正在执行的 AI 请求 |
| `/clear` / `/new` / `/reset` | 清除当前会话 |
| `/help` | 显示帮助 |

## 关键文件

| 文件 | 用途 |
|------|------|
| `main.go` | 入口，信号处理 (SIGUSR1/SIGTERM) |
| `handler.go` | 飞书事件处理、命令分发、in-flight 追踪 |
| `feishu.go` | 飞书 REST 客户端（发送/回复消息、下载图片） |
| `opencode.go` | opencode HTTP 客户端（会话管理、消息发送） |
| `session.go` | 会话管理器（磁盘持久化） |
| `recover.go` | 崩溃恢复（待投递持久化 + 5s 轮询重试） |
| `config.go` | JSON 配置加载 |
| `config.example.json` | 示例配置 |

## 构建与部署

```bash
go build -o opencode-feishu-channel .
sudo cp opencode-feishu-channel /usr/local/bin/
sudo cp config.json /etc/opencode-feishu-channel/
```

## 运行方式

```bash
# 前台运行
./opencode-feishu-channel --config /etc/opencode-feishu-channel/config.json

# systemd
systemctl --user start opencode-opencode-feishu-channel
systemctl --user stop opencode-opencode-feishu-channel
systemctl --user restart opencode-opencode-feishu-channel
systemctl --user status opencode-opencode-feishu-channel
```

## 日志

```bash
tail -f /var/log/opencode-opencode-feishu-channel.log
# 或 systemd journal
journalctl --user -u opencode-opencode-feishu-channel -n 50 --no-pager
```

## 信号处理

- `SIGUSR1` — 优雅排空（无超时，等待所有 in-flight 完成）
- `SIGTERM` — 优雅关闭（`requestTimeoutMs` 超时限制）
- SIGKILL / crash — 重启时通过 `feishu-pending.json` 恢复

## MCP 管理工具

`/root/.opencode/mcp-opencode-feishu-channel.js` 提供 4 个操作：
- `feishu_channel_status` — 查看状态
- `feishu_channel_restart` — 两阶段重启（SIGUSR1 → 等待 → systemctl start）
- `feishu_channel_logs` — 查看日志
- `feishu_channel_config` — 查看配置摘要

## 频道配置

`/etc/opencode-opencode-feishu-channel/config.json` 中 `channels` 段：
- key: 飞书群 Chat ID
- value: `{ name, project }`

## 性能指标

- 内存: ~8MB RSS
- 二进制: ~11MB (静态编译)
- 并发: goroutine-per-request，受 `requestTimeoutMs` 保护

## 关键架构决策

- 两阶段重启而非热重载：SIGUSR1 排空旧进程 → 启动新进程
- 使用官方 Go SDK `ws.NewClient()` 而非自建 WebSocket
- 待投递持久化到 JSON 文件（非 DB），崩溃后通过 polling 恢复
- send 用 POST 消息（`im/v1/messages`），reply 用回消息（`im/v1/messages/{id}/reply`）
