package backend

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/masapico/minihub/internal/auth"
	"github.com/masapico/minihub/internal/domain"
	"github.com/masapico/minihub/internal/storage"
	"github.com/masapico/minihub/internal/storage/sqlitestore"
	"golang.org/x/crypto/bcrypt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestUnrelatedDBIsUntouched(t *testing.T) {
	root := fixture(t)
	path := filepath.Join(root, DBName)
	db, err := sql.Open("sqlite", path)
	must(t, err)
	_, err = db.Exec("CREATE TABLE unrelated(value TEXT); INSERT INTO unrelated VALUES('keep')")
	must(t, err)
	must(t, db.Close())
	before, err := os.ReadFile(path)
	must(t, err)
	if _, err = Migrate(context.Background(), root, false); err == nil {
		t.Fatal("unrelated DB accepted")
	}
	after, err := os.ReadFile(path)
	must(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("unrelated DB modified")
	}
}

func TestCancelledMigrationCanRetry(t *testing.T) {
	root := fixture(t)
	before := sourceHashes(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Migrate(ctx, root, false); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, sourceHashes(t, root)) {
		t.Fatal("source changed")
	}
	_, err := Migrate(context.Background(), root, false)
	must(t, err)
}

func TestWithdrawalSurvivesMigration(t *testing.T) {
	root := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	value := domain.Withdrawal{
		Version:   domain.Version,
		Kind:      "message",
		ChannelID: "c",
		TargetID:  "1",
		ActorID:   "v",
		At:        now,
		Events: []domain.WithdrawalEvent{
			{Action: "withdraw", ActorID: "u", At: now.Add(-2 * time.Minute)},
			{Action: "restore", ActorID: "v", At: now.Add(-1 * time.Minute)},
			{Action: "withdraw", ActorID: "v", At: now},
		},
	}
	must(t, os.MkdirAll(filepath.Join(root, "withdrawals", "c", "message"), 0700))
	wBytes, err := json.Marshal(value)
	must(t, err)
	must(t, os.WriteFile(filepath.Join(root, "withdrawals", "c", "message", "1.json"), wBytes, 0600))
	before, err := os.ReadFile(filepath.Join(root, "withdrawals", "c", "message", "1.json"))
	must(t, err)
	_, err = Migrate(ctx, root, false)
	must(t, err)
	h, err := Open(root, "sqlite")
	must(t, err)
	defer h.Close()
	got, err := h.GetWithdrawal(ctx, "message", "c", "1")
	must(t, err)
	if got.ActorID != "v" || !got.At.Equal(value.At) || len(got.Events) != 3 {
		t.Fatalf("migrated withdrawal=%+v", got)
	}
	after, err := os.ReadFile(filepath.Join(root, "withdrawals", "c", "message", "1.json"))
	must(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("source withdrawal changed")
	}
}

