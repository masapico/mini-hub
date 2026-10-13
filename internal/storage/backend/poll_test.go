package backend

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/masapico/minihub/internal/domain"
)

func TestPollMigrationAndRestore(t *testing.T) {
	root := fixture(t)
	ctx := context.Background()
	poll := &domain.Poll{
		Version:   domain.Version,
		ID:        "poll1",
		ChannelID: "c",
		Question:  "Choice?",
		Options:   []domain.PollOption{{ID: "o1", Text: "A"}, {ID: "o2", Text: "B"}},
		Status:    "open",
		CreatedBy: "u",
		Revision:  2,
	}
	must(t, os.MkdirAll(filepath.Join(root, "polls", poll.ID), 0700))
	pollJSON, err := json.Marshal(poll)
	must(t, err)
	must(t, os.WriteFile(filepath.Join(root, "polls", poll.ID, "meta.json"), pollJSON, 0600))

	msg := domain.Message{
		Seq:       7,
		ID:        "m7",
		UserID:    "u",
		Text:      "投票を開始しました",
		PollRef:   &domain.PollReference{ID: poll.ID},
		Timestamp: time.Date(2026, 1, 7, 0, 0, 0, 0, time.UTC),
	}
	msgJSON, err := json.Marshal(msg)
	must(t, err)
	must(t, os.WriteFile(filepath.Join(root, "channels", "c", "messages", "2026-01-07.jsonl"), append(msgJSON, '\n'), 0600))

	r1 := domain.PollResponse{Version: domain.Version, Seq: 1, UserID: "v", OptionIDs: []string{"o1"}}
	r2 := domain.PollResponse{Version: domain.Version, Seq: 2, UserID: "v", OptionIDs: []string{"o2"}}
	b1, err := json.Marshal(r1)
	must(t, err)
	b2, err := json.Marshal(r2)
	must(t, err)
	lines := append(append(b1, '\n'), append(b2, '\n')...)
	must(t, os.WriteFile(filepath.Join(root, "polls", poll.ID, "responses.jsonl"), lines, 0600))

	before := sourceHashes(t, root)
	report, err := Migrate(ctx, root, false)
	must(t, err)
	if report.Records["poll_response_events"].Count != 2 {
		t.Fatal(report)
	}
	after := sourceHashes(t, root)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("source data changed")
	}
	restored, err := Open(root, "sqlite")
	must(t, err)
	p, err := restored.GetPoll(ctx, poll.ID)
	must(t, err)
	if p.Question != poll.Question {
		t.Fatal(p)
	}
	answers, err := restored.ListPollResponses(ctx, poll.ID)
	must(t, err)
	if len(answers) != 1 || answers[0].OptionIDs[0] != "o2" {
		t.Fatal(answers)
	}
	msgGot, err := restored.GetMessage(ctx, "c", 7)
	must(t, err)
	if msgGot.PollRef == nil || msgGot.PollRef.ID != poll.ID {
		t.Fatal(msgGot)
	}
	must(t, restored.Close())
	backup := t.TempDir()
	must(t, os.CopyFS(backup, os.DirFS(root)))
	copyStore, err := Open(backup, "sqlite")
	must(t, err)
	defer copyStore.Close()
	_, err = copyStore.GetPoll(ctx, poll.ID)
	must(t, err)
}
