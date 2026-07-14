package main

type PendingDelivery struct {
	MsgID     string `json:"msgId"`
	ChatID    string `json:"chatId"`
	OpenID    string `json:"openId"`
	ChatType  string `json:"chatType"`
	SID       string `json:"sid"`
	CreatedAt int64  `json:"createdAt"`
}

type ChannelSession struct {
	SessionID string `json:"sessionId"`
	Key       string `json:"key"`
}

func safeStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}


type MessageItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
