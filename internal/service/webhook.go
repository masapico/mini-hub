package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/masapico/minihub/internal/domain"
)

type CreateWebhookInput struct {
	Name        string `json:"name"`
	ChannelID   string `json:"channelId"`
	Description string `json:"description,omitempty"`
	Enabled     *bool  `json:"enabled,omitempty"`
}

type UpdateWebhookInput struct {
	Name        string `json:"name"`
	ChannelID   string `json:"channelId"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`
}

type CreatedWebhookResult struct {
	Webhook  domain.Webhook `json:"webhook"`
	RawToken string         `json:"token"`
}

type webhookRateLimiter struct {
	mu      sync.Mutex
	history map[string][]time.Time
}

func newWebhookRateLimiter() *webhookRateLimiter {
	return &webhookRateLimiter{
		history: make(map[string][]time.Time),
	}
}

func (rl *webhookRateLimiter) allow(webhookID string, now time.Time) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	windowSec := now.Add(-1 * time.Second)
	windowMin := now.Add(-1 * time.Minute)

	times := rl.history[webhookID]
	start := 0
	for start < len(times) && times[start].Before(windowMin) {
		start++
	}
	times = times[start:]

	secCount := 0
	for i := len(times) - 1; i >= 0; i-- {
		if times[i].After(windowSec) {
			secCount++
		} else {
			break
		}
	}

	if secCount >= 5 || len(times) >= 60 {
		rl.history[webhookID] = times
		return false
	}

	rl.history[webhookID] = append(times, now)
	return true
}

func hashWebhookToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func generateWebhookToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func generateWebhookID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "whk_" + hex.EncodeToString(b), nil
}

func (s *Service) checkAdmin(ctx context.Context, actorID string) error {
	actor, err := s.user(ctx, actorID)
	if err != nil {
		return err
	}
	if actor.Role != domain.RoleAdmin {
		return ErrForbidden
	}
	return nil
}

func (s *Service) ListAdminWebhooks(ctx context.Context, actorID string) ([]domain.Webhook, error) {
	if err := s.checkAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	list, err := s.storage.ListWebhooks(ctx)
	if err != nil {
		return nil, err
	}
	for i := range list {
		list[i].TokenHash = ""
	}
	return list, nil
}

func (s *Service) GetAdminWebhook(ctx context.Context, actorID, id string) (*domain.Webhook, error) {
	if err := s.checkAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	wh, err := s.storage.GetWebhook(ctx, id)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	wh.TokenHash = ""
	return wh, nil
}

func (s *Service) CreateWebhook(ctx context.Context, actorID string, in CreateWebhookInput) (*CreatedWebhookResult, error) {
	if err := s.checkAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 100 {
		return nil, fmt.Errorf("%w: 表示名は必須で、100文字以内にしてください", ErrInvalid)
	}
	if in.ChannelID == "" {
		return nil, fmt.Errorf("%w: 投稿先チャンネルを指定してください", ErrInvalid)
	}
	ch, err := s.channel(ctx, in.ChannelID)
	if err != nil {
		return nil, err
	}
	if ch.ArchivedAt != nil {
		return nil, fmt.Errorf("%w: アーカイブ済みのチャンネルは指定できません", ErrInvalid)
	}

	id, err := generateWebhookID()
	if err != nil {
		return nil, err
	}
	rawToken, err := generateWebhookToken()
	if err != nil {
		return nil, err
	}

	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}

	now := time.Now().UTC()
	wh := domain.Webhook{
		Version:     domain.Version,
		ID:          id,
		ChannelID:   in.ChannelID,
		Name:        in.Name,
		Description: strings.TrimSpace(in.Description),
		TokenHash:   hashWebhookToken(rawToken),
		Enabled:     enabled,
		CreatedBy:   actorID,
		CreatedAt:   now,
	}

	if err := s.storage.SaveWebhook(ctx, &wh); err != nil {
		return nil, err
	}

	resWH := wh
	resWH.TokenHash = ""
	return &CreatedWebhookResult{
		Webhook:  resWH,
		RawToken: rawToken,
	}, nil
}

func (s *Service) UpdateWebhook(ctx context.Context, actorID, id string, in UpdateWebhookInput) (*domain.Webhook, error) {
	if err := s.checkAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 100 {
		return nil, fmt.Errorf("%w: 表示名は必須で、100文字以内にしてください", ErrInvalid)
	}
	if in.ChannelID == "" {
		return nil, fmt.Errorf("%w: 投稿先チャンネルを指定してください", ErrInvalid)
	}
	ch, err := s.channel(ctx, in.ChannelID)
	if err != nil {
		return nil, err
	}
	if ch.ArchivedAt != nil {
		return nil, fmt.Errorf("%w: アーカイブ済みのチャンネルは指定できません", ErrInvalid)
	}

	wh, err := s.storage.GetWebhook(ctx, id)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	wh.Name = in.Name
	wh.ChannelID = in.ChannelID
	wh.Description = strings.TrimSpace(in.Description)
	wh.Enabled = in.Enabled

	if err := s.storage.SaveWebhook(ctx, wh); err != nil {
		return nil, err
	}

	wh.TokenHash = ""
	return wh, nil
}

