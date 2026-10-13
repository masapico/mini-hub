package service

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/masapico/minihub/internal/domain"
)

func TestAIPayloadWithAttachmentAndTimestamp(t *testing.T) {
	s, ctx, user, ch := setupTestService(t)

	// Stage an attachment
	fileContent := "sample binary excel content"
	summary, err := s.StageAttachment(ctx, user, ch, "report.xlsx", strings.NewReader(fileContent))
	if err != nil {
		t.Fatal(err)
	}

	// Post message with AI mention and attachment
	msg, err := s.PostMessage(ctx, user, ch, "@ai:helper この売上データを分析してください", summary.ID)
	if err != nil {
		t.Fatal(err)
	}

	endpoint := aiEndpoint{
		AIAccount: domain.AIAccount{
			ID:    "helper",
			Model: "test-model",
		},
	}
	expectedB64 := base64.StdEncoding.EncodeToString([]byte(fileContent))
	job := aiJob{
		account:  endpoint,
		author:   user,
		channel:  ch,
		id:       "req-001",
		trigger:  msg,
		attachments: []AIAttachmentPayload{
			{Name: summary.Filename, Base64: expectedB64},
		},
	}

	payloadBytes, err := s.aiPayload(ctx, job)
	if err != nil {
		t.Fatalf("aiPayload failed: %v", err)
	}

	var req struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(payloadBytes, &req); err != nil {
		t.Fatalf("unmarshal payload failed: %v", err)
	}

	if len(req.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(req.Messages))
	}

	userMsg := req.Messages[0]
	if userMsg.Role != "user" {
		t.Fatalf("expected role user, got %s", userMsg.Role)
	}

	// Verify content is a parts array
	var parts []aiContentPart
	if err := json.Unmarshal(userMsg.Content, &parts); err != nil {
		t.Fatalf("expected content parts array, unmarshal error: %v (raw: %s)", err, string(userMsg.Content))
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts (text + file), got %d: %+v", len(parts), parts)
	}

	// 1. Text part must contain timestamp prefix [現在日時: ...]
	textPart := parts[0]
	if textPart.Type != "text" {
		t.Fatalf("expected part 0 to be text, got %s", textPart.Type)
	}
	if !strings.Contains(textPart.Text, "[現在日時:") {
		t.Fatalf("expected timestamp prefix in text, got: %s", textPart.Text)
	}
	if !strings.Contains(textPart.Text, "Alice (alice):") {
		t.Fatalf("expected user header in text, got: %s", textPart.Text)
	}

	// 2. File part must contain filename and base64 data
	filePart := parts[1]
	if filePart.Type != "file" {
		t.Fatalf("expected part 1 to be file, got %s", filePart.Type)
	}
	if filePart.Name != "report.xlsx" {
		t.Fatalf("expected filename report.xlsx, got %s", filePart.Name)
	}
	if filePart.Data != expectedB64 {
		t.Fatalf("expected base64 %s, got %s", expectedB64, filePart.Data)
	}
}

