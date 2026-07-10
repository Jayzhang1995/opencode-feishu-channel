package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type InFlightEntry struct {
	Cancel   context.CancelFunc
	MsgID    string
	ChatID   string
	OpenID   string
	ChatType string
	Key      string
	Done     chan struct{}
}

type Handler struct {
	feishu       *FeishuClient
	opencode     *OpencodeClient
	sessions     *SessionManager
	config       *Config
	inFlight     map[string]*InFlightEntry
	inFlightMu   sync.Mutex
	shuttingDown bool
	processed    map[string]bool
	processedMu  sync.Mutex
	replied      map[string]bool
	repliedMu    sync.Mutex
}

func NewHandler(feishu *FeishuClient, opencode *OpencodeClient, sessions *SessionManager, config *Config) *Handler {
	h := &Handler{
		feishu:    feishu,
		opencode:  opencode,
		sessions:  sessions,
		config:    config,
		inFlight:  make(map[string]*InFlightEntry),
		processed: make(map[string]bool),
		replied:   make(map[string]bool),
	}

	cleanupMs := config.Bridge.ProcessedCleanupIntervalMs
	if cleanupMs <= 0 {
		cleanupMs = 60000
	}

	go func() {
		ticker := time.NewTicker(time.Duration(cleanupMs) * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			h.processedMu.Lock()
			for id := range h.processed {
				if !strings.HasPrefix(id, "om_") {
					delete(h.processed, id)
				}
			}
			h.processedMu.Unlock()
		}
	}()

	go func() {
		ticker := time.NewTicker(time.Duration(config.Bridge.RepliedCleanupIntervalMs) * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			h.repliedMu.Lock()
			h.replied = make(map[string]bool)
			h.repliedMu.Unlock()
		}
	}()

	return h
}

func (h *Handler) HandleEvent(ctx context.Context, event *larkim.P2MessageReceiveV1) {
	if h.shuttingDown || event == nil || event.Event == nil {
		return
	}

	e := event.Event
	msg := e.Message
	sender := e.Sender
	if msg == nil || sender == nil {
		return
	}

	msgID := safeStr(msg.MessageId)
	if msgID == "" {
		return
	}

	h.processedMu.Lock()
	if h.processed[msgID] {
		h.processedMu.Unlock()
		return
	}
	h.processed[msgID] = true
	h.processedMu.Unlock()

	msgType := safeStr(msg.MessageType)
	openID := ""
	if sender.SenderId != nil {
		openID = safeStr(sender.SenderId.OpenId)
	}
	chatID := safeStr(msg.ChatId)
	chatType := safeStr(msg.ChatType)
	senderType := safeStr(sender.SenderType)

	if senderType == "app" {
		return
	}

	key := chatID + ":" + openID
	text := parseMessageContent(msgType, safeStr(msg.Content))

	if msgType == "image" {
		var imgContent struct {
			ImageKey string `json:"image_key"`
		}
		json.Unmarshal([]byte(safeStr(msg.Content)), &imgContent)
		if imgContent.ImageKey != "" {
			buf, err := h.feishu.DownloadImage(ctx, msgID, imgContent.ImageKey)
			if err == nil {
				text = ocrImage(buf)
				logf("OCR: %s", truncate(text, 80))
			} else {
				text = "(图片处理失败: " + err.Error() + ")"
			}
		}
	}

	if text == "" && msgType != "image" {
		return
	}

	logf("MSG openId=%s chatId=%s type=%s: %s", openID, truncate(chatID, 20), chatType, truncate(text, 100))

	sendOnce := func(t string) {
		h.repliedMu.Lock()
		if h.replied[msgID] {
			h.repliedMu.Unlock()
			return
		}
		h.replied[msgID] = true
		h.repliedMu.Unlock()
		if err := h.feishu.ReplyToMessage(ctx, msgID, t); err != nil {
			logf("Reply failed: %v", err)
		} else {
			logf("  -> sent")
		}
	}

	if strings.HasPrefix(text, "/") {
		parts := strings.Fields(text[1:])
		if len(parts) > 0 {
			reply := h.handleCommand(parts[0], parts[1:], key, chatType)
			if reply != "" {
				logf("CMD /%s -> %s", parts[0], truncate(reply, 60))
				sendOnce(reply)
				return
			}
		}
	}

	go func() {
		h.feishu.ReplyToMessage(ctx, msgID, "⏳ 正在处理，请稍候...")
	}()

	channelName := ""
	if ch, ok := h.config.Channels[chatID]; ok {
		channelName = ch.Name
	}
	nameMap := map[string]string{"日常助手": "日常", "理财助手": "理财", "编程专家": "编程"}
	shortName := channelName
	if n, ok := nameMap[channelName]; ok {
		shortName = n
	}
	today := time.Now().UTC().Add(8 * time.Hour).Format("2006-01-02")

	sid, err := h.sessions.GetOrCreate(key, shortName+" "+today)
	if err != nil {
		logf("Session error: %v", err)
		return
	}
	logf("  session %s ready for key=%s", sid, key)

	userText := text
	if ctx := h.sessions.GetPendingContext(key); ctx != "" {
		userText = ctx + "\n\n---\n\n" + text
		logf("  prepended context (%d chars) to user message", len(ctx))
	}

	addPendingDelivery(PendingDelivery{
		MsgID:     msgID,
		ChatID:    chatID,
		OpenID:    openID,
		ChatType:  chatType,
		SID:       sid,
		CreatedAt: time.Now().UnixMilli(),
	})

	ctxCancel, cancel := context.WithCancel(ctx)
	entry := &InFlightEntry{
		Cancel:   cancel,
		MsgID:    msgID,
		ChatID:   chatID,
		OpenID:   openID,
		ChatType: chatType,
		Key:      key,
		Done:     make(chan struct{}),
	}

	h.inFlightMu.Lock()
	h.inFlight[key] = entry
	h.inFlightMu.Unlock()

	go func() {
		defer close(entry.Done)
		defer func() {
			h.inFlightMu.Lock()
			if h.inFlight[key] == entry {
				delete(h.inFlight, key)
			}
			h.inFlightMu.Unlock()
		}()

		reply, err := h.opencode.SendMessage(ctxCancel, sid, userText)
		if err != nil {
			if strings.Contains(err.Error(), "canceled") || strings.Contains(err.Error(), "deadline") {
				return
			}
			logf("AI error for key=%s: %v", key, err)
			removePendingDelivery(msgID)
			h.sessions.Remove(key)
			h.feishu.SendMessage(ctx, chatID, openID, chatType, "("+err.Error()+")")
			return
		}

		var texts []string
		for _, p := range reply.Parts {
			if p.Type == "text" {
				texts = append(texts, p.Text)
			}
		}
		if len(texts) == 0 {
			texts = []string{"(无回复)"}
		}
		result := strings.Join(texts, "\n")

		logf("REPLY to key=%s (%d chars)", key, len(result))
		removePendingDelivery(msgID)
		h.feishu.SendMessage(ctx, chatID, openID, chatType, result)
	}()
}

