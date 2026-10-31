package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"noise-server/internal/model"
	"strconv"
	"strings"
)

type SendResult struct {
	OK          bool   `json:"ok"`
	Reason      string `json:"reason,omitempty"`
	Description string `json:"description,omitempty"`
}

func SendTelegram(ctx context.Context, client *http.Client, c model.TelegramConfig, message string) SendResult {
	body := url.Values{"chat_id": {c.ChatID}, "text": {message}}
	if c.MessageThreadID != nil {
		body.Set("message_thread_id", strconv.FormatInt(*c.MessageThreadID, 10))
	}
	request, e := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint+c.BotToken+"/sendMessage", strings.NewReader(body.Encode()))
	if e != nil {
		return SendResult{Reason: "unreachable"}
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, e := client.Do(request)
	if e != nil {
		return SendResult{Reason: networkReason(e)}
	}
	defer response.Body.Close()
	var data struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	e = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&data)
	if e == nil && response.StatusCode >= 200 && response.StatusCode < 300 && data.OK {
		return SendResult{OK: true}
	}
	description := data.Description
	if description == "" {
		description = fmt.Sprintf("HTTP %d", response.StatusCode)
	}
	return SendResult{Reason: "rejected", Description: description}
}
func networkReason(e error) string {
	var n net.Error
	if errors.Is(e, context.DeadlineExceeded) || (errors.As(e, &n) && n.Timeout()) {
		return "timeout"
	}
	return "unreachable"
}
