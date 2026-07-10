package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type FeishuClient struct {
	client    *lark.Client
	appID     string
	appSecret string
}

func NewFeishuClient(client *lark.Client, appID, appSecret string) *FeishuClient {
	return &FeishuClient{
		client:    client,
		appID:     appID,
		appSecret: appSecret,
	}
}

func makePostContent(text string) string {
	if len(text) > 2000 {
		text = text[:2000]
	}
	content := map[string]interface{}{
		"zh_cn": map[string]interface{}{
			"title": "",
			"content": [][]map[string]interface{}{{
				{"tag": "md", "text": text},
			}},
		},
	}
	b, _ := json.Marshal(content)
	return string(b)
}

func (c *FeishuClient) SendMessage(ctx context.Context, chatID, openID, chatType, text string) error {
	content := makePostContent(text)
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType("chat_id").
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType("post").
			Content(content).
			Build()).
		Build()

	resp, err := c.client.Im.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}
	if resp.Code != 0 {
		return fmt.Errorf("send: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func (c *FeishuClient) ReplyToMessage(ctx context.Context, msgID, text string) error {
	content := makePostContent(text)
	req := larkim.NewReplyMessageReqBuilder().
		MessageId(msgID).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType("post").
			Content(content).
			Build()).
		Build()

	resp, err := c.client.Im.Message.Reply(ctx, req)
	if err != nil {
		return fmt.Errorf("reply: %w", err)
	}
	if resp.Code != 0 {
		return fmt.Errorf("reply: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func (c *FeishuClient) DownloadImage(ctx context.Context, msgID, imageKey string) ([]byte, error) {
	token, err := c.getTenantToken(ctx)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("https://open.feishu.cn/open-apis/im/v1/messages/%s/resources/%s?type=image", msgID, imageKey)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (c *FeishuClient) getTenantToken(ctx context.Context) (string, error) {
	body := fmt.Sprintf(`{"app_id":"%s","app_secret":"%s"}`, c.appID, c.appSecret)
	resp, err := http.Post("https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal",
		"application/json", io.NopCloser(strings.NewReader(body)))
	if err != nil {
		return "", fmt.Errorf("token: %w", err)
	}
	defer resp.Body.Close()
	var tr struct {
		Token string `json:"tenant_access_token"`
		Code  int    `json:"code"`
		Msg   string `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", fmt.Errorf("token parse: %w", err)
	}
	if tr.Code != 0 {
		return "", fmt.Errorf("token: code=%d msg=%s", tr.Code, tr.Msg)
	}
	return tr.Token, nil
}
