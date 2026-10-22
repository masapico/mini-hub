package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/masapico/minihub/internal/domain"
	"github.com/masapico/minihub/internal/httpapi"
	"github.com/masapico/minihub/internal/service"
	"github.com/masapico/minihub/internal/storage/storetest"
)

func setupWebhookHTTPTest(t *testing.T) (http.Handler, *service.Service) {
	t.Helper()
	store, err := storetest.New(t, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_ = store.SaveUser(ctx, &domain.User{ID: "admin", Name: "Admin", Enabled: true, Role: domain.RoleAdmin})
	_ = store.SaveUser(ctx, &domain.User{ID: "alice", Name: "Alice", Enabled: true, Role: domain.RoleUser})
	_ = store.SaveChannel(ctx, &domain.Channel{ID: "general", Name: "General", Type: domain.ChannelPublic})

	svc := service.New(store)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := httpapi.New(svc, httpapi.HeaderAuthenticator{}, logger)
	return handler, svc
}

func TestWebhookAdminHTTP(t *testing.T) {
	handler, _ := setupWebhookHTTPTest(t)

	// 非管理者の作成拒否 (403)
	rec := request(t, handler, http.MethodPost, "/api/admin/webhooks", "alice", map[string]any{
		"name":      "Bot",
		"channelId": "general",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}

	// 管理者による作成 (201)
	createRec := request(t, handler, http.MethodPost, "/api/admin/webhooks", "admin", map[string]any{
		"name":        "RPA Bot",
		"channelId":   "general",
		"description": "RPA execution notice",
	})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", createRec.Code, createRec.Body.String())
	}
	var createdRes struct {
		Webhook domain.Webhook `json:"webhook"`
		Token   string         `json:"token"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createdRes); err != nil {
		t.Fatal(err)
	}
	if createdRes.Token == "" || createdRes.Webhook.ID == "" {
		t.Fatalf("empty token or id: %#v", createdRes)
	}

	// 一覧取得 (200)
	listRec := request(t, handler, http.MethodGet, "/api/admin/webhooks", "admin", nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", listRec.Code)
	}
	var list []domain.Webhook
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil || len(list) != 1 {
		t.Fatalf("unexpected list: %#v %v", list, err)
	}

	// 単体取得 (200)
	getRec := request(t, handler, http.MethodGet, "/api/admin/webhooks/"+createdRes.Webhook.ID, "admin", nil)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", getRec.Code)
	}

	// 更新 (200)
	updateRec := request(t, handler, http.MethodPut, "/api/admin/webhooks/"+createdRes.Webhook.ID, "admin", map[string]any{
		"name":        "Updated Bot",
		"channelId":   "general",
		"description": "new desc",
		"enabled":     true,
	})
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d", updateRec.Code)
	}

	// トークン再生成 (200)
	regenRec := request(t, handler, http.MethodPost, "/api/admin/webhooks/"+createdRes.Webhook.ID+"/regenerate", "admin", nil)
	if regenRec.Code != http.StatusOK {
		t.Fatalf("regenerate: expected 200, got %d", regenRec.Code)
	}
	var regenRes struct {
		Webhook domain.Webhook `json:"webhook"`
		Token   string         `json:"token"`
	}
	if err := json.Unmarshal(regenRec.Body.Bytes(), &regenRes); err != nil || regenRes.Token == "" || regenRes.Token == createdRes.Token {
		t.Fatalf("unexpected regen result: %#v", regenRes)
	}

	// 削除 (204)
	delRec := request(t, handler, http.MethodDelete, "/api/admin/webhooks/"+createdRes.Webhook.ID, "admin", nil)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", delRec.Code)
	}
}

func TestIncomingWebhookDelivery(t *testing.T) {
	handler, _ := setupWebhookHTTPTest(t)

	// Webhookを作成
	createRec := request(t, handler, http.MethodPost, "/api/admin/webhooks", "admin", map[string]any{
		"name":      "CI Bot",
		"channelId": "general",
	})
	var createdRes struct {
		Webhook domain.Webhook `json:"webhook"`
		Token   string         `json:"token"`
	}
	_ = json.Unmarshal(createRec.Body.Bytes(), &createdRes)
	whID := createdRes.Webhook.ID
	token := createdRes.Token

	// 1. パス形式 URL での JSON 投稿
	jsonBody := `{"text": "Build #42 started"}`
	req1 := httptest.NewRequest(http.MethodPost, "/api/webhooks/"+whID+"/"+token, strings.NewReader(jsonBody))
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("path JSON post: expected 201, got %d (%s)", rec1.Code, rec1.Body.String())
	}
	var res1 struct {
		ID  string `json:"id"`
		Seq int64  `json:"seq"`
	}
	_ = json.Unmarshal(rec1.Body.Bytes(), &res1)
	if res1.Seq != 1 {
		t.Fatalf("expected seq 1, got %d", res1.Seq)
	}

	// 2. スレッド返信（threadRootSeq 指定）
	replyBody := `{"text": "Build #42 test passed", "threadRootSeq": 1}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/webhooks/"+whID+"/"+token, strings.NewReader(replyBody))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusCreated {
		t.Fatalf("thread reply: expected 201, got %d (%s)", rec2.Code, rec2.Body.String())
	}

	// 3. PlainText 投稿
	plainBody := "Plain text notice"
	req3 := httptest.NewRequest(http.MethodPost, "/api/webhooks/"+whID+"/"+token, strings.NewReader(plainBody))
	req3.Header.Set("Content-Type", "text/plain")
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusCreated {
		t.Fatalf("plain text: expected 201, got %d (%s)", rec3.Code, rec3.Body.String())
	}

	// 4. Slack 互換形式（attachments fallback）
	slackBody := `{"attachments": [{"text": "Slack formatted message"}]}`
	req4 := httptest.NewRequest(http.MethodPost, "/api/webhooks/"+whID+"/"+token, strings.NewReader(slackBody))
	req4.Header.Set("Content-Type", "application/json")
	rec4 := httptest.NewRecorder()
	handler.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusCreated {
		t.Fatalf("slack compat: expected 201, got %d (%s)", rec4.Code, rec4.Body.String())
	}

	// 5. ヘッダー認証形式（Bearer）
	bearerBody := `{"text": "Header auth notice"}`
	req5 := httptest.NewRequest(http.MethodPost, "/api/webhooks/"+whID, strings.NewReader(bearerBody))
	req5.Header.Set("Content-Type", "application/json")
	req5.Header.Set("Authorization", "Bearer "+token)
	rec5 := httptest.NewRecorder()
	handler.ServeHTTP(rec5, req5)
	if rec5.Code != http.StatusCreated {
		t.Fatalf("bearer auth: expected 201, got %d (%s)", rec5.Code, rec5.Body.String())
	}

	// 6. 不正トークン (401)
	req6 := httptest.NewRequest(http.MethodPost, "/api/webhooks/"+whID+"/wrong_token", strings.NewReader(jsonBody))
	req6.Header.Set("Content-Type", "application/json")
	rec6 := httptest.NewRecorder()
	handler.ServeHTTP(rec6, req6)
	if rec6.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token: expected 401, got %d", rec6.Code)
	}
}