func TestLoginSurvivesMigrationAndRestore(t *testing.T) {
	root := fixture(t)
	ctx := context.Background()
	uPath := filepath.Join(root, "users", "u.json")
	var u domain.User
	b, err := os.ReadFile(uPath)
	must(t, err)
	must(t, json.Unmarshal(b, &u))
	hash, err := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
	must(t, err)
	u.PasswordHash = string(hash)
	b, err = json.Marshal(u)
	must(t, err)
	must(t, os.WriteFile(uPath, b, 0600))

	_, err = Migrate(ctx, root, false)
	must(t, err)
	backup := t.TempDir()
	must(t, os.CopyFS(backup, os.DirFS(root)))
	h, err := Open(backup, "sqlite")
	must(t, err)
	defer h.Close()
	restored := auth.NewManager(h, false)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	request.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	_, csrf, err := restored.Login(rec, request, "u", "test-password")
	must(t, err)
	cookie := rec.Result().Cookies()[0]
	r := httptest.NewRequest(http.MethodPost, "/api/channels", nil)
	r.AddCookie(cookie)
	r.Header.Set("X-CSRF-Token", csrf)
	id, err := restored.Authenticate(r)
	must(t, err)
	if id != "u" || !restored.ValidateCSRF(r) {
		t.Fatal("authentication did not survive")
	}
}

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// 1. groups
	must(t, os.MkdirAll(filepath.Join(root, "groups"), 0700))
	groupJSON, _ := json.Marshal(domain.Group{Version: domain.Version, ID: "g", Name: "Group"})
	must(t, os.WriteFile(filepath.Join(root, "groups", "g.json"), groupJSON, 0600))

	// 2. users
	must(t, os.MkdirAll(filepath.Join(root, "users"), 0700))
	for _, u := range []string{"u", "v"} {
		userJSON, _ := json.Marshal(domain.User{Version: domain.Version, ID: u, Name: u, Groups: []string{"g"}, Enabled: true, PasswordHash: "hash", AuthGeneration: 7})
		must(t, os.WriteFile(filepath.Join(root, "users", u+".json"), userJSON, 0600))
	}

	// 3. channels
	must(t, os.MkdirAll(filepath.Join(root, "channels", "c", "messages"), 0700))
	must(t, os.MkdirAll(filepath.Join(root, "channels", "c", "reactions"), 0700))
	chJSON, _ := json.Marshal(domain.Channel{Version: domain.Version, ID: "c", Name: "Private", Type: domain.ChannelPrivate, Members: []string{"u", "v"}, Managers: []string{"u"}, Groups: []string{"g"}})
	must(t, os.WriteFile(filepath.Join(root, "channels", "c", "meta.json"), chJSON, 0600))

	for i := 0; i < 6; i++ {
		r := int64(0)
		if i%2 != 0 {
			r = 1
		}
		msg := domain.Message{
			Seq:            int64(i + 1),
			ID:             fmt.Sprintf("m%d", i+1),
			UserID:         "u",
			ThreadRootSeq:  r,
			Text:           "日本語 @v @group:g",
			MentionUserIDs: []string{"v"},
			Timestamp:      time.Date(2026, 1, 1+i, 0, 0, 0, 0, time.UTC),
		}
		line, _ := json.Marshal(msg)
		filename := fmt.Sprintf("2026-01-%02d.jsonl", i+1)
		must(t, os.WriteFile(filepath.Join(root, "channels", "c", "messages", filename), append(line, '\n'), 0600))
	}

	var reactionLines []byte
	for i, active := range []bool{true, false, true} {
		rx := domain.ReactionEvent{
			Version:    domain.Version,
			MessageSeq: 1,
			UserID:     "v",
			Key:        "thanks",
			Active:     active,
			Timestamp:  time.Date(2026, 1, 1, 0, i, 0, 0, time.UTC),
		}
		line, _ := json.Marshal(rx)
		reactionLines = append(reactionLines, append(line, '\n')...)
	}
	must(t, os.WriteFile(filepath.Join(root, "channels", "c", "reactions", "2026-01-01.jsonl"), reactionLines, 0600))

	// 4. schedules
	must(t, os.MkdirAll(filepath.Join(root, "schedules", "sch"), 0700))
	schJSON, _ := json.Marshal(domain.Schedule{Version: domain.Version, ID: "sch", ChannelID: "c", Revision: 3, Title: "Meeting", CreatedBy: "u"})
	must(t, os.WriteFile(filepath.Join(root, "schedules", "sch", "meta.json"), schJSON, 0600))

	var respLines []byte
	for i := 0; i < 3; i++ {
		resp := domain.ScheduleResponse{
			Version:   domain.Version,
			Seq:       int64(i + 1),
			UserID:    "u",
			Choices:   map[string]domain.ScheduleChoice{"a": domain.ScheduleYes},
			UpdatedAt: time.Date(2026, 1, 1, 0, i, 0, 0, time.UTC),
		}
		line, _ := json.Marshal(resp)
		respLines = append(respLines, append(line, '\n')...)
	}
	must(t, os.WriteFile(filepath.Join(root, "schedules", "sch", "responses.jsonl"), respLines, 0600))

	// 5. state
	must(t, os.MkdirAll(filepath.Join(root, "state"), 0700))
	stateJSON, _ := json.Marshal(map[string]any{
		"version": domain.Version,
		"channels": map[string]any{
			"c": map[string]any{"lastReadSeq": 3},
		},
		"threads": map[string]any{
			"c": map[string]any{
				"1": map[string]any{"lastReadSeq": 4},
			},
		},
		"mentions": map[string]any{
			"m1": time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		},
	})
	must(t, os.WriteFile(filepath.Join(root, "state", "v.json"), stateJSON, 0600))

	// 6. sessions
	must(t, os.MkdirAll(filepath.Join(root, "sessions"), 0700))
	sessJSON, _ := json.Marshal(domain.Session{
		Version:        domain.Version,
		TokenHash:      "tokenhash",
		UserID:         "u",
		CSRFToken:      "csrf",
		ExpiresAt:      time.Now().Add(time.Hour).UTC(),
		AuthGeneration: 7,
	})
	must(t, os.WriteFile(filepath.Join(root, "sessions", "tokenhash.json"), sessJSON, 0600))

	return root
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func sourceHashes(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	for _, dir := range []string{"users", "groups", "channels", "state", "sessions", "schedules"} {
		must(t, filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			out[rel] = sha256.Sum256(b)
			return nil
		}))
	}
	return out
}
func verifyMigratedData(t *testing.T, s storage.Storage) {
	t.Helper()
	ctx := context.Background()
	for _, rootSeq := range []int64{0, 1} {
		msgs, err := s.GetMessages(ctx, "c", storage.MessageQuery{Limit: 100, ThreadRootSeq: rootSeq})
		must(t, err)
		if len(msgs.Messages) != 3 {
			t.Fatalf("expected 3 messages for rootSeq=%d, got %d", rootSeq, len(msgs.Messages))
		}
	}
	sums, err := s.ThreadSummaries(ctx, "v", "c")
	must(t, err)
	if len(sums) != 1 || sums[0].RootSeq != 1 || sums[0].ReplyCount != 3 {
		t.Fatalf("thread state differs: %+v", sums)
	}
	mentions, err := s.ListMentions(ctx, "v")
	must(t, err)
	if len(mentions) != 6 {
		t.Fatalf("mentions differ: %+v", mentions)
	}
	reactions, err := s.GetReactions(ctx, "c", 0)
	must(t, err)
	if len(reactions) != 1 || reactions[0].Key != "thanks" || len(reactions[0].UserIDs) != 1 {
		t.Fatalf("reactions differ: %+v", reactions)
	}
	schedules, err := s.ListScheduleResponses(ctx, "sch")
	must(t, err)
	if len(schedules) != 1 || schedules[0].Choices["a"] != domain.ScheduleYes {
		t.Fatalf("schedule responses differ: %+v", schedules)
	}
	user, err := s.GetUser(ctx, "u")
	must(t, err)
	if user.ID != "u" || user.Name != "u" {
		t.Fatalf("user differs: %+v", user)
	}
	session, err := s.GetSession(ctx, "tokenhash")
	must(t, err)
	if session.UserID != "u" {
		t.Fatalf("session differs: %+v", session)
	}
	readSeq, err := s.GetReadState(ctx, "v", "c")
	must(t, err)
	if readSeq != 3 {
		t.Fatalf("read state differs: %d", readSeq)
	}
}

