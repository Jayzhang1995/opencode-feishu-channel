package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type sessionInfo struct {
	SessionID string `json:"sessionId"`
	Key       string `json:"key"`
	CreatedAt int64  `json:"createdAt"`
}

type sessionDB struct {
	Sessions []sessionInfo `json:"sessions"`
}

type SessionManager struct {
	path     string
	opencode *OpencodeClient
	mu       sync.Mutex
	sessions map[string]*sessionInfo
	contexts map[string]string
}

func NewSessionManager(path string, opencode *OpencodeClient) *SessionManager {
	sm := &SessionManager{
		path:     path,
		opencode: opencode,
		sessions: make(map[string]*sessionInfo),
		contexts: make(map[string]string),
	}
	sm.load()
	return sm
}

func (sm *SessionManager) load() {
	data, err := os.ReadFile(sm.path)
	if err != nil {
		return
	}
	var db sessionDB
	if err := json.Unmarshal(data, &db); err != nil {
		logf("Failed to parse sessions: %v", err)
		return
	}
	for _, s := range db.Sessions {
		sm.sessions[s.Key] = &sessionInfo{
			SessionID: s.SessionID,
			Key:       s.Key,
			CreatedAt: s.CreatedAt,
		}
	}
	logf("Loaded %d sessions from disk", len(db.Sessions))
}

func (sm *SessionManager) save() {
	db := sessionDB{}
	for _, s := range sm.sessions {
		db.Sessions = append(db.Sessions, *s)
	}
	data, err := json.Marshal(db)
	if err != nil {
		logf("Failed to marshal sessions: %v", err)
		return
	}
	if err := os.WriteFile(sm.path, data, 0644); err != nil {
		logf("Failed to save sessions: %v", err)
	}
}

func (sm *SessionManager) loadContext(key string) string {
	if ctx, ok := sm.contexts[key]; ok {
		return ctx
	}
	return ""
}

func (sm *SessionManager) GetPendingContext(key string) string {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.loadContext(key)
}

func (sm *SessionManager) Get(key string) *sessionInfo {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.sessions[key]
}

func (sm *SessionManager) GetAll() []sessionInfo {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	var result []sessionInfo
	for _, s := range sm.sessions {
		result = append(result, *s)
	}
	return result
}

func (sm *SessionManager) Remove(key string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.sessions, key)
	delete(sm.contexts, key)
	sm.save()
}

func (sm *SessionManager) GetOrCreate(key, contextHint string) (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if s, ok := sm.sessions[key]; ok {
		return s.SessionID, nil
	}

	ctx := context.Background()
	customCtx := ""
	if contextHint != "" {
		customCtx = fmt.Sprintf(`{"context":"你是一个%s助手，今天的日期是%s。","additionalContext":"%s"}`,
			contextHint, time.Now().UTC().Add(8*time.Hour).Format("2006-01-02"), contextHint)
	}

	if customCtx != "" {
		sm.contexts[key] = customCtx
	}

	sid, err := sm.opencode.CreateSession(ctx)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	sm.sessions[key] = &sessionInfo{
		SessionID: sid,
		Key:       key,
		CreatedAt: time.Now().UnixMilli(),
	}
	sm.save()

	if strings.HasPrefix(customCtx, "{") {
		sm.opencode.SendMessage(ctx, sid, customCtx)
	}

	return sid, nil
}
