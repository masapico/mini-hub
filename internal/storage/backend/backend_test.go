package backend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/masapico/minihub/internal/auth"
	"github.com/masapico/minihub/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

func TestSelectionAndLocking(t *testing.T) {
	root := t.TempDir()
	h, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := Open(root); err == nil {
		other.Close()
		t.Fatal("double open succeeded")
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h2, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := h2.Close(); err != nil {
		t.Fatal(err)
	}

	empty := t.TempDir()
	h3, err := Open(empty)
	if err != nil {
		t.Fatal(err)
	}
	if err := h3.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(empty, DBName)); err != nil {
		t.Fatal(err)
	}
	if h4, err := Open(empty); err == nil {
		h4.Close()
		t.Fatal("missing DB unexpectedly reopened")
	}
}

func TestUnmarkedDBRejection(t *testing.T) {
	root := t.TempDir()
	h, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, markerName)); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root); err == nil {
		t.Fatal("unmarked SQLite database accepted")
	}
}

func TestSQLiteBackupRestoreAndAuth(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	h, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	user := &domain.User{
		Version:        domain.Version,
		ID:             "u",
		Name:           "User U",
		Enabled:        true,
		PasswordHash:   string(hash),
		AuthGeneration: 1,
	}
	if err := h.SaveUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	channel := &domain.Channel{
		Version: domain.Version,
		ID:      "c",
		Name:    "Channel C",
		Type:    domain.ChannelPublic,
	}
	if err := h.SaveChannel(ctx, channel); err != nil {
		t.Fatal(err)
	}
	msg, err := h.AddMessage(ctx, "c", domain.Message{UserID: "u", Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}

	backup := t.TempDir()
	if err := os.CopyFS(backup, os.DirFS(root)); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()

	gotUser, err := restored.GetUser(ctx, "u")
	if err != nil || gotUser.Name != "User U" {
		t.Fatalf("restored user: %+v err: %v", gotUser, err)
	}
	gotMsg, err := restored.GetMessage(ctx, "c", msg.Seq)
	if err != nil || gotMsg.Text != "hello" {
		t.Fatalf("restored msg: %+v err: %v", gotMsg, err)
	}

	mgr := auth.NewManager(restored, false)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	_, csrf, err := mgr.Login(rec, req, "u", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	cookie := rec.Result().Cookies()[0]
	r := httptest.NewRequest(http.MethodPost, "/api/channels", nil)
	r.AddCookie(cookie)
	r.Header.Set("X-CSRF-Token", csrf)
	id, err := mgr.Authenticate(r)
	if err != nil || id != "u" || !mgr.ValidateCSRF(r) {
		t.Fatalf("authenticated id=%q err=%v", id, err)
	}
}
