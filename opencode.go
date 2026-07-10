package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type OpencodeClient struct {
	baseURL        string
	modelProvider  string
	modelID        string
	httpClient     *http.Client
}

type OpenCodeSendResponse struct {
	Parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"parts"`
	Role string `json:"role"`
}

type OpenCodeSessionResponse struct {
	SessionID string `json:"id"`
	Messages  []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func NewOpencodeClient(baseURL, provider, modelID string, timeout time.Duration) *OpencodeClient {
	return &OpencodeClient{
		baseURL:       baseURL,
		modelProvider: provider,
		modelID:       modelID,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *OpencodeClient) CreateSession(ctx context.Context) (string, error) {
	body := map[string]interface{}{
		"modelProvider": c.modelProvider,
		"modelId":       c.modelID,
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/session", bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	defer resp.Body.Close()

	var sr struct {
		SessionID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return "", fmt.Errorf("create session parse: %w", err)
	}
	if sr.SessionID == "" {
		return "", fmt.Errorf("create session: empty id")
	}
	return sr.SessionID, nil
}

func (c *OpencodeClient) SendMessage(ctx context.Context, sid, msg string) (*OpenCodeSendResponse, error) {
	bodyMap := map[string]interface{}{
		"parts": []map[string]string{
			{"type": "text", "text": msg},
		},
	}
	b, _ := json.Marshal(bodyMap)
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/session/"+sid+"/message", bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var reply OpenCodeSendResponse
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, fmt.Errorf("send message parse: %w", err)
	}
	return &reply, nil
}

func (c *OpencodeClient) GetSession(ctx context.Context, sid string) (*OpenCodeSessionResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/session/"+sid, nil)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	defer resp.Body.Close()

	var sr OpenCodeSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("get session parse: %w", err)
	}
	sr.SessionID = sid
	return &sr, nil
}

func (c *OpencodeClient) GetSessionMessages(ctx context.Context, sid string) ([]struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/session/"+sid+"/message", nil)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var msgs []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return nil, fmt.Errorf("get messages parse: %w", err)
	}
	return msgs, nil
}
