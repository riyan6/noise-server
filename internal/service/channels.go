package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/netip"
	"noise-server/internal/integration"
	"noise-server/internal/model"
	"regexp"
	"strings"
)

var botToken = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]+$`)
var chatID = regexp.MustCompile(`^(?:-?[0-9]+|@[A-Za-z][A-Za-z0-9_]{3,})$`)

func validateTelegram(c model.TelegramConfig) error {
	if !botToken.MatchString(c.BotToken) || len(c.BotToken) > 512 || !chatID.MatchString(c.ChatID) || len(c.ChatID) > 128 || !URL(c.Endpoint, false) || strings.Contains(c.Endpoint, "?") || !strings.HasSuffix(c.Endpoint, "/bot") || (c.MessageThreadID != nil && (*c.MessageThreadID < 1 || *c.MessageThreadID > 9007199254740991)) {
		return invalid("invalid Telegram token, chat ID, API base URL (ending in /bot), or topic ID")
	}
	return nil
}
func redacted(c model.Channel) model.Channel {
	_, secret, _ := strings.Cut(c.Config.BotToken, ":")
	c.TokenHint = "••••"
	if len(secret) > 4 {
		bot, _, _ := strings.Cut(c.Config.BotToken, ":")
		c.TokenHint = bot + ":••••" + secret[len(secret)-4:]
	}
	c.Config.BotToken = ""
	return c
}
func (s *Service) Channels(ctx context.Context) ([]model.Channel, error) {
	values, e := s.store.Channels(ctx)
	if e != nil {
		return nil, e
	}
	for i := range values {
		values[i] = redacted(values[i])
	}
	return values, nil
}

type ChannelChanges struct {
	Name   *string               `json:"name"`
	Type   *string               `json:"type"`
	Config *model.TelegramConfig `json:"config"`
}

func (s *Service) SaveChannel(ctx context.Context, id string, p ChannelChanges) (model.Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now().UnixMilli()
	c := model.Channel{ID: rand.Text(), Type: "telegram", CreatedAt: now, Config: model.TelegramConfig{Endpoint: "https://api.telegram.org/bot"}}
	if id != "" {
		values, e := s.store.Channels(ctx)
		if e != nil {
			return c, e
		}
		found := false
		for _, v := range values {
			if v.ID == id {
				c = v
				found = true
				break
			}
		}
		if !found {
			return c, model.ErrNotFound
		}
	}
	if p.Name != nil {
		c.Name = strings.TrimSpace(*p.Name)
	}
	if p.Type != nil {
		c.Type = *p.Type
	}
	if p.Config != nil {
		next := *p.Config
		if next.BotToken == "" {
			next.BotToken = c.Config.BotToken
		}
		c.Config = next
	}
	c.UpdatedAt = now
	if c.Type != "telegram" || !text(c.Name, 240, false) {
		return c, invalid("invalid channel type or name")
	}
	if e := validateTelegram(c.Config); e != nil {
		return c, e
	}
	if e := s.store.SaveChannel(ctx, c); e != nil {
		return c, e
	}
	return redacted(c), nil
}
func (s *Service) DeleteChannel(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.DeleteChannel(ctx, id)
}
func (s *Service) TestChannel(ctx context.Context, id string, c *model.TelegramConfig) (integration.SendResult, error) {
	var saved model.TelegramConfig
	if id != "" {
		values, e := s.store.Channels(ctx)
		if e != nil {
			return integration.SendResult{}, e
		}
		found := false
		for _, v := range values {
			if v.ID == id {
				saved = v.Config
				found = true
				break
			}
		}
		if !found {
			return integration.SendResult{}, model.ErrNotFound
		}
	}
	if c != nil {
		next := *c
		if next.BotToken == "" {
			next.BotToken = saved.BotToken
		}
		saved = next
	}
	if e := validateTelegram(saved); e != nil {
		return integration.SendResult{}, e
	}
	message := fmt.Sprintf("Noise 测试通知\n时间：%s\n通知渠道配置正常。", s.Now().In(s.Location).Format("2006-01-02 15:04:05 MST"))
	return integration.SendTelegram(ctx, s.Client, saved, message), nil
}
func (s *Service) GeoLookup(ctx context.Context, provider, ip, userAgent string) (integration.GeoLookup, error) {
	if !contains(provider, "ipinfo", "ip-api", "ip.sb") || !text(userAgent, 512, false) {
		return integration.GeoLookup{}, invalid("invalid GeoIP provider or User-Agent")
	}
	if ip != "" {
		if _, e := netip.ParseAddr(ip); e != nil {
			return integration.GeoLookup{}, invalid("invalid IP address")
		}
	}
	if userAgent == "" {
		userAgent = s.Settings().GeoIP.UserAgent
	}
	return integration.LookupGeoIP(ctx, s.Client, provider, ip, userAgent), nil
}