func TestMigrateAndRestore(t *testing.T) {
	root := fixture(t)
	ctx := context.Background()
	before := sourceHashes(t, root)
	report, err := Migrate(ctx, root, true)
	must(t, err)
	if report.Records["messages"].Count != 6 || report.Records["reaction_events"].Count != 3 || report.Records["response_events"].Count != 3 {
		t.Fatal(report)
	}
	if _, err = os.Stat(filepath.Join(root, DBName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("dry-run published DB")
	}
	if mark, err := readMarker(root); err != nil || mark != "" {
		t.Fatal(mark, err)
	}
	_, err = Migrate(ctx, root, false)
	must(t, err)
	if !reflect.DeepEqual(before, sourceHashes(t, root)) {
		t.Fatal("source changed")
	}
	if h, err := Open(root, "file"); err == nil {
		h.Close()
		t.Fatal("file opened after migration")
	}
	h, err := Open(root, "sqlite")
	must(t, err)
	verifyMigratedData(t, h)
	must(t, h.Close())
	backup := t.TempDir()
	must(t, os.CopyFS(backup, os.DirFS(root)))
	restored, err := Open(backup, "sqlite")
	must(t, err)
	defer restored.Close()
	verifyMigratedData(t, restored)
	m, err := restored.AddMessage(ctx, "c", domain.Message{UserID: "u", Text: "after restore"})
	must(t, err)
	if m.Seq != 7 {
		t.Fatal(m.Seq)
	}
	r, err := restored.AddScheduleResponse(ctx, "sch", domain.ScheduleResponse{UserID: "u"})
	must(t, err)
	if r.Seq != 4 {
		t.Fatal(r.Seq)
	}
	must(t, restored.Close())
}

func TestSelectionAndLocking(t *testing.T) {
	root := t.TempDir()
	if _, err := Open(root, "file"); err == nil {
		t.Fatal("file storage should be rejected")
	}
	h, err := Open(root, "sqlite")
	must(t, err)
	if other, err := Open(root, "sqlite"); err == nil {
		other.Close()
		t.Fatal("double open")
	}
	must(t, h.Close())
	root = fixture(t)
	if other, err := Open(root, "sqlite"); err == nil {
		other.Close()
		t.Fatal("file data shadowed without migration")
	}
	empty := t.TempDir()
	h, err = Open(empty, "sqlite")
	must(t, err)
	must(t, h.Close())
	must(t, os.Remove(filepath.Join(empty, DBName)))
	if h, err = Open(empty, "sqlite"); err == nil {
		h.Close()
		t.Fatal("missing DB recreated")
	}
}

func TestMigrationCorruptionAndIncompleteTail(t *testing.T) {
	for _, tc := range []struct {
		name, tail string
		ok         bool
	}{{"incomplete", "{", true}, {"corrupt", "{bad}\n", false}, {"duplicate", "duplicate", false}} {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t)
			path := filepath.Join(root, "channels", "c", "messages", "2026-01-01.jsonl")
			tail := []byte(tc.tail)
			if tc.name == "duplicate" {
				var err error
				tail, err = os.ReadFile(path)
				must(t, err)
			}
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
			must(t, err)
			_, err = f.Write(tail)
			must(t, err)
			must(t, f.Close())
			before := sourceHashes(t, root)
			report, err := Migrate(context.Background(), root, false)
			if tc.ok {
				must(t, err)
				if len(report.IncompleteTails) != 1 {
					t.Fatal(report)
				}
			} else {
				if err == nil {
					t.Fatal("bad data accepted")
				}
				if _, err = os.Stat(filepath.Join(root, DBName)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("failed DB published")
				}
			}
			if !reflect.DeepEqual(before, sourceHashes(t, root)) {
				t.Fatal("source changed")
			}
		})
	}
}

