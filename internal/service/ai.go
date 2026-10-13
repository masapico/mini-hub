package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/masapico/minihub/internal/domain"
	"github.com/masapico/minihub/internal/storage"
)

const aiMaxContextBytes = 1024 * 1024
const aiMaxContextMessages = 1000
const aiMaxAttachmentRequestBytes = 70 * 1024 * 1024

var validAIID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,60}$`)

func ValidateAIAccount(a domain.AIAccount) error {
	if !validAIID.MatchString(a.ID) {
		return fmt.Errorf("ai: invalid id %q", a.ID)
	}
	if strings.TrimSpace(a.Name) == "" || utf8.RuneCountInString(a.Name) > 100 {
		return fmt.Errorf("ai %s: name must contain 1 to 100 characters", a.ID)
	}
	if strings.TrimSpace(a.Model) == "" {
		return fmt.Errorf("ai %s: model must not be blank", a.ID)
	}
	u, err := url.Parse(a.URL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("ai %s: invalid HTTP/HTTPS URL", a.ID)
	}
	if a.Timeout != "" {
		if d, err := time.ParseDuration(a.Timeout); err != nil || d <= 0 {
			return fmt.Errorf("ai %s: timeout must be positive", a.ID)
		}
	}
	if a.Token != "" && a.TokenEnv != "" {
		return fmt.Errorf("ai %s: token and tokenEnv cannot both be set", a.ID)
	}
	if a.Token != "" && (strings.TrimSpace(a.Token) == "" || strings.ContainsAny(a.Token, "\r\n")) {
		return fmt.Errorf("ai %s: token must not be blank or contain line breaks", a.ID)
	}
	if a.Enabled && a.TokenEnv != "" && strings.TrimSpace(os.Getenv(a.TokenEnv)) == "" {
		return fmt.Errorf("ai %s: token environment variable is missing", a.ID)
	}
	if a.Temperature != nil && (*a.Temperature < 0 || *a.Temperature > 2.0) {
		return fmt.Errorf("ai %s: temperature must be between 0.0 and 2.0", a.ID)
	}
	if a.TopP != nil && (*a.TopP < 0 || *a.TopP > 1.0) {
		return fmt.Errorf("ai %s: top_p must be between 0.0 and 1.0", a.ID)
	}
	if a.MaxTokens != nil && *a.MaxTokens <= 0 {
		return fmt.Errorf("ai %s: max_tokens must be positive", a.ID)
	}
	return nil
}

type AIAccountSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ManagedAIAccount struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Enabled         bool     `json:"enabled"`
	URL             string   `json:"url"`
	Model           string   `json:"model"`
	SystemPrompt    string   `json:"systemPrompt,omitempty"`
	Timeout         string   `json:"timeout,omitempty"`
	TokenConfigured bool     `json:"tokenConfigured"`
	TokenEnv        string   `json:"tokenEnv,omitempty"`
	Groups          []string `json:"groups,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	MaxTokens       *int     `json:"maxTokens,omitempty"`
}

type aiEndpoint struct {
	domain.AIAccount
	token string
}

func (a aiEndpoint) RequestTimeout() time.Duration {
	if a.Timeout == "" {
		return 120 * time.Second
	}
	d, err := time.ParseDuration(a.Timeout)
	if err != nil || d <= 0 {
		return 120 * time.Second
	}
	return d
}

type aiJob struct {
	account             aiEndpoint
	author, channel, id string
	trigger             domain.Message
	attachments         []AIAttachmentPayload
}

type aiDispatcher struct {
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.RWMutex
	accounts []aiEndpoint
	queue    chan aiJob
	wg       sync.WaitGroup
	logger   *slog.Logger
	client   *http.Client
}

