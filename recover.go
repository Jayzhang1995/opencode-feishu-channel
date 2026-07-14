package main

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"
)

var (
	pendingMu    sync.Mutex
	pendingDBPath string
)

func setPendingDBPath(path string) {
	pendingDBPath = path
}

func loadPendingDeliveries() []PendingDelivery {
	pendingMu.Lock()
	defer pendingMu.Unlock()

	data, err := os.ReadFile(pendingDBPath)
	if err != nil {
		return nil
	}
	var entries []PendingDelivery
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil
	}
	return entries
}

func savePendingDeliveries(entries []PendingDelivery) {
	pendingMu.Lock()
	defer pendingMu.Unlock()

	data, err := json.Marshal(entries)
	if err != nil {
		return
	}
	os.WriteFile(pendingDBPath, data, 0644)
}

func addPendingDelivery(entry PendingDelivery) {
	entries := loadPendingDeliveries()
	entries = append(entries, entry)
	savePendingDeliveries(entries)
}

func removePendingDelivery(msgID string) {
	entries := loadPendingDeliveries()
	var kept []PendingDelivery
	for _, e := range entries {
		if e.MsgID != msgID {
			kept = append(kept, e)
		}
	}
	savePendingDeliveries(kept)
}

func recoverPendingDeliveries(feishu *FeishuClient, opencode *OpencodeClient, cfg *Config) {
	entries := loadPendingDeliveries()
	if len(entries) == 0 {
		return
	}
	logf("Recovery: %d pending deliveries found, starting recovery...", len(entries))

	ctx := context.Background()
	for _, entry := range entries {
		done, err := processPendingEntry(ctx, feishu, opencode, &entry)
		if err != nil {
			logf("Recovery: error for msgId=%s: %v", entry.MsgID, err)
			continue
		}
		if done {
			removePendingDelivery(entry.MsgID)
			logf("Recovery: resolved msgId=%s", entry.MsgID)
		}
	}

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			remaining := loadPendingDeliveries()
			if len(remaining) == 0 {
				return
			}
			logf("Recovery: %d remaining, retrying...", len(remaining))
			for _, entry := range remaining {
				done, err := processPendingEntry(ctx, feishu, opencode, &entry)
				if err != nil {
					logf("Recovery: error for msgId=%s: %v", entry.MsgID, err)
					continue
				}
				if done {
					removePendingDelivery(entry.MsgID)
				}
			}
		}
	}()
}

func processPendingEntry(ctx context.Context, feishu *FeishuClient, opencode *OpencodeClient, entry *PendingDelivery) (bool, error) {
	msgs, err := opencode.GetSessionMessages(ctx, entry.SID)
	if err != nil {
		return false, err
	}

	var lastAssistant string
	for _, m := range msgs {
		if m.Role == "assistant" {
			lastAssistant = m.Content
		}
	}
	if lastAssistant == "" {
		return false, nil
	}

	if err := feishu.SendMessage(ctx, entry.ChatID, entry.OpenID, entry.ChatType, lastAssistant); err != nil {
		return false, err
	}
	return true, nil
}