func TestInterruptedMarkerPublicationRecovery(t *testing.T) {
	root := fixture(t)
	_, err := Migrate(context.Background(), root, false)
	must(t, err)
	must(t, writeMarker(root, "file"))
	if h, err := Open(root, "file"); err == nil {
		h.Close()
		t.Fatal("opened retained source")
	}
	_, err = Migrate(context.Background(), root, false)
	must(t, err)
	h, err := Open(root, "sqlite")
	must(t, err)
	must(t, h.Close())
}

func TestMigrationRejectsUnrelatedDBAndReferences(t *testing.T) {
	root := fixture(t)
	s, err := sqlitestore.Open(filepath.Join(root, DBName))
	must(t, err)
	must(t, s.Close())
	if _, err = Migrate(context.Background(), root, false); err == nil {
		t.Fatal("unrelated DB adopted")
	}
	root = fixture(t)
	path := filepath.Join(root, "channels", "c", "meta.json")
	b, err := os.ReadFile(path)
	must(t, err)
	var c domain.Channel
	must(t, json.Unmarshal(b, &c))
	c.Members = append(c.Members, "missing")
	b, err = json.Marshal(c)
	must(t, err)
	must(t, os.WriteFile(path, b, 0600))
	if _, err = Migrate(context.Background(), root, false); err == nil {
		t.Fatal("broken member reference accepted")
	}
}

func TestFileBackupRestore(t *testing.T) {
	root := fixture(t)
	_, err := Migrate(context.Background(), root, false)
	must(t, err)
	copyRoot := t.TempDir()
	must(t, os.CopyFS(copyRoot, os.DirFS(root)))
	h, err := Open(copyRoot, "sqlite")
	must(t, err)
	defer h.Close()
	u, err := h.GetUser(context.Background(), "u")
	must(t, err)
	if u.ID != "u" {
		t.Fatalf("expected user u, got %v", u)
	}
}
