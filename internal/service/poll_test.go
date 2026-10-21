package service_test

import (
	"context"
	"errors"
	"github.com/masapico/minihub/internal/domain"
	"github.com/masapico/minihub/internal/service"
	"github.com/masapico/minihub/internal/storage"
	"sync"
	"testing"
)

func TestPollAnonymousCountsAndAnswerChanges(t *testing.T) {
	ctx := context.Background()
	svc := setup(t)
	if _, err := svc.CreateChannel(ctx, "admin", domain.Channel{ID: "general", Name: "General", Type: domain.ChannelPublic, Members: []string{"alice", "bob"}}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreatePoll(ctx, "alice", service.NewPoll{ChannelID: "general", Question: "どちらを選びますか？", Options: []string{"A", "B"}, Multiple: true})
	if err != nil {
		t.Fatal(err)
	}
	page, err := svc.GetMessages(ctx, "alice", "general", storage.MessageQuery{Limit: 10})
	if err != nil || len(page.Messages) != 1 || page.Messages[0].Text != "アンケートを開始しました: どちらを選びますか？" {
		t.Fatalf("announcement=%+v err=%v", page.Messages, err)
	}
	id := created.Poll.ID
	if _, err := svc.VotePoll(ctx, "bob", id, 0, []string{"o1", "o2"}); err != nil {
		t.Fatal(err)
	}
	alice, err := svc.GetPoll(ctx, "alice", id)
	if err != nil {
		t.Fatal(err)
	}
	if alice.MyResponse != nil || alice.Respondents != 1 || alice.TotalVotes != 2 || alice.Counts[0].Percent != 100 {
		t.Fatalf("unexpected anonymous summary: %+v", alice)
	}
	bob, err := svc.GetPoll(ctx, "bob", id)
	if err != nil {
		t.Fatal(err)
	}
	if bob.MyResponse == nil || bob.MyResponse.UserID != "bob" {
		t.Fatalf("own answer missing: %+v", bob.MyResponse)
	}
	if _, err := svc.VotePoll(ctx, "bob", id, 0, []string{"o1"}); !errors.Is(err, service.ErrStale) {
		t.Fatalf("stale answer accepted: %v", err)
	}
	changed, err := svc.VotePoll(ctx, "bob", id, bob.MyResponse.Seq, []string{"o2"})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Respondents != 1 || changed.TotalVotes != 1 || changed.Counts[0].Votes != 0 || changed.Counts[1].Votes != 1 {
		t.Fatalf("double counted: %+v", changed)
	}
	if _, err := svc.ClosePoll(ctx, "alice", id, changed.Poll.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VotePoll(ctx, "bob", id, changed.MyResponse.Seq, []string{"o1"}); err == nil {
		t.Fatal("closed poll accepted answer")
	}
}

func TestPollAuthorizationAndConcurrentVotes(t *testing.T) {
	ctx := context.Background()
	svc := setup(t)
	if _, err := svc.CreateChannel(ctx, "admin", domain.Channel{ID: "private", Name: "Private", Type: domain.ChannelPrivate, Members: []string{"alice"}}); err != nil {
		t.Fatal(err)
	}
	p, err := svc.CreatePoll(ctx, "alice", service.NewPoll{ChannelID: "private", Question: "選択", Options: []string{"A", "B"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetPoll(ctx, "bob", p.Poll.ID); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("private detail leaked: %v", err)
	}
	if _, err := svc.VotePoll(ctx, "bob", p.Poll.ID, 0, []string{"o1"}); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("private vote accepted: %v", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := svc.VotePoll(ctx, "alice", p.Poll.ID, 0, []string{"o1"})
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else if !errors.Is(e, service.ErrStale) {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatalf("concurrent duplicate answers: %d", success)
	}
}

func TestPollCreateRetryKeepsOneAnnouncement(t *testing.T) {
	ctx := context.Background()
	svc := setup(t)
	if _, err := svc.CreateChannel(ctx, "admin", domain.Channel{ID: "general", Name: "General", Type: domain.ChannelPublic, Members: []string{"alice"}}); err != nil {
		t.Fatal(err)
	}
	input := service.NewPoll{RequestID: "request-1", ChannelID: "general", Question: "Retry?", Options: []string{"A", "B"}}
	first, err := svc.CreatePoll(ctx, "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreatePoll(ctx, "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Poll.ID != second.Poll.ID || first.Poll.AnnouncementSeq != second.Poll.AnnouncementSeq {
		t.Fatalf("duplicate: %+v %+v", first.Poll, second.Poll)
	}
	page, err := svc.GetMessages(ctx, "alice", "general", storage.MessageQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 {
		t.Fatalf("messages=%d", len(page.Messages))
	}
	input.Question = "Different"
	if _, err := svc.CreatePoll(ctx, "alice", input); !errors.Is(err, service.ErrConflict) {
		t.Fatalf("changed retry accepted: %v", err)
	}
}

func TestPollClearResponse(t *testing.T) {
	ctx := context.Background()
	svc := setup(t)
	if _, err := svc.CreateChannel(ctx, "admin", domain.Channel{ID: "general", Name: "General", Type: domain.ChannelPublic, Members: []string{"alice", "bob"}}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreatePoll(ctx, "alice", service.NewPoll{ChannelID: "general", Question: "どちらを選びますか？", Options: []string{"A", "B"}, Multiple: true})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Poll.ID

	// 未回答のユーザーがクリア（選択肢0）を送るとエラー
	if _, err := svc.VotePoll(ctx, "bob", id, 0, []string{}); !errors.Is(err, service.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for clear without response, got %v", err)
	}

	// 回答する
	voted, err := svc.VotePoll(ctx, "bob", id, 0, []string{"o1", "o2"})
	if err != nil {
		t.Fatal(err)
	}
	if voted.Respondents != 1 || voted.TotalVotes != 2 || voted.MyResponse == nil {
		t.Fatalf("unexpected voted summary: %+v", voted)
	}
	bobSeq := voted.MyResponse.Seq

	// 古い revision でのクリアは ErrStale
	if _, err := svc.VotePoll(ctx, "bob", id, bobSeq-1, []string{}); !errors.Is(err, service.ErrStale) {
		t.Fatalf("expected ErrStale, got %v", err)
	}

	// 正しい revision で回答を取り消す（クリア）
	cleared, err := svc.VotePoll(ctx, "bob", id, bobSeq, []string{})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Respondents != 0 || cleared.TotalVotes != 0 || cleared.MyResponse != nil {
		t.Fatalf("expected cleared state, got respondents=%d totalVotes=%d myResponse=%+v", cleared.Respondents, cleared.TotalVotes, cleared.MyResponse)
	}
	for i, c := range cleared.Counts {
		if c.Votes != 0 || c.Percent != 0 {
			t.Fatalf("expected 0 votes for option %d, got %+v", i, c)
		}
	}

	// 別のユーザー（alice）から見ても集計が 0 になっていること
	aliceView, err := svc.GetPoll(ctx, "alice", id)
	if err != nil {
		t.Fatal(err)
	}
	if aliceView.Respondents != 0 || aliceView.TotalVotes != 0 {
		t.Fatalf("alice sees incorrect summary after clear: %+v", aliceView)
	}

	// クリア済みの状態で再度クリアしようとすると ErrInvalid
	if _, err := svc.VotePoll(ctx, "bob", id, bobSeq, []string{}); !errors.Is(err, service.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for double clear, got %v", err)
	}

	// クリア後に revision 0 で再度回答できること
	revoted, err := svc.VotePoll(ctx, "bob", id, 0, []string{"o1"})
	if err != nil {
		t.Fatalf("failed to revote after clear: %v", err)
	}
	if revoted.Respondents != 1 || revoted.TotalVotes != 1 || revoted.MyResponse == nil || revoted.Counts[0].Votes != 1 {
		t.Fatalf("unexpected revoted summary: %+v", revoted)
	}
}
