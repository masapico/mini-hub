package service

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/masapico/minihub/internal/config"
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
		AIAccount: config.AIAccount{
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
		AIAccount: config.AIAccount{
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