func (h *Handler) handleCommand(cmd string, args []string, key, chatType string) string {
	switch cmd {
	case "sessions", "list":
		all := h.sessions.GetAll()
		currentSid := ""
		if s := h.sessions.Get(key); s != nil {
			currentSid = s.SessionID
		}
		if len(all) == 0 {
			return "服务器上没有会话记录。"
		}
		var lines []string
		lines = append(lines, fmt.Sprintf("📋 服务器上共 %d 个会话：", len(all)), "")
		for _, s := range all {
			isCurrent := ""
			if s.SessionID == currentSid {
				isCurrent = " ← 你"
			}
			lines = append(lines, fmt.Sprintf("`%s`%s", s.SessionID, isCurrent))
		}
		return strings.Join(lines, "\n")

	case "session", "status":
		s := h.sessions.Get(key)
		if s == nil {
			return "你还没有创建会话。发条消息即可自动创建。"
		}
		return fmt.Sprintf("会话ID: `%s`", s.SessionID)

	case "clear", "new", "reset":
		s := h.sessions.Get(key)
		if s != nil {
			h.sessions.Remove(key)
			return "会话已清除。下一条消息将开始新对话。"
		}
		return "没有可清除的会话。"

	case "stop", "cancel":
		h.inFlightMu.Lock()
		entry := h.inFlight[key]
		h.inFlightMu.Unlock()
		if entry == nil {
			return "当前没有正在执行的任务。"
		}
		entry.Cancel()
		logf("STOP: cancelled request for %s", key)
		return "✅任务已停止。"

	case "help":
		return "支持的命令：\n• /session — 查看当前会话\n• /sessions — 列出所有会话\n• /stop — 停止当前任务\n• /clear — 清除当前会话\n• /help — 显示此帮助"

	default:
		return ""
	}
}

func (h *Handler) WaitForInFlight() {
	h.inFlightMu.Lock()
	entries := make([]*InFlightEntry, 0, len(h.inFlight))
	for _, e := range h.inFlight {
		entries = append(entries, e)
	}
	h.inFlightMu.Unlock()

	if len(entries) == 0 {
		return
	}

	logf("Waiting for %d in-flight request(s)...", len(entries))
	for _, e := range entries {
		<-e.Done
	}
	logf("All in-flight completed")
}

func (h *Handler) Shutdown() {
	h.shuttingDown = true
}

func parseMessageContent(msgType, content string) string {
	if msgType == "text" {
		var pc struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(content), &pc); err == nil {
			return strings.TrimSpace(pc.Text)
		}
		return strings.TrimSpace(content)
	}
	if msgType == "post" {
		var post struct {
			ZhCN json.RawMessage `json:"zh_cn"`
		}
		if err := json.Unmarshal([]byte(content), &post); err != nil {
			return content
		}
		raw := post.ZhCN
		if raw == nil {
			raw = json.RawMessage(content)
		}
		var parsed struct {
			Content [][]struct {
				Tag  string `json:"tag"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return content
		}
		var text string
		for _, p := range parsed.Content {
			for _, seg := range p {
				if seg.Tag == "text" || seg.Tag == "md" {
					text += seg.Text
				}
			}
		}
		return text
	}
	return content
}

func ocrImage(buf []byte) string {
	tmpFile := fmt.Sprintf("/tmp/feishu-img-%d.png", time.Now().UnixNano())
	if err := os.WriteFile(tmpFile, buf, 0644); err != nil {
		return "(OCR failed: write)"
	}
	defer os.Remove(tmpFile)

	cmd := exec.Command("tesseract", tmpFile, "stdout", "-l", "chi_sim+eng")
	out, err := cmd.Output()
	if err != nil {
		return "(OCR failed)"
	}
	return strings.TrimSpace(string(out))
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
