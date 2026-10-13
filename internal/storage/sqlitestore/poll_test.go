package sqlitestore

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/masapico/minihub/internal/domain"
)

func TestPollStorage(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	poll := &domain.Poll{ID: "poll1", ChannelID: "c", Question: "Q", Options: []domain.PollOption{{ID: "o1", Text: "A"}, {ID: "o2", Text: "B"}}, Status: "open"}
	copy := *poll
	if err := s.SavePoll(ctx, &copy); err != nil {
		t.Fatal(err)
	}
	for _, choice := range []string{"o1", "o2"} {
		if _, err := s.AddPollResponse(ctx, poll.ID, domain.PollResponse{UserID: "u", OptionIDs: []string{choice}}); err != nil {
			t.Fatal(err)
		}
	}
	b, e := s.ListPollResponses(ctx, poll.ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(b) != 1 || b[0].Seq != 2 || b[0].OptionIDs[0] != "o2" {
		t.Fatalf("sqlite=%+v", b)
	}
	pb, e := s.GetPoll(ctx, poll.ID)
	if e != nil {
		t.Fatal(e)
	}
	if pb.Question != "Q" || len(pb.Options) != 2 {
		t.Fatalf("poll mismatch: %+v", pb)
	}
}

func TestUpgradeV1ForPolls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveUser(context.Background(), &domain.User{ID: "u", Name: "User"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`DROP TABLE polls; DROP TABLE poll_response_events; DROP INDEX message_poll; ALTER TABLE messages DROP COLUMN poll_id; ALTER TABLE messages DROP COLUMN ai; PRAGMA user_version=1;`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, err := s.GetUser(context.Background(), "u")
	if err != nil || u.Name != "User" {
		t.Fatalf("user lost: %+v %v", u, err)
	}
	if err := s.SavePoll(context.Background(), &domain.Poll{ID: "p", ChannelID: "c", Question: "Q"}); err != nil {
		t.Fatal(err)
	}
}