func (s *Service) RegenerateWebhookToken(ctx context.Context, actorID, id string) (*CreatedWebhookResult, error) {
	if err := s.checkAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	wh, err := s.storage.GetWebhook(ctx, id)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	rawToken, err := generateWebhookToken()
	if err != nil {
		return nil, err
	}
	wh.TokenHash = hashWebhookToken(rawToken)

	if err := s.storage.SaveWebhook(ctx, wh); err != nil {
		return nil, err
	}

	resWH := *wh
	resWH.TokenHash = ""
	return &CreatedWebhookResult{
		Webhook:  resWH,
		RawToken: rawToken,
	}, nil
}

func (s *Service) DeleteWebhook(ctx context.Context, actorID, id string) error {
	if err := s.checkAdmin(ctx, actorID); err != nil {
		return err
	}
	if _, err := s.storage.GetWebhook(ctx, id); errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	return s.storage.DeleteWebhook(ctx, id)
}

func (s *Service) PostWebhookMessage(ctx context.Context, webhookID, rawToken, text string, threadRootSeq int64) (domain.Message, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" || webhookID == "" {
		return domain.Message{}, ErrUnauthenticated
	}

	wh, err := s.storage.GetWebhook(ctx, webhookID)
	if errors.Is(err, os.ErrNotExist) {
		return domain.Message{}, ErrUnauthenticated
	}
	if err != nil {
		return domain.Message{}, err
	}

	tokenHash := hashWebhookToken(rawToken)
	if subtle.ConstantTimeCompare([]byte(wh.TokenHash), []byte(tokenHash)) != 1 {
		return domain.Message{}, ErrUnauthenticated
	}

	if !wh.Enabled {
		return domain.Message{}, ErrForbidden
	}

	ch, err := s.channel(ctx, wh.ChannelID)
	if err != nil {
		return domain.Message{}, err
	}
	if ch.ArchivedAt != nil {
		return domain.Message{}, ErrForbidden
	}

	if !s.webhookLimiter.allow(wh.ID, time.Now()) {
		return domain.Message{}, ErrRateLimited
	}

	text = strings.TrimSpace(text)
	if text == "" || len([]byte(text)) > MaxMessageBytes {
		return domain.Message{}, fmt.Errorf("%w: メッセージは空にせず、%dバイト以内にしてください", ErrInvalid, MaxMessageBytes)
	}

	recipients, err := s.mentionRecipients(ctx, wh.ID, wh.ChannelID, text)
	if err != nil {
		return domain.Message{}, err
	}

	webhookInfo := &domain.WebhookMessageInfo{
		ID:   wh.ID,
		Name: wh.Name,
	}

	var committed domain.Message
	if threadRootSeq > 0 {
		lock := s.channelLock("withdraw:message:" + wh.ChannelID + ":" + strconv.FormatInt(threadRootSeq, 10))
		lock.Lock()
		defer lock.Unlock()

		parent, err := s.storage.GetMessage(ctx, wh.ChannelID, threadRootSeq)
		if errors.Is(err, os.ErrNotExist) {
			return domain.Message{}, ErrNotFound
		}
		if err != nil {
			return domain.Message{}, err
		}
		if parent.ThreadRootSeq != 0 {
			return domain.Message{}, fmt.Errorf("%w: 返信をスレッドの起点にはできません", ErrInvalid)
		}
		if parent.WithdrawnAt != nil {
			return domain.Message{}, ErrInvalid
		}

		m, err := s.storage.AddMessage(ctx, wh.ChannelID, domain.Message{
			UserID:         wh.ID,
			Webhook:        webhookInfo,
			Text:           text,
			ThreadRootSeq:  threadRootSeq,
			MentionUserIDs: recipients,
		})
		if err != nil {
			return domain.Message{}, err
		}
		committed = m

		if s.publisher != nil {
			s.publisher.NewMessage(ctx, wh.ChannelID, m.Seq)
		}
		if tp, ok := s.publisher.(ThreadPublisher); ok {
			tp.ThreadUpdated(ctx, wh.ChannelID, threadRootSeq, m.Seq, wh.ID)
		}
	} else {
		m, err := s.storage.AddMessage(ctx, wh.ChannelID, domain.Message{
			UserID:         wh.ID,
			Webhook:        webhookInfo,
			Text:           text,
			MentionUserIDs: recipients,
		})
		if err != nil {
			return domain.Message{}, err
		}
		committed = m

		if s.publisher != nil {
			s.publisher.NewMessage(ctx, wh.ChannelID, m.Seq)
		}
	}

	_ = s.storage.TouchWebhookUsed(ctx, wh.ID, time.Now().UTC())
	return committed, nil
}

