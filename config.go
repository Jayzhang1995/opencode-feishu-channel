package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Feishu   FeishuConfig   `json:"feishu"`
	Opencode OpenCodeConfig `json:"opencode"`
	Paths    PathsConfig    `json:"paths"`
	Bridge   BridgeConfig   `json:"bridge"`
	Channels map[string]ChannelConfig `json:"channels"`
}

type FeishuConfig struct {
	AppID     string `json:"appId"`
	AppSecret string `json:"appSecret"`
}

type OpenCodeConfig struct {
	URL            string `json:"url"`
	ModelProvider  string `json:"modelProvider"`
	ModelID        string `json:"modelId"`
}

type PathsConfig struct {
	SessionDb      string `json:"sessionDb"`
	OpencodeDb     string `json:"opencodeDb"`
	LogFile        string `json:"logFile"`
	PendingDb      string `json:"pendingDb"`
}

type BridgeConfig struct {
	RequestTimeoutMs            int `json:"requestTimeoutMs"`
	MessageExpiryMs             int `json:"messageExpiryMs"`
	ProcessedCleanupIntervalMs  int `json:"processedCleanupIntervalMs"`
	RepliedCleanupIntervalMs    int `json:"repliedCleanupIntervalMs"`
}

type ChannelConfig struct {
	Name    string `json:"name"`
	Project string `json:"project"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	cfg := &Config{
		Bridge: BridgeConfig{
			RequestTimeoutMs:           3600000,
			MessageExpiryMs:            300000,
			ProcessedCleanupIntervalMs: 60000,
			RepliedCleanupIntervalMs:   600000,
		},
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return cfg, nil
}
