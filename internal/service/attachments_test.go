package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/masapico/minihub/internal/domain"
	"github.com/masapico/minihub/internal/storage/storetest"
)

func setupTestService(t *testing.T) (*Service, context.Context, string, string) {
	t.Helper()
	ctx := context.Background()
	store, err := storetest.New(t, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []domain.User{
		{ID: "admin", Name: "Admin", Role: domain.RoleAdmin, Enabled: true},
		{ID: "alice", Name: "Alice", Role: domain.RoleUser, Enabled: true},
	} {
		if err := store.SaveUser(ctx, &u); err != nil {
			t.Fatal(err)
		}
	}
	s := New(store)
	ch, err := s.CreateChannel(ctx, "admin", domain.Channel{
		ID:      "general",
		Name:    "General",
		Type:    domain.ChannelPublic,
		Members: []string{"alice"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, ctx, "alice", ch.ID
}

func TestAttachmentExtensionValidation(t *testing.T) {
	s, ctx, user, ch := setupTestService(t)

	// Valid extensions
	for _, validName := range []string{"data.xlsx", "report.DOCX", "slides.pptx", "doc.pdf", "text.txt", "table.csv", "data.json", "notes.md"} {
		content := strings.NewReader("sample content")
		summary, err := s.StageAttachment(ctx, user, ch, validName, content)
		if err != nil {
			t.Fatalf("expected %s to be allowed, got error: %v", validName, err)
		}
		if summary.ID == "" || summary.Filename != sanitizeFilename(validName) {
			t.Fatalf("unexpected summary for %s: %+v", validName, summary)
		}
	}

	// Invalid extensions
	for _, invalidName := range []string{"virus.exe", "script.sh", "page.html", "photo.png", "audio.mp3"} {
		content := strings.NewReader("bad content")
		_, err := s.StageAttachment(ctx, user, ch, invalidName, content)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("expected %s to be rejected with ErrInvalid, got: %v", invalidName, err)
		}
	}
}

func TestAttachmentRequireAIMention(t *testing.T) {
	s, ctx, user, ch := setupTestService(t)

	// Stage an attachment
	summary, err := s.StageAttachment(ctx, user, ch, "report.xlsx", strings.NewReader("excel data"))
	if err != nil {
		t.Fatal(err)
	}

	// 1. Post message with attachment but WITHOUT @ai: mention -> MUST fail
	_, err = s.PostMessage(ctx, user, ch, "Here is the report without AI mention", summary.ID)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid when posting attachment without @ai: mention, got: %v", err)
	}

	// 2. Post thread message with attachment but WITHOUT @ai: mention -> MUST fail
	root, err := s.PostMessage(ctx, user, ch, "thread root")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.PostThreadMessage(ctx, user, ch, root.Seq, "reply without AI mention", summary.ID)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid when posting thread attachment without @ai: mention, got: %v", err)
	}

	// 3. Post message with attachment WITH @ai: mention -> MUST succeed
	msg, err := s.PostMessage(ctx, user, ch, "@ai:helper analyze this report", summary.ID)
	if err != nil {
		t.Fatalf("expected post with @ai: mention to succeed, got: %v", err)
	}
	if len(msg.Attachments) != 1 || msg.Attachments[0].ID != summary.ID {
		t.Fatalf("expected message to have attachment, got: %+v", msg.Attachments)
	}
	if msg.Attachments[0].Path != "" {
		t.Fatalf("expected Path to be empty (no host file storage), got: %s", msg.Attachments[0].Path)
	}

	// Staged attachment should be consumed; second use must fail
	_, err = s.PostMessage(ctx, user, ch, "@ai:helper analyze again", summary.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for already consumed attachment, got: %v", err)
	}
}
