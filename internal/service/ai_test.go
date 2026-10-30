package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/masapico/minihub/internal/domain"
	"github.com/masapico/minihub/internal/storage"
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

func TestMultipleAIMentionsRejected(t *testing.T) {
	s, ctx, user, ch := setupTestService(t)

	// Single AI mention is accepted
	msg, err := s.PostMessage(ctx, user, ch, "@ai:helper こんにちは")
	if err != nil {
		t.Fatalf("single AI mention should succeed, got: %v", err)
	}

	// Multiple AI mentions in PostMessage must be rejected
	_, err = s.PostMessage(ctx, user, ch, "@ai:helper @ai:secondbot こんにちは")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for multiple AI mentions in PostMessage, got: %v", err)
	}

	// Single AI mention in PostThreadMessage is accepted
	_, err = s.PostThreadMessage(ctx, user, ch, msg.Seq, "@ai:helper 返信です")
	if err != nil {
		t.Fatalf("single AI mention in thread should succeed, got: %v", err)
	}

	// Multiple AI mentions in PostThreadMessage must be rejected
	_, err = s.PostThreadMessage(ctx, user, ch, msg.Seq, "@ai:helper @ai:secondbot 返信です")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for multiple AI mentions in PostThreadMessage, got: %v", err)
	}
}



func TestDirectAICallSlidingWindow(t *testing.T) {
	s, ctx, user, _ := setupTestService(t)

	var receivedMessages []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []map[string]any `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		receivedMessages = req.Messages

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": "AI reply",
					},
				},
			},
		})
	}))
	defer server.Close()

	account := domain.AIAccount{
		ID:      "window-ai",
		Name:    "Window AI",
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

	s.SetAICanvasOptions(true, 0)

	// Send 15 messages (more than sliding window limit of 10)
	var inputMsgs []domain.AIDirectMessage
	for i := 1; i <= 15; i++ {
		inputMsgs = append(inputMsgs, domain.AIDirectMessage{
			Role:    "user",
			Content: fmt.Sprintf("Message %d", i),
		})
	}

	resp, err := s.DirectAICall(ctx, user, domain.AIDirectRequest{
		AIAccountID: "window-ai",
		Messages:    inputMsgs,
	})
	if err != nil {
		t.Fatalf("DirectAICall failed: %v", err)
	}
	if resp.Reply != "AI reply" {
		t.Fatalf("unexpected reply: %s", resp.Reply)
	}

	if len(receivedMessages) != 10 {
		t.Fatalf("expected 10 messages received by LLM, got %d", len(receivedMessages))
	}
	// Verify that received messages are Message 6 through Message 15
	firstReceived := receivedMessages[0]["content"]
	if firstReceived != "Message 6" {
		t.Fatalf("expected first received message to be 'Message 6', got '%v'", firstReceived)
	}
	lastReceived := receivedMessages[9]["content"]
	if lastReceived != "Message 15" {
		t.Fatalf("expected last received message to be 'Message 15', got '%v'", lastReceived)
	}
}

func TestDirectAICallUpstreamErrors(t *testing.T) {
	s, ctx, user, _ := setupTestService(t)

	writeReply := func(w http.ResponseWriter, content string) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": content}}},
		})
	}
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/long":
			writeReply(w, strings.Repeat("あ", 10000)) // 30,000 bytes: over MaxMessageBytes
		case "/huge":
			writeReply(w, strings.Repeat("a", aiMaxDirectReplyBytes+1))
		case "/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {}\n\n")
		case "/status":
			http.Error(w, "upstream failure", http.StatusInternalServerError)
		case "/format":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"answer":"no choices"}`)
		case "/empty":
			writeReply(w, "  ")
		case "/slow":
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}
	}))
	defer server.Close()
	defer close(release)

	cases := []struct {
		id, path, timeout string
		want              error
	}{
		{"long-ai", "/long", "", nil},
		{"huge-ai", "/huge", "", ErrAIReplyTooLarge},
		{"stream-ai", "/stream", "", ErrAIUpstream},
		{"status-ai", "/status", "", ErrAIUpstream},
		{"format-ai", "/format", "", ErrAIUpstream},
		{"empty-ai", "/empty", "", ErrAIUpstream},
		{"slow-ai", "/slow", "50ms", ErrAITimeout},
	}
	for _, c := range cases {
		account := domain.AIAccount{ID: c.id, Name: c.id, Enabled: true, URL: server.URL + c.path, Model: "test-model", Timeout: c.timeout}
		if err := s.storage.SaveAIAccount(ctx, &account); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.StartAI(ctx, nil); err != nil {
		t.Fatal(err)
	}
	defer s.CloseAI()
	s.SetAICanvasOptions(true, 0)

	if got := s.DirectAITimeout("slow-ai"); got != 50*time.Millisecond {
		t.Fatalf("DirectAITimeout(slow-ai) = %v", got)
	}
	if got := s.DirectAITimeout("missing"); got != 120*time.Second {
		t.Fatalf("DirectAITimeout(missing) = %v", got)
	}

	for _, c := range cases {
		resp, err := s.DirectAICall(ctx, user, domain.AIDirectRequest{
			AIAccountID: c.id,
			Messages:    []domain.AIDirectMessage{{Role: "user", Content: "hello"}},
		})
		if c.want == nil {
			if err != nil || len(resp.Reply) != 30000 {
				t.Fatalf("%s: expected long reply, got err=%v", c.id, err)
			}
			continue
		}
		if !errors.Is(err, c.want) {
			t.Fatalf("%s: expected %v, got %v", c.id, c.want, err)
		}
		if strings.Contains(err.Error(), server.URL) {
			t.Fatalf("%s: error leaks upstream URL: %v", c.id, err)
		}
	}
}

