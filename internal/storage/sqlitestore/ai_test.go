package sqlitestore

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/masapico/minihub/internal/domain"
)

func TestUpgradeV3AIColumn(t *testing.T) {
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
	if _, err := s.write.Exec("ALTER TABLE messages DROP COLUMN ai; PRAGMA user_version=3"); err != nil {
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
	old, err := s.GetMessage(ctx, "c", m.Seq)
	if err != nil || old.Text != m.Text || old.AI != nil {
		t.Fatalf("old message: %#v %v", old, err)
	}
	answer, err := s.AddMessage(ctx, "c", domain.Message{UserID: "ai_helper", Text: "answer", ThreadRootSeq: m.Seq, AI: &domain.AIMessage{ID: "helper", Name: "AI"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMessage(ctx, "c", answer.Seq)
	if err != nil || got.AI == nil || got.AI.Name != "AI" {
		t.Fatalf("AI message: %#v %v", got, err)
	}
}

func TestAIAccountStorage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	list, err := s.ListAIAccounts(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("initial accounts: %#v %v", list, err)
	}

	temp := 0.7
	maxTokens := 2048
	account := domain.AIAccount{
		ID:          "assistant",
		Name:        "社内アシスタント",
		Enabled:     true,
		URL:         "http://127.0.0.1:8080/v1/chat/completions",
		Model:       "local-model",
		Groups:      []string{"dev", "sales"},
		Temperature: &temp,
		MaxTokens:   &maxTokens,
	}
	if err := s.SaveAIAccount(ctx, &account); err != nil {
		t.Fatalf("save account failed: %v", err)
	}

	got, err := s.GetAIAccount(ctx, "assistant")
	if err != nil {
		t.Fatalf("get account failed: %v", err)
	}
	if got.Name != account.Name || got.Model != account.Model || len(got.Groups) != 2 || *got.Temperature != 0.7 || *got.MaxTokens != 2048 {
		t.Fatalf("unexpected account: %#v", got)
	}

	list, err = s.ListAIAccounts(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list accounts failed: %#v %v", list, err)
	}

	if err := s.DeleteAIAccount(ctx, "assistant"); err != nil {
		t.Fatalf("delete account failed: %v", err)
	}
	_, err = s.GetAIAccount(ctx, "assistant")
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}