func (d *aiDispatcher) setAccounts(accounts []domain.AIAccount) {
	active := []aiEndpoint{}
	for _, a := range accounts {
		if a.Enabled {
			token := a.Token
			if a.TokenEnv != "" {
				token = os.Getenv(a.TokenEnv)
			}
			active = append(active, aiEndpoint{a, token})
		}
	}
	d.mu.Lock()
	d.accounts = active
	d.mu.Unlock()
}

func (d *aiDispatcher) getAccounts() []aiEndpoint {
	d.mu.RLock()
	defer d.mu.RUnlock()
	res := make([]aiEndpoint, len(d.accounts))
	copy(res, d.accounts)
	return res
}

func canUseAI(user *domain.User, a domain.AIAccount) bool {
	if user == nil {
		return false
	}
	if len(a.Groups) == 0 {
		return true
	}
	if user.Role == domain.RoleAdmin {
		return true
	}
	for _, g := range a.Groups {
		if member(user.Groups, g) {
			return true
		}
	}
	return false
}

func (s *Service) ReloadAIAccounts(ctx context.Context) error {
	if s.ai == nil {
		return nil
	}
	accounts, err := s.storage.ListAIAccounts(ctx)
	if err != nil {
		return err
	}
	s.ai.setAccounts(accounts)
	return nil
}

// StartAI is called once at startup, before serving requests or modifying users.
func (s *Service) StartAI(ctx context.Context, logger *slog.Logger) error {
	if s.ai != nil {
		return errors.New("AI dispatcher already started")
	}
	users, err := s.storage.ListUsers(ctx)
	if err != nil {
		return err
	}
	for _, u := range users {
		if strings.HasPrefix(u.ID, "ai_") {
			return fmt.Errorf("user %s conflicts with reserved AI identity prefix ai_", u.ID)
		}
	}
	if logger == nil {
		logger = slog.Default()
	}
	workerCtx, cancel := context.WithCancel(ctx)
	d := &aiDispatcher{
		ctx: workerCtx, cancel: cancel, queue: make(chan aiJob, 32), logger: logger,
		client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
	s.ai = d
	if err := s.ReloadAIAccounts(ctx); err != nil {
		return err
	}
	for i := 0; i < 4; i++ {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			for {
				select {
				case <-d.ctx.Done():
					return
				case job := <-d.queue:
					if d.ctx.Err() != nil {
						return
					}
					s.runAI(d, job)
				}
			}
		}()
	}
	return nil
}

// CloseAI cancels outstanding requests and waits before Storage can be closed.
func (s *Service) CloseAI() {
	if s.ai != nil {
		s.ai.cancel()
		s.ai.wg.Wait()
		s.ai.client.CloseIdleConnections()
	}
}

func (s *Service) AIAccounts(ctx context.Context, userID string) ([]AIAccountSummary, error) {
	u, err := s.user(ctx, userID)
	if err != nil {
		return nil, err
	}
	result := []AIAccountSummary{}
	if s.ai != nil {
		for _, a := range s.ai.getAccounts() {
			if a.Enabled && canUseAI(u, a.AIAccount) {
				result = append(result, AIAccountSummary{a.ID, a.Name})
			}
		}
	}
	return result, nil
}

func (s *Service) enqueueAI(author, channel string, m domain.Message, attPayloads ...AIAttachmentPayload) {
	d := s.ai
	if d == nil || m.AI != nil || d.ctx.Err() != nil {
		return
	}
	wanted := map[string]bool{}
	for _, match := range mentionPattern.FindAllStringSubmatch(m.Text, -1) {
		if match[1] == "ai" {
			wanted[match[2]] = true
		}
	}
	if len(wanted) == 0 {
		return
	}
	u, err := s.user(context.Background(), author)
	if err != nil {
		return
	}
	for _, a := range d.getAccounts() {
		if !wanted[a.ID] {
			continue
		}
		if !a.Enabled || !canUseAI(u, a.AIAccount) {
			continue
		}
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			d.logger.Error("create AI request identifier", "channelId", channel)
			continue
		}
		job := aiJob{account: a, author: author, channel: channel, id: hex.EncodeToString(id[:]), trigger: m, attachments: attPayloads}
		select {
		case <-d.ctx.Done():
			return
		case d.queue <- job:
		default:
			s.aiFailure(d, job, "現在AIへの依頼が混み合っています。時間をおいて再度メンションしてください。", "queue_full")
		}
	}
}

