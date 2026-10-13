package httpapi_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/masapico/minihub/internal/domain"
	"github.com/masapico/minihub/internal/httpapi"
	"github.com/masapico/minihub/internal/service"
	"github.com/masapico/minihub/internal/storage/storetest"
)

func TestAIAccountsHTTPDirectory(t *testing.T) {
	store, err := storetest.New(t, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.SaveUser(ctx, &domain.User{ID: "alice", Name: "Alice", Enabled: true, Role: domain.RoleUser, Groups: []string{"general"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveUser(ctx, &domain.User{ID: "bob", Name: "Bob", Enabled: true, Role: domain.RoleUser, Groups: []string{"sales"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveUser(ctx, &domain.User{ID: "admin", Name: "Admin", Enabled: true, Role: domain.RoleAdmin}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MINIHUB_AI_DIRECTORY_TEST", "private-token")
	if err := store.SaveAIAccount(ctx, &domain.AIAccount{ID: "helper", Name: "社内AI", URL: "http://private-ai-host/secret-route", Model: "private-model", Token: "direct-private-token", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAIAccount(ctx, &domain.AIAccount{ID: "sales-bot", Name: "営業Bot", URL: "http://private-ai-host/secret-route", Model: "test-model", TokenEnv: "MINIHUB_AI_DIRECTORY_TEST", Groups: []string{"sales"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAIAccount(ctx, &domain.AIAccount{ID: "disabled", Name: "停止AI", URL: "http://private-ai-host/", Model: "test-model", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	svc := service.New(store)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := svc.StartAI(ctx, logger); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.CloseAI)
	handler := httpapi.New(svc, httpapi.HeaderAuthenticator{}, logger)

	// alice cannot see sales-bot because she is not in sales group
	got := request(t, handler, http.MethodGet, "/api/ai-accounts", "alice", nil)
	if got.Code != 200 || got.Body.String() != "[{\"id\":\"helper\",\"name\":\"社内AI\"}]\n" {
		t.Fatalf("alice directory: %d %s", got.Code, got.Body.String())
	}

	// bob can see helper (available to all) and sales-bot (his group)
	gotBob := request(t, handler, http.MethodGet, "/api/ai-accounts", "bob", nil)
	if gotBob.Code != 200 || gotBob.Body.String() != "[{\"id\":\"helper\",\"name\":\"社内AI\"},{\"id\":\"sales-bot\",\"name\":\"営業Bot\"}]\n" {
		t.Fatalf("bob directory: %d %s", gotBob.Code, gotBob.Body.String())
	}

	// admin can see helper and sales-bot
	gotAdmin := request(t, handler, http.MethodGet, "/api/ai-accounts", "admin", nil)
	if gotAdmin.Code != 200 || gotAdmin.Body.String() != "[{\"id\":\"helper\",\"name\":\"社内AI\"},{\"id\":\"sales-bot\",\"name\":\"営業Bot\"}]\n" {
		t.Fatalf("admin directory: %d %s", gotAdmin.Code, gotAdmin.Body.String())
	}

	for _, secret := range []string{"private-token", "private-ai-host", "secret-route", "private-model", "token", "disabled"} {
		if strings.Contains(got.Body.String(), secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
	if r := request(t, handler, http.MethodGet, "/api/ai-accounts", "", nil); r.Code != 401 {
		t.Fatalf("anonymous directory: %d", r.Code)
	}
	if r := request(t, handler, http.MethodGet, "/api/ai-accounts", "missing", nil); r.Code != 401 {
		t.Fatalf("unknown user directory: %d", r.Code)
	}
	if r := request(t, handler, http.MethodPost, "/api/ai-accounts", "alice", nil); r.Code != 405 {
		t.Fatalf("method: %d", r.Code)
	}
}

func TestAIAccountsAdminEndpoints(t *testing.T) {
	store, err := storetest.New(t, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.SaveUser(ctx, &domain.User{ID: "admin", Name: "Admin", Enabled: true, Role: domain.RoleAdmin}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveUser(ctx, &domain.User{ID: "user", Name: "User", Enabled: true, Role: domain.RoleUser}); err != nil {
		t.Fatal(err)
	}
	svc := service.New(store)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := svc.StartAI(ctx, logger); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.CloseAI)
	handler := httpapi.New(svc, httpapi.HeaderAuthenticator{}, logger)

	// Non-admin rejected
	if r := request(t, handler, http.MethodGet, "/api/admin/ai-accounts", "user", nil); r.Code != 403 {
		t.Fatalf("expected 403 for non-admin GET, got %d", r.Code)
	}
	if r := request(t, handler, http.MethodPost, "/api/admin/ai-accounts", "user", map[string]any{"id": "test"}); r.Code != 403 {
		t.Fatalf("expected 403 for non-admin POST, got %d", r.Code)
	}

	// Admin Create
	createPayload := map[string]any{
		"id":          "new-bot",
		"name":        "New Bot",
		"enabled":     true,
		"url":         "http://127.0.0.1:8080/v1/chat/completions",
		"model":       "test-model",
		"token":       "secret-key-123",
		"groups":      []string{"g1"},
		"temperature": 0.8,
		"maxTokens":   500,
	}
	createResp := request(t, handler, http.MethodPost, "/api/admin/ai-accounts", "admin", createPayload)
	if createResp.Code != 201 {
		t.Fatalf("create: %d %s", createResp.Code, createResp.Body.String())
	}
	if strings.Contains(createResp.Body.String(), "secret-key-123") {
		t.Fatal("token should not be returned in cleartext")
	}

	// Admin List
	listResp := request(t, handler, http.MethodGet, "/api/admin/ai-accounts", "admin", nil)
	if listResp.Code != 200 || !strings.Contains(listResp.Body.String(), `"id":"new-bot"`) {
		t.Fatalf("list: %d %s", listResp.Code, listResp.Body.String())
	}

	// Admin Get
	getResp := request(t, handler, http.MethodGet, "/api/admin/ai-accounts/new-bot", "admin", nil)
	if getResp.Code != 200 || !strings.Contains(getResp.Body.String(), `"tokenConfigured":true`) {
		t.Fatalf("get: %d %s", getResp.Code, getResp.Body.String())
	}

	// Admin Update without modifying token
	updatePayload := map[string]any{
		"name":        "Updated Bot",
		"enabled":     true,
		"url":         "http://127.0.0.1:8080/v1/chat/completions",
		"model":       "test-model-v2",
		"temperature": 0.5,
	}
	updateResp := request(t, handler, http.MethodPut, "/api/admin/ai-accounts/new-bot", "admin", updatePayload)
	if updateResp.Code != 200 || !strings.Contains(updateResp.Body.String(), `"name":"Updated Bot"`) {
		t.Fatalf("update: %d %s", updateResp.Code, updateResp.Body.String())
	}
	// Verify token was preserved in storage
	stored, err := store.GetAIAccount(ctx, "new-bot")
	if err != nil || stored.Token != "secret-key-123" {
		t.Fatalf("token not preserved: %#v", stored)
	}

	// Admin Delete
	delResp := request(t, handler, http.MethodDelete, "/api/admin/ai-accounts/new-bot", "admin", nil)
	if delResp.Code != 204 {
		t.Fatalf("delete: %d", delResp.Code)
	}
	if _, err := store.GetAIAccount(ctx, "new-bot"); err == nil {
		t.Fatal("expected account to be deleted from store")
	}
}
