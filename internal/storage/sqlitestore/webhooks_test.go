package sqlitestore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/masapico/minihub/internal/domain"
)

func TestUpgradeV7Webhook(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	m, err := s.AddMessage(ctx, "c", domain.Message{UserID: "u", Text: "old message"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.write.Exec("ALTER TABLE messages DROP COLUMN webhook; DROP TABLE IF EXISTS webhooks; PRAGMA user_version=7"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	var ver int
	if err := s.read.QueryRowContext(ctx, "PRAGMA user_version").Scan(&ver); err != nil || ver != 8 {
		t.Fatalf("expected version 8, got %d (%v)", ver, err)
	}

	old, err := s.GetMessage(ctx, "c", m.Seq)
	if err != nil || old.Text != m.Text || old.Webhook != nil {
		t.Fatalf("old message: %#v %v", old, err)
	}
	whMsg, err := s.AddMessage(ctx, "c", domain.Message{
		UserID: "whk_bot1",
		Text:   "webhook notification",
		Webhook: &domain.WebhookMessageInfo{
			ID:   "whk_bot1",
			Name: "RPA Bot",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMessage(ctx, "c", whMsg.Seq)
	if err != nil || got.Webhook == nil || got.Webhook.Name != "RPA Bot" {
		t.Fatalf("webhook message: %#v %v", got, err)
	}
}

func TestWebhookStorage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	list, err := s.ListWebhooks(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("initial webhooks: %#v %v", list, err)
	}

	wh := domain.Webhook{
		ID:          "whk_test1",
		ChannelID:   "general",
		Name:        "Deploy Bot",
		Description: "Production deploy notices",
		TokenHash:   "hash_abc123",
		Enabled:     true,
		CreatedBy:   "admin",
		CreatedAt:   time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := s.SaveWebhook(ctx, &wh); err != nil {
		t.Fatalf("SaveWebhook: %v", err)
	}

	got, err := s.GetWebhook(ctx, "whk_test1")
	if err != nil || got.Name != "Deploy Bot" || got.TokenHash != "hash_abc123" {
		t.Fatalf("GetWebhook: %#v %v", got, err)
	}

	byHash, err := s.GetWebhookByTokenHash(ctx, "hash_abc123")
	if err != nil || byHash.ID != "whk_test1" {
		t.Fatalf("GetWebhookByTokenHash: %#v %v", byHash, err)
	}

	chList, err := s.ListChannelWebhooks(ctx, "general")
	if err != nil || len(chList) != 1 {
		t.Fatalf("ListChannelWebhooks: %#v %v", chList, err)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := s.TouchWebhookUsed(ctx, "whk_test1", now); err != nil {
		t.Fatalf("TouchWebhookUsed: %v", err)
	}
	gotAfterTouch, err := s.GetWebhook(ctx, "whk_test1")
	if err != nil || gotAfterTouch.LastUsedAt == nil {
		t.Fatalf("LastUsedAt not updated: %#v %v", gotAfterTouch, err)
	}

	if err := s.DeleteWebhook(ctx, "whk_test1"); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
	afterDelete, err := s.GetWebhook(ctx, "whk_test1")
	if err == nil || afterDelete != nil {
		t.Fatalf("expected not found, got %#v %v", afterDelete, err)
	}
}

