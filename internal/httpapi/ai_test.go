package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestAICanvasEndpoints(t *testing.T) {
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
	svc.SetAICanvasOptions(true, 1)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := svc.StartAI(ctx, logger); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.CloseAI)
	handler := httpapi.New(svc, httpapi.HeaderAuthenticator{}, logger)

	// 1. Quota endpoint
	qRespAnon := request(t, handler, http.MethodGet, "/api/ai/quota", "", nil)
	if qRespAnon.Code != 401 {
		t.Fatalf("anon quota code: %d", qRespAnon.Code)
	}
	qResp := request(t, handler, http.MethodGet, "/api/ai/quota", "user", nil)
	if qResp.Code != 200 || !strings.Contains(qResp.Body.String(), `"dailyQuota":1`) {
		t.Fatalf("user quota: %d %s", qResp.Code, qResp.Body.String())
	}

	// 2. Admin usage summary endpoint
	uRespForbidden := request(t, handler, http.MethodGet, "/api/admin/ai/usage", "user", nil)
	if uRespForbidden.Code != 403 {
		t.Fatalf("user accessing admin usage: %d", uRespForbidden.Code)
	}
	uRespAdmin := request(t, handler, http.MethodGet, "/api/admin/ai/usage", "admin", nil)
	if uRespAdmin.Code != 200 || !strings.Contains(uRespAdmin.Body.String(), `"totalCount":0`) {
		t.Fatalf("admin usage: %d %s", uRespAdmin.Code, uRespAdmin.Body.String())
	}

	// 3. Direct AI endpoint validation
	dMethod := request(t, handler, http.MethodGet, "/api/ai/direct", "user", nil)
	if dMethod.Code != 405 {
		t.Fatalf("GET /api/ai/direct expected 405, got %d", dMethod.Code)
	}

	dMissing := request(t, handler, http.MethodPost, "/api/ai/direct", "user", map[string]any{
		"aiAccountId": "non-existent",
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	})
	if dMissing.Code != 404 {
		t.Fatalf("missing AI expected 404, got %d %s", dMissing.Code, dMissing.Body.String())
	}

	// 4. Invalid JSON body
	dInvalidJSON := request(t, handler, http.MethodPost, "/api/ai/direct", "user", "[object Object]")
	if dInvalidJSON.Code != 400 || !strings.Contains(dInvalidJSON.Body.String(), "invalid JSON body") {
		t.Fatalf("invalid JSON expected 400 with details, got %d %s", dInvalidJSON.Code, dInvalidJSON.Body.String())
	}
}


func TestAIDirectUpstreamStatusAndWriteDeadline(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply := func(content string) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": content}}},
			})
		}
		switch r.URL.Path {
		case "/delayed":
			time.Sleep(300 * time.Millisecond)
			reply("delayed answer")
		case "/long":
			reply(strings.Repeat("あ", 10000))
		case "/huge":
			reply(strings.Repeat("a", 300*1024))
		case "/stream":
			w.Header().Set("Content-Type", "text/event-stream")
		case "/incomplete":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":""},"finish_reason":"length"}]}`)
		case "/slow":
			select {
			case <-time.After(time.Second):
			case <-r.Context().Done():
			}
		}
	}))
	defer upstream.Close()

	store, err := storetest.New(t, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.SaveUser(ctx, &domain.User{ID: "user", Name: "User", Enabled: true, Role: domain.RoleUser}); err != nil {
		t.Fatal(err)
	}
	accounts := map[string]string{"delayed-ai": "", "long-ai": "", "huge-ai": "", "stream-ai": "", "incomplete-ai": "", "slow-ai": "50ms"}
	for id, timeout := range accounts {
		path := "/" + strings.TrimSuffix(id, "-ai")
		if err := store.SaveAIAccount(ctx, &domain.AIAccount{ID: id, Name: id, URL: upstream.URL + path, Model: "m", Timeout: timeout, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	svc := service.New(store)
	svc.SetAICanvasOptions(true, 0)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := svc.StartAI(ctx, logger); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.CloseAI)
	handler := httpapi.New(svc, httpapi.HeaderAuthenticator{}, logger)

	body := func(id string) map[string]any {
		return map[string]any{"aiAccountId": id, "messages": []map[string]string{{"role": "user", "content": "hi"}}}
	}
	for _, c := range []struct {
		id     string
		status int
		text   string
	}{
		{"long-ai", http.StatusOK, `"reply":"あ`},
		{"huge-ai", http.StatusBadGateway, "長すぎる"},
		{"stream-ai", http.StatusBadGateway, "正常な応答"},
		{"incomplete-ai", http.StatusBadGateway, "最大トークン数"},
		{"slow-ai", http.StatusGatewayTimeout, "時間内"},
	} {
		got := request(t, handler, http.MethodPost, "/api/ai/direct", "user", body(c.id))
		if got.Code != c.status || !strings.Contains(got.Body.String(), c.text) {
			t.Fatalf("%s: got %d %s", c.id, got.Code, got.Body.String())
		}
		if strings.Contains(got.Body.String(), upstream.URL) {
			t.Fatalf("%s: response leaks upstream URL", c.id)
		}
	}

	// An AI reply slower than the server WriteTimeout must still reach the client.
	server := httptest.NewUnstartedServer(handler)
	server.Config.WriteTimeout = 100 * time.Millisecond
	server.Start()
	defer server.Close()
	raw, _ := json.Marshal(body("delayed-ai"))
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/ai/direct", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-User-ID", "user")
	req.Header.Set("Content-Type", "application/json")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("delayed reply was cut off by WriteTimeout: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), "delayed answer") {
		t.Fatalf("delayed reply: %d %s", resp.StatusCode, b)
	}
}