func (j aiJob) root() int64 {
	if j.trigger.ThreadRootSeq > 0 {
		return j.trigger.ThreadRootSeq
	}
	return j.trigger.Seq
}

func (s *Service) validateAIJob(ctx context.Context, j aiJob) error {
	if err := s.canPost(ctx, j.author, j.channel); err != nil {
		return err
	}
	u, err := s.user(ctx, j.author)
	if err != nil {
		return err
	}
	if !canUseAI(u, j.account.AIAccount) {
		return ErrForbidden
	}
	for _, seq := range []int64{j.root(), j.trigger.Seq} {
		m, err := s.storage.GetMessage(ctx, j.channel, seq)
		if err != nil {
			return err
		}
		m, err = s.publicMessage(ctx, j.channel, m)
		if err != nil {
			return err
		}
		if m.WithdrawnAt != nil || (seq == j.trigger.Seq && m.ID != j.trigger.ID) {
			return ErrForbidden
		}
	}
	return nil
}

type aiContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Name string `json:"name,omitempty"`
	Data string `json:"data,omitempty"`
}

type aiContextMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}
type aiRequest struct {
	Model       string             `json:"model"`
	Stream      bool               `json:"stream"`
	Messages    []aiContextMessage `json:"messages"`
	Temperature *float64           `json:"temperature,omitempty"`
	TopP        *float64           `json:"top_p,omitempty"`
	MaxTokens   *int               `json:"max_tokens,omitempty"`
}

var errAIContextLimit = errors.New("AI context limit exceeded")

func (s *Service) aiPayload(ctx context.Context, j aiJob) ([]byte, error) {
	if err := s.validateAIJob(ctx, j); err != nil {
		return nil, err
	}
	users, err := s.storage.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, u := range users {
		names[u.ID] = u.Name
	}
	p := aiRequest{
		Model:       j.account.Model,
		Messages:    []aiContextMessage{},
		Temperature: j.account.Temperature,
		TopP:        j.account.TopP,
		MaxTokens:   j.account.MaxTokens,
	}
	if strings.TrimSpace(j.account.SystemPrompt) != "" {
		p.Messages = append(p.Messages, aiContextMessage{Role: "system", Content: j.account.SystemPrompt})
	}
	maxBytes := aiMaxContextBytes
	if len(j.attachments) > 0 {
		maxBytes = aiMaxAttachmentRequestBytes
	}
	total := 0
	count := 0
	add := func(m domain.Message) error {
		count++
		if count > aiMaxContextMessages {
			return errAIContextLimit
		}
		m, err = s.publicMessage(ctx, j.channel, m)
		if err != nil {
			return err
		}
		if m.WithdrawnAt != nil || (m.AI != nil && m.AI.Kind == "error") {
			return nil
		}
		isTrigger := (m.Seq == j.trigger.Seq && m.ID == j.trigger.ID)
		text := m.Text
		if len(m.Attachments) > 0 {
			var tags []string
			for _, att := range m.Attachments {
				tags = append(tags, fmt.Sprintf("[添付ファイル: %s]", att.Filename))
			}
			text = text + "\n" + strings.Join(tags, "\n")
		}
		item := aiContextMessage{Role: "user", Content: text}
		if m.AI != nil && m.AI.ID == j.account.ID {
			item.Role = "assistant"
		} else {
			id, name := m.UserID, names[m.UserID]
			if m.AI != nil {
				name = m.AI.Name
			}
			if name == "" {
				name = id
			}
			prefix := ""
			if isTrigger {
				nowStr := time.Now().Format("2006-01-02 15:04:05 MST")
				prefix = fmt.Sprintf("[現在日時: %s]\n", nowStr)
			}
			userText := fmt.Sprintf("%s%s (%s):\n%s", prefix, name, id, text)
			if isTrigger && len(j.attachments) > 0 {
				parts := []aiContentPart{
					{Type: "text", Text: userText},
				}
				for _, att := range j.attachments {
					parts = append(parts, aiContentPart{
						Type: "file",
						Name: att.Name,
						Data: att.Base64,
					})
				}
				item.Content = parts
			} else {
				item.Content = userText
			}
		}
		encoded, err := json.Marshal(item)
		if err != nil {
			return err
		}
		total += len(encoded)
		if total > maxBytes {
			return errAIContextLimit
		}
		p.Messages = append(p.Messages, item)
		return nil
	}
	root, err := s.storage.GetMessage(ctx, j.channel, j.root())
	if err != nil {
		return nil, err
	}
	if err := add(root); err != nil {
		return nil, err
	}
	if j.trigger.ThreadRootSeq > 0 {
		after := int64(0)
		for {
			page, err := s.storage.GetMessages(ctx, j.channel, storage.MessageQuery{ThreadRootSeq: j.root(), AfterSeq: &after, Limit: 100})
			if err != nil {
				return nil, err
			}
			done := false
			for _, m := range page.Messages {
				if m.Seq > j.trigger.Seq {
					done = true
					break
				}
				if err := add(m); err != nil {
					return nil, err
				}
				after = m.Seq
				if m.Seq == j.trigger.Seq {
					done = true
					break
				}
			}
			if done || page.NextAfter == nil || len(page.Messages) == 0 {
				break
			}
		}
	}
	b, err := json.Marshal(p)
	if len(b) > maxBytes {
		return nil, errAIContextLimit
	}
	return b, err
}