func TestParseAIReplyDiagnostics(t *testing.T) {
	cases := []struct {
		name, body     string
		ok, incomplete bool
		reasoning      int
	}{
		{"answer", `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`, true, false, 0},
		{"length", `{"choices":[{"message":{"content":""},"finish_reason":"length"}],"usage":{"prompt_tokens":10,"completion_tokens":20}}`, true, true, 0},
		{"reasoning_content", `{"choices":[{"message":{"content":"","reasoning_content":"考え中"},"finish_reason":"stop"}]}`, true, true, len("考え中")},
		{"reasoning", `{"choices":[{"message":{"content":"","reasoning":"think"}}]}`, true, true, 5},
		{"reasoning object", `{"choices":[{"message":{"content":"","reasoning":{"text":"x"}}}]}`, true, true, len(`{"text":"x"}`)},
		{"null reasoning", `{"choices":[{"message":{"content":" ","reasoning_content":null},"finish_reason":"stop"}]}`, true, false, 0},
		{"no content", `{"choices":[{"message":{"reasoning_content":"x"}}]}`, false, false, 0},
	}
	for _, c := range cases {
		_, diag, ok := parseAIReply([]byte(c.body))
		if ok != c.ok || (ok && (diag.incomplete() != c.incomplete || diag.reasoningBytes != c.reasoning)) {
			t.Fatalf("%s: ok=%v diag=%+v", c.name, ok, diag)
		}
	}
	_, diag, _ := parseAIReply([]byte(`{"choices":[{"message":{"content":""},"finish_reason":"length"}],"usage":{"prompt_tokens":10,"completion_tokens":20}}`))
	if diag.finishReason != "length" || diag.promptTokens != 10 || diag.completionTokens != 20 {
		t.Fatalf("usage not parsed: %+v", diag)
	}
}

