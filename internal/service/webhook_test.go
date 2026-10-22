package service_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/masapico/minihub/internal/domain"
	"github.com/masapico/minihub/internal/service"
	"github.com/masapico/minihub/internal/storage/sqlitestore"
)

func setupWebhookTestService(t *testing.T) (*service.Service, *sqlitestore.Store) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := sqlitestore.Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	ctx := context.Background()
	admin := domain.User{ID: "admin", Role: domain.RoleAdmin, Enabled: true}
	if err := store.SaveUser(ctx, &admin); err != nil {
		t.Fatalf("SaveUser(admin): %v", err)
	}
	user := domain.User{ID: "alice", Role: domain.RoleUser, Enabled: true}
	if err := store.SaveUser(ctx, &user); err != nil {
		t.Fatalf("SaveUser(user): %v", err)
	}
	ch := domain.Channel{ID: "general", Name: "General", Type: domain.ChannelPublic}
	if err := store.SaveChannel(ctx, &ch); err != nil {
		t.Fatalf("SaveChannel: %v", err)
	}

	svc := service.New(store)
	return svc, store
}

func TestWebhookAdminCRUD(t *testing.T) {
	svc, store := setupWebhookTestService(t)
	defer store.Close()
	ctx := context.Background()

	// 非管理者は拒否
	_, err := svc.CreateWebhook(ctx, "alice", service.CreateWebhookInput{
		Name:      "Bot",
		ChannelID: "general",
	})
	if err != service.ErrForbidden {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}

	// 管理者による作成
	created, err := svc.CreateWebhook(ctx, "admin", service.CreateWebhookInput{
		Name:        "RPA Bot",
		ChannelID:   "general",
		Description: "RPA notifications",
	})
	if err != nil {
		t.Fatalf("CreateWebhook failed: %v", err)
	}
	if created.RawToken == "" || created.Webhook.ID == "" || created.Webhook.Name != "RPA Bot" {
		t.Fatalf("invalid created result: %#v", created)
	}
	if created.Webhook.TokenHash != "" {
		t.Fatalf("TokenHash must not be exposed in result")
	}

	// 一覧取得
	list, err := svc.ListAdminWebhooks(ctx, "admin")
	if err != nil || len(list) != 1 {
		t.Fatalf("ListAdminWebhooks: %#v %v", list, err)
	}

	// 更新
	updated, err := svc.UpdateWebhook(ctx, "admin", created.Webhook.ID, service.UpdateWebhookInput{
		Name:        "Renamed Bot",
		ChannelID:   "general",
		Description: "Updated desc",
		Enabled:     true,
	})
	if err != nil || updated.Name != "Renamed Bot" {
		t.Fatalf("UpdateWebhook failed: %#v %v", updated, err)
	}

	// トークン再生成
	regen, err := svc.RegenerateWebhookToken(ctx, "admin", created.Webhook.ID)
	if err != nil || regen.RawToken == "" || regen.RawToken == created.RawToken {
		t.Fatalf("RegenerateWebhookToken failed: %#v %v", regen, err)
	}

	// 削除
	if err := svc.DeleteWebhook(ctx, "admin", created.Webhook.ID); err != nil {
		t.Fatalf("DeleteWebhook failed: %v", err)
	}
	_, err = svc.GetAdminWebhook(ctx, "admin", created.Webhook.ID)
	if err != service.ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestPostWebhookMessage(t *testing.T) {
	svc, store := setupWebhookTestService(t)
	defer store.Close()
	ctx := context.Background()

	created, err := svc.CreateWebhook(ctx, "admin", service.CreateWebhookInput{
		Name:      "Monitor Bot",
		ChannelID: "general",
	})
	if err != nil {
		t.Fatalf("CreateWebhook failed: %v", err)
	}

	// 1. 正常な投稿（本文）
	msg, err := svc.PostWebhookMessage(ctx, created.Webhook.ID, created.RawToken, "Server OK", 0)
	if err != nil {
		t.Fatalf("PostWebhookMessage failed: %v", err)
	}
	if msg.Seq != 1 || msg.Text != "Server OK" || msg.Webhook == nil || msg.Webhook.Name != "Monitor Bot" {
		t.Fatalf("unexpected message: %#v", msg)
	}

	// 2. スレッド返信
	threadMsg, err := svc.PostWebhookMessage(ctx, created.Webhook.ID, created.RawToken, "Thread reply", msg.Seq)
	if err != nil {
		t.Fatalf("PostWebhookMessage thread reply failed: %v", err)
	}
	if threadMsg.ThreadRootSeq != msg.Seq || threadMsg.Text != "Thread reply" {
		t.Fatalf("unexpected thread message: %#v", threadMsg)
	}

	// 3. 不正トークンでの投稿
	_, err = svc.PostWebhookMessage(ctx, created.Webhook.ID, "invalid_token", "Hello", 0)
	if err != service.ErrUnauthenticated {
		t.Fatalf("expected ErrUnauthenticated, got %v", err)
	}

	// 4. 無効化されたWebhook
	_, err = svc.UpdateWebhook(ctx, "admin", created.Webhook.ID, service.UpdateWebhookInput{
		Name:      "Monitor Bot",
		ChannelID: "general",
		Enabled:   false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.PostWebhookMessage(ctx, created.Webhook.ID, created.RawToken, "Hello", 0)
	if err != service.ErrForbidden {
		t.Fatalf("expected ErrForbidden for disabled webhook, got %v", err)
	}

	// 有効に戻す
	_, _ = svc.UpdateWebhook(ctx, "admin", created.Webhook.ID, service.UpdateWebhookInput{
		Name:      "Monitor Bot",
		ChannelID: "general",
		Enabled:   true,
	})

	// 5. アーカイブ済みチャンネル
	chToArchive := domain.Channel{ID: "will_archive", Name: "Will Archive", Type: domain.ChannelPublic}
	_ = store.SaveChannel(ctx, &chToArchive)
	archivedWH, err := svc.CreateWebhook(ctx, "admin", service.CreateWebhookInput{
		Name:      "Archived Bot",
		ChannelID: "will_archive",
	})
	if err != nil {
		t.Fatalf("CreateWebhook before archive failed: %v", err)
	}

	// チャンネルをアーカイブする
	archivedAt := time.Now()
	chToArchive.ArchivedAt = &archivedAt
	_ = store.SaveChannel(ctx, &chToArchive)

	// チャンネルアーカイブ状態での投稿は拒否される
	_, err = svc.PostWebhookMessage(ctx, archivedWH.Webhook.ID, archivedWH.RawToken, "Fail", 0)
	if err != service.ErrForbidden {
		t.Fatalf("expected ErrForbidden for archived channel, got %v", err)
	}
}

func TestWebhookRateLimiter(t *testing.T) {
	svc, store := setupWebhookTestService(t)
	defer store.Close()
	ctx := context.Background()

	created, err := svc.CreateWebhook(ctx, "admin", service.CreateWebhookInput{
		Name:      "Spam Bot",
		ChannelID: "general",
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	// 1秒間に5回までは成功
	for i := 0; i < 5; i++ {
		_, err := svc.PostWebhookMessage(ctx, created.Webhook.ID, created.RawToken, "msg", 0)
		if err != nil {
			t.Fatalf("call %d failed unexpectedly: %v", i+1, err)
		}
	}
	// 6回目はレート制限される
	_, err = svc.PostWebhookMessage(ctx, created.Webhook.ID, created.RawToken, "msg", 0)
	if err != service.ErrRateLimited {
		t.Fatalf("expected ErrRateLimited on 6th burst call, got %v", err)
	}
}