func (s *Service) runAI(d *aiDispatcher, j aiJob) {
	ctx, cancel := context.WithTimeout(d.ctx, j.account.RequestTimeout())
	defer cancel()
	body, err := s.aiPayload(ctx, j)
	if err != nil {
		if errors.Is(err, errAIContextLimit) {
			s.aiFailure(d, j, "スレッドが送信上限（1,000件・1 MiB）を超えています。短い新規投稿から呼び出してください。", "context_limit")
		} else {
			s.aiFailure(d, j, "AIへの依頼を準備できませんでした。", "context_unavailable")
		}
		return
	}
	if err := s.validateAIJob(ctx, j); err != nil {
		s.aiFailure(d, j, "AIへの依頼を送信できませんでした。時間をおいて再度お試しください。", "request_unavailable")
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.account.URL, bytes.NewReader(body))
	if err != nil {
		s.aiFailure(d, j, "AIへの接続に失敗しました。", "request_invalid")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-MiniHub-Request-ID", j.id)
	if j.account.token != "" {
		req.Header.Set("Authorization", "Bearer "+j.account.token)
	}
	// No automatic retry: a lost response may already have generated an answer.
	resp, err := d.client.Do(req)
	if err != nil {
		s.aiFailure(d, j, "AIへの接続に失敗したか、処理がタイムアウトしました。時間をおいて再度お試しください。", "request_failed")
		return
	}
	defer resp.Body.Close()
	media, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if resp.StatusCode != http.StatusOK || media != "application/json" {
		s.aiFailure(d, j, "AIから正常な応答を受信できませんでした。", "response_invalid")
		return
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, aiMaxContextBytes+1))
	var reply struct {
		Choices []struct {
			Message struct {
				Content *string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err != nil || len(b) > aiMaxContextBytes || json.Unmarshal(b, &reply) != nil || len(reply.Choices) == 0 || reply.Choices[0].Message.Content == nil {
		s.aiFailure(d, j, "AIの回答形式または回答サイズが不正です（本文は16 KiB以内）。", "response_invalid")
		return
	}
	answer := *reply.Choices[0].Message.Content
	if len(answer) > MaxMessageBytes || strings.TrimSpace(answer) == "" {
		s.aiFailure(d, j, "AIの回答形式または回答サイズが不正です（本文は16 KiB以内）。", "response_invalid")
		return
	}
	if err := s.saveAIReply(d.ctx, j, answer, "answer"); err != nil {
		d.logger.Warn("AI reply not saved", "requestId", j.id, "channelId", j.channel)
	}
}

func (s *Service) aiFailure(d *aiDispatcher, j aiJob, text, code string) {
	d.logger.Warn("AI request failed", "requestId", j.id, "aiId", j.account.ID, "channelId", j.channel, "code", code)
	if d.ctx.Err() == nil {
		if err := s.saveAIReply(d.ctx, j, text, "error"); err != nil {
			d.logger.Warn("AI failure notice not saved", "requestId", j.id, "channelId", j.channel)
		}
	}
}

func (s *Service) saveAIReply(ctx context.Context, j aiJob, text, kind string) error {
	rootLock := s.channelLock("withdraw:message:" + j.channel + ":" + strconv.FormatInt(j.root(), 10))
	rootLock.Lock()
	defer rootLock.Unlock()
	if j.trigger.Seq != j.root() {
		triggerLock := s.channelLock("withdraw:message:" + j.channel + ":" + strconv.FormatInt(j.trigger.Seq, 10))
		triggerLock.Lock()
		defer triggerLock.Unlock()
	}
	if err := s.validateAIJob(ctx, j); err != nil {
		return err
	}
	m, err := s.storage.AddMessage(ctx, j.channel, domain.Message{UserID: "ai_" + j.account.ID, Text: text, ThreadRootSeq: j.root(), MentionUserIDs: []string{}, AI: &domain.AIMessage{ID: j.account.ID, Name: j.account.Name, RequestID: j.id, TriggerMessageID: j.trigger.ID, RequestedBy: j.author, Kind: kind}})
	if err != nil {
		return err
	}
	if publisher, ok := s.publisher.(ThreadPublisher); ok {
		publisher.ThreadUpdated(ctx, j.channel, j.root(), m.Seq, m.UserID)
	}
	return nil
}

func (s *Service) ListManagedAIAccounts(ctx context.Context, actorID string) ([]ManagedAIAccount, error) {
	actor, err := s.user(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if actor.Role != domain.RoleAdmin {
		return nil, ErrForbidden
	}
	list, err := s.storage.ListAIAccounts(ctx)
	if err != nil {
		return nil, err
	}
	res := make([]ManagedAIAccount, len(list))
	for i, a := range list {
		res[i] = ManagedAIAccount{
			ID:              a.ID,
			Name:            a.Name,
			Enabled:         a.Enabled,
			URL:             a.URL,
			Model:           a.Model,
			SystemPrompt:    a.SystemPrompt,
			Timeout:         a.Timeout,
			TokenConfigured: a.Token != "",
			TokenEnv:        a.TokenEnv,
			Groups:          a.Groups,
			Temperature:     a.Temperature,
			TopP:            a.TopP,
			MaxTokens:       a.MaxTokens,
		}
	}
	return res, nil
}

func (s *Service) GetManagedAIAccount(ctx context.Context, actorID, id string) (*ManagedAIAccount, error) {
	actor, err := s.user(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if actor.Role != domain.RoleAdmin {
		return nil, ErrForbidden
	}
	a, err := s.storage.GetAIAccount(ctx, id)
	if err != nil {
		return nil, err
	}
	return &ManagedAIAccount{
		ID:              a.ID,
		Name:            a.Name,
		Enabled:         a.Enabled,
		URL:             a.URL,
		Model:           a.Model,
		SystemPrompt:    a.SystemPrompt,
		Timeout:         a.Timeout,
		TokenConfigured: a.Token != "",
		TokenEnv:        a.TokenEnv,
		Groups:          a.Groups,
		Temperature:     a.Temperature,
		TopP:            a.TopP,
		MaxTokens:       a.MaxTokens,
	}, nil
}

func (s *Service) CreateAIAccount(ctx context.Context, actorID string, account domain.AIAccount) (*ManagedAIAccount, error) {
	actor, err := s.user(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if actor.Role != domain.RoleAdmin {
		return nil, ErrForbidden
	}
	account.ID = strings.TrimSpace(account.ID)
	account.Name = strings.TrimSpace(account.Name)
	account.Model = strings.TrimSpace(account.Model)
	account.URL = strings.TrimSpace(account.URL)
	if err := ValidateAIAccount(account); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if _, err := s.storage.GetAIAccount(ctx, account.ID); err == nil {
		return nil, ErrConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	users, err := s.storage.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		if u.ID == "ai_"+account.ID {
			return nil, fmt.Errorf("%w: AI id conflicts with existing user ai_%s", ErrConflict, account.ID)
		}
	}
	account.Version = domain.Version
	account.Groups = unique(account.Groups)
	if err := s.storage.SaveAIAccount(ctx, &account); err != nil {
		return nil, err
	}
	if err := s.ReloadAIAccounts(ctx); err != nil {
		return nil, err
	}
	return &ManagedAIAccount{
		ID:              account.ID,
		Name:            account.Name,
		Enabled:         account.Enabled,
		URL:             account.URL,
		Model:           account.Model,
		SystemPrompt:    account.SystemPrompt,
		Timeout:         account.Timeout,
		TokenConfigured: account.Token != "",
		TokenEnv:        account.TokenEnv,
		Groups:          account.Groups,
		Temperature:     account.Temperature,
		TopP:            account.TopP,
		MaxTokens:       account.MaxTokens,
	}, nil
}

func (s *Service) UpdateAIAccount(ctx context.Context, actorID string, id string, account domain.AIAccount, keepExistingToken bool) (*ManagedAIAccount, error) {
	actor, err := s.user(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if actor.Role != domain.RoleAdmin {
		return nil, ErrForbidden
	}
	existing, err := s.storage.GetAIAccount(ctx, id)
	if err != nil {
		return nil, err
	}
	account.ID = id
	account.Name = strings.TrimSpace(account.Name)
	account.Model = strings.TrimSpace(account.Model)
	account.URL = strings.TrimSpace(account.URL)
	if keepExistingToken && account.Token == "" && account.TokenEnv == "" {
		account.Token = existing.Token
		account.TokenEnv = existing.TokenEnv
	}
	if err := ValidateAIAccount(account); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	account.Version = domain.Version
	account.Groups = unique(account.Groups)
	if err := s.storage.SaveAIAccount(ctx, &account); err != nil {
		return nil, err
	}
	if err := s.ReloadAIAccounts(ctx); err != nil {
		return nil, err
	}
	return &ManagedAIAccount{
		ID:              account.ID,
		Name:            account.Name,
		Enabled:         account.Enabled,
		URL:             account.URL,
		Model:           account.Model,
		SystemPrompt:    account.SystemPrompt,
		Timeout:         account.Timeout,
		TokenConfigured: account.Token != "",
		TokenEnv:        account.TokenEnv,
		Groups:          account.Groups,
		Temperature:     account.Temperature,
		TopP:            account.TopP,
		MaxTokens:       account.MaxTokens,
	}, nil
}

func (s *Service) DeleteAIAccount(ctx context.Context, actorID, id string) error {
	actor, err := s.user(ctx, actorID)
	if err != nil {
		return err
	}
	if actor.Role != domain.RoleAdmin {
		return ErrForbidden
	}
	if err := s.storage.DeleteAIAccount(ctx, id); err != nil {
		return err
	}
	return s.ReloadAIAccounts(ctx)
}