func TestDirectAICallEmptyReplyDiagnostics(t *testing.T) {
	s, ctx, user, _ := setupTestService(t)
	const secret = "秘密の思考内容"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/length":
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"`+secret+`"},"finish_reason":"length"}],"usage":{"prompt_tokens":900,"completion_tokens":100}}`)
		case "/reasoning":
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"\n","reasoning":"`+secret+`"},"finish_reason":"stop"}]}`)
		case "/blank":
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"  "},"finish_reason":"stop"}]}`)
		}
	}))
	defer server.Close()

	cases := []struct {
		id, path, finish string
		want             error
	}{
		{"length-ai", "/length", "length", ErrAIReplyIncomplete},
		{"reasoning-ai", "/reasoning", "stop", ErrAIReplyIncomplete},
		{"blank-ai", "/blank", "stop", ErrAIUpstream},
	}
	for _, c := range cases {
		account := domain.AIAccount{ID: c.id, Name: c.id, Enabled: true, URL: server.URL + c.path, Model: "test-model"}
		if err := s.storage.SaveAIAccount(ctx, &account); err != nil {
			t.Fatal(err)
		}
	}
	var logs bytes.Buffer
	if err := s.StartAI(ctx, slog.New(slog.NewTextHandler(&logs, nil))); err != nil {
		t.Fatal(err)
	}
	defer s.CloseAI()
	s.SetAICanvasOptions(true, 0)

	const prompt = "利用者の質問本文"
	for _, c := range cases {
		logs.Reset()
		_, err := s.DirectAICall(ctx, user, domain.AIDirectRequest{
			AIAccountID: c.id,
			Messages:    []domain.AIDirectMessage{{Role: "user", Content: prompt}},
		})
		if !errors.Is(err, c.want) {
			t.Fatalf("%s: expected %v, got %v", c.id, c.want, err)
		}
		if c.want == ErrAIUpstream && errors.Is(err, ErrAIReplyIncomplete) {
			t.Fatalf("%s: blank reply reported as incomplete", c.id)
		}
		line := logs.String()
		for _, key := range []string{"AI direct reply empty", "aiId=" + c.id, "finishReason=" + c.finish, "reasoningBytes=", "promptTokens=", "completionTokens=", "requestBytes="} {
			if !strings.Contains(line, key) {
				t.Fatalf("%s: log lacks %q: %s", c.id, key, line)
			}
		}
		if strings.Contains(line, secret) || strings.Contains(line, prompt) {
			t.Fatalf("%s: log leaks message text: %s", c.id, line)
		}
	}
	used, err := s.storage.GetAIDailyUsage(ctx, user, time.Now().Format("2006-01-02"))
	if err != nil || used != 0 {
		t.Fatalf("failed replies counted against quota: used=%d err=%v", used, err)
	}
}

func TestChannelAIEmptyReplyNotice(t *testing.T) {
	s, ctx, user, ch := setupTestService(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"thinking"},"finish_reason":"length"}]}`)
	}))
	defer server.Close()
	account := domain.AIAccount{ID: "helper", Name: "AI", URL: server.URL, Model: "test-model", Enabled: true}
	if err := s.storage.SaveAIAccount(ctx, &account); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	if err := s.StartAI(ctx, slog.New(slog.NewTextHandler(&logs, nil))); err != nil {
		t.Fatal(err)
	}
	defer s.CloseAI()
	trigger, err := s.PostMessage(ctx, user, ch, "question")
	if err != nil {
		t.Fatal(err)
	}
	s.runAI(s.ai, aiJob{account: s.ai.getAccounts()[0], author: user, channel: ch, id: "request", trigger: trigger})

	after := int64(0)
	page, err := s.storage.GetMessages(ctx, ch, storage.MessageQuery{ThreadRootSeq: trigger.Seq, AfterSeq: &after, Limit: 100})
	if err != nil || len(page.Messages) != 1 {
		t.Fatalf("expected one failure notice: %#v %v", page, err)
	}
	m := page.Messages[0]
	if m.AI == nil || m.AI.Kind != "error" || m.Text != aiReplyIncompleteNotice {
		t.Fatalf("unexpected notice: %#v", m)
	}
	if line := logs.String(); !strings.Contains(line, "code=reply_incomplete") || !strings.Contains(line, "finishReason=length") || strings.Contains(line, "thinking") {
		t.Fatalf("unexpected log: %s", line)
	}
}