func TestAIPayloadThreadHistoryAttachments(t *testing.T) {
	s, ctx, user, ch := setupTestService(t)

	// Stage an attachment for root message
	summary, err := s.StageAttachment(ctx, user, ch, "plan.docx", strings.NewReader("docx content"))
	if err != nil {
		t.Fatal(err)
	}

	// Root message has attachment
	root, err := s.PostMessage(ctx, user, ch, "@ai:helper 最初のリクエスト", summary.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Thread reply without attachment
	reply, err := s.PostThreadMessage(ctx, user, ch, root.Seq, "@ai:helper 続けて教えてください")
	if err != nil {
		t.Fatal(err)
	}

	endpoint := aiEndpoint{
		AIAccount: domain.AIAccount{
			ID:    "helper",
			Model: "test-model",
		},
	}
	job := aiJob{
		account: endpoint,
		author:  user,
		channel: ch,
		id:      "req-002",
		trigger: reply,
	}

	payloadBytes, err := s.aiPayload(ctx, job)
	if err != nil {
		t.Fatalf("aiPayload failed: %v", err)
	}

	var req struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(payloadBytes, &req); err != nil {
		t.Fatalf("unmarshal payload failed: %v", err)
	}

	if len(req.Messages) != 2 {
		t.Fatalf("expected 2 messages in thread history, got %d", len(req.Messages))
	}

	// Root message in history: string content with [添付ファイル: plan.docx]
	var rootText string
	if err := json.Unmarshal(req.Messages[0].Content, &rootText); err != nil {
		t.Fatalf("expected history content to be plain string: %v", err)
	}
	if !strings.Contains(rootText, "[添付ファイル: plan.docx]") {
		t.Fatalf("expected root history to contain filename reference, got: %s", rootText)
	}

	// Reply message (trigger without attachment): string content with [現在日時: ...]
	var replyText string
	if err := json.Unmarshal(req.Messages[1].Content, &replyText); err != nil {
		t.Fatalf("expected reply content to be plain string: %v", err)
	}
	if !strings.Contains(replyText, "[現在日時:") {
		t.Fatalf("expected reply trigger to contain timestamp, got: %s", replyText)
	}
}

func TestAIPayloadParametersAndPrompt(t *testing.T) {
	s, ctx, user, ch := setupTestService(t)
	msg, err := s.PostMessage(ctx, user, ch, "@ai:helper 質問です")
	if err != nil {
		t.Fatal(err)
	}

	temp := 0.7
	topP := 0.95
	maxTokens := 1500

	// 1. With prompt and parameters
	jobWithAll := aiJob{
		account: aiEndpoint{
			AIAccount: domain.AIAccount{
				ID:           "helper",
				Model:        "test-model",
				SystemPrompt: "指示文です",
				Temperature:  &temp,
				TopP:         &topP,
				MaxTokens:    &maxTokens,
			},
		},
		author:  user,
		channel: ch,
		id:      "req-003",
		trigger: msg,
	}
	bytesWithAll, err := s.aiPayload(ctx, jobWithAll)
	if err != nil {
		t.Fatal(err)
	}
	var reqWithAll struct {
		Model       string `json:"model"`
		Temperature *float64 `json:"temperature"`
		TopP        *float64 `json:"top_p"`
		MaxTokens   *int     `json:"max_tokens"`
		Messages    []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(bytesWithAll, &reqWithAll); err != nil {
		t.Fatal(err)
	}
	if reqWithAll.Temperature == nil || *reqWithAll.Temperature != 0.7 {
		t.Fatalf("expected temperature 0.7, got %v", reqWithAll.Temperature)
	}
	if reqWithAll.TopP == nil || *reqWithAll.TopP != 0.95 {
		t.Fatalf("expected top_p 0.95, got %v", reqWithAll.TopP)
	}
	if reqWithAll.MaxTokens == nil || *reqWithAll.MaxTokens != 1500 {
		t.Fatalf("expected max_tokens 1500, got %v", reqWithAll.MaxTokens)
	}
	if len(reqWithAll.Messages) != 2 || reqWithAll.Messages[0].Role != "system" || reqWithAll.Messages[0].Content != "指示文です" {
		t.Fatalf("expected system message, got %#v", reqWithAll.Messages)
	}

	// 2. Without prompt and parameters (empty values)
	jobBlank := aiJob{
		account: aiEndpoint{
			AIAccount: domain.AIAccount{
				ID:    "helper",
				Model: "test-model",
			},
		},
		author:  user,
		channel: ch,
		id:      "req-004",
		trigger: msg,
	}
	bytesBlank, err := s.aiPayload(ctx, jobBlank)
	if err != nil {
		t.Fatal(err)
	}
	var rawMap map[string]any
	if err := json.Unmarshal(bytesBlank, &rawMap); err != nil {
		t.Fatal(err)
	}
	if _, ok := rawMap["temperature"]; ok {
		t.Fatal("expected temperature to be omitted when nil")
	}
	if _, ok := rawMap["top_p"]; ok {
		t.Fatal("expected top_p to be omitted when nil")
	}
	if _, ok := rawMap["max_tokens"]; ok {
		t.Fatal("expected max_tokens to be omitted when nil")
	}
	var reqBlank struct {
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(bytesBlank, &reqBlank); err != nil {
		t.Fatal(err)
	}
	if len(reqBlank.Messages) != 1 || reqBlank.Messages[0].Role != "user" {
		t.Fatalf("expected only user message when SystemPrompt is empty, got %#v", reqBlank.Messages)
	}
}

func TestAIGroupAuthorization(t *testing.T) {
	user := domain.User{ID: "alice", Role: domain.RoleUser, Groups: []string{"engineering"}}
	admin := domain.User{ID: "admin", Role: domain.RoleAdmin}
	other := domain.User{ID: "bob", Role: domain.RoleUser, Groups: []string{"sales"}}

	// Unrestricted account
	globalAccount := domain.AIAccount{ID: "global", Enabled: true}
	if !canUseAI(&user, globalAccount) || !canUseAI(&admin, globalAccount) || !canUseAI(&other, globalAccount) {
		t.Fatal("unrestricted AI should be accessible to all")
	}

	// Group-restricted account
	engAccount := domain.AIAccount{ID: "eng", Enabled: true, Groups: []string{"engineering"}}
	if !canUseAI(&user, engAccount) {
		t.Fatal("eng user should be allowed")
	}
	if !canUseAI(&admin, engAccount) {
		t.Fatal("admin should be allowed regardless of groups")
	}
	if canUseAI(&other, engAccount) {
		t.Fatal("sales user should not be allowed for eng AI")
	}
}

func TestDirectAICallQuotaAndUsage(t *testing.T) {
	s, ctx, user, _ := setupTestService(t)

	// Test GetAIQuota initially
	quota, err := s.GetAIQuota(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if quota.DailyQuota != 0 || quota.DailyUsed != 0 || !quota.Enabled {
		t.Fatalf("unexpected initial quota: %+v", quota)
	}

	// Setup mock AI server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": "AI reply from canvas",
					},
				},
			},
		})
	}))
	defer server.Close()

	account := domain.AIAccount{
		ID:      "board-ai",
		Name:    "Board AI",
		Enabled: true,
		URL:     server.URL,
		Model:   "test-model",
	}
	if err := s.storage.SaveAIAccount(ctx, &account); err != nil {
		t.Fatal(err)
	}
	if err := s.StartAI(ctx, nil); err != nil {
		t.Fatal(err)
	}
	defer s.CloseAI()

	// 1. Quota = 0 (unlimited)
	s.SetAICanvasOptions(true, 0)
	resp, err := s.DirectAICall(ctx, user, domain.AIDirectRequest{
		AIAccountID: "board-ai",
		Messages: []domain.AIDirectMessage{
			{Role: "user", Content: "Hello AI"},
		},
	})
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if resp.Reply != "AI reply from canvas" || resp.DailyUsed != 1 {
		t.Fatalf("unexpected resp: %+v", resp)
	}

	// 2. Set quota to 1
	s.SetAICanvasOptions(true, 1)
	// Already used 1, so next call must fail with ErrQuotaExceeded
	_, err = s.DirectAICall(ctx, user, domain.AIDirectRequest{
		AIAccountID: "board-ai",
		Messages: []domain.AIDirectMessage{
			{Role: "user", Content: "Another call"},
		},
	})
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("expected ErrQuotaExceeded, got %v", err)
	}

	// 3. Disabled canvas
	s.SetAICanvasOptions(false, 0)
	_, err = s.DirectAICall(ctx, user, domain.AIDirectRequest{
		AIAccountID: "board-ai",
		Messages: []domain.AIDirectMessage{
			{Role: "user", Content: "Call when disabled"},
		},
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden when disabled, got %v", err)
	}

	// 4. Admin summary
	summary, err := s.GetAIDailyUsageSummary(ctx, "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalCount != 1 || len(summary.UserUsage) != 1 || summary.UserUsage[0].UserID != user {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

