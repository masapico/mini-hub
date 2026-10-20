package service_test

import (
	"context"
	"errors"
	"github.com/masapico/minihub/internal/domain"
	"github.com/masapico/minihub/internal/service"
	"github.com/masapico/minihub/internal/storage"
	"testing"
)

func (p *recordingPublisher) ThreadUpdated(_ context.Context, channel string, root, seq int64, userID string) {
	p.channelID = channel
	p.reactionSeq = root
	p.seq = seq
}
func TestThreadsAuthorizationUnreadAndMentions(t *testing.T) {
	svc := setup(t)
	ctx := context.Background()
	pub := &recordingPublisher{}
	svc.SetMessagePublisher(pub)
	_, err := svc.CreateChannel(ctx, "admin", domain.Channel{ID: "general", Name: "General", Type: domain.ChannelPublic, Members: []string{"alice"}})
	if err != nil {
		t.Fatal(err)
	}
	root, err := svc.PostMessage(ctx, "alice", "general", "Question")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostThreadMessage(ctx, "bob", "general", root.Seq, "no membership"); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("unjoined=%v", err)
	}
	if _, err := svc.GetThreadMessages(ctx, "bob", "general", root.Seq, storage.MessageQuery{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.JoinChannel(ctx, "bob", "general"); err != nil {
		t.Fatal(err)
	}
	reply, err := svc.PostThreadMessage(ctx, "bob", "general", root.Seq, "@alice answer")
	if err != nil {
		t.Fatal(err)
	}
	if pub.seq != reply.Seq || pub.reactionSeq != root.Seq {
		t.Fatalf("event=%+v", pub)
	}
	status, err := svc.GetReadStatus(ctx, "bob", "general")
	if err != nil || status.LastReadSeq != root.Seq || status.UnreadCount != 0 || status.LatestSeq != root.Seq || status.ThreadUpdates {
		t.Fatalf("reply advanced channel read: %+v %v", status, err)
	}
	status, err = svc.GetReadStatus(ctx, "alice", "general")
	if err != nil || status.UnreadCount != 0 || !status.ThreadUpdates {
		t.Fatalf("alice=%+v %v", status, err)
	}
	page, err := svc.GetMessages(ctx, "alice", "general", storage.MessageQuery{})
	if err != nil || len(page.Messages) != 1 || page.Threads[root.Seq].ReplyCount != 1 {
		t.Fatalf("main=%+v %v", page, err)
	}
	list, err := svc.ListThreads(ctx, "alice", "", true, true, "", 1)
	if err != nil || len(list.Threads) != 1 || list.UnreadCount != 1 {
		t.Fatalf("list=%+v %v", list, err)
	}
	mentions, err := svc.ListMentions(ctx, "alice", "", 50)
	if err != nil || len(mentions.Mentions) != 1 || mentions.Mentions[0].Message.ThreadRootSeq != root.Seq {
		t.Fatalf("mentions=%+v %v", mentions, err)
	}
	if _, err := svc.PostThreadMessage(ctx, "bob", "general", reply.Seq, "nested"); !errors.Is(err, service.ErrInvalid) {
		t.Fatalf("nested=%v", err)
	}
	if _, err := svc.PostThreadMessage(ctx, "bob", "general", 9999, "missing"); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("missing=%v", err)
	}
	if err := svc.MarkRead(ctx, "alice", "general", reply.Seq); !errors.Is(err, service.ErrInvalid) {
		t.Fatalf("main read accepted reply=%v", err)
	}
	if err := svc.MarkThreadRead(ctx, "alice", "general", root.Seq, root.Seq); !errors.Is(err, service.ErrInvalid) {
		t.Fatalf("thread read accepted root=%v", err)
	}
	if err := svc.MarkThreadRead(ctx, "alice", "general", root.Seq, reply.Seq); err != nil {
		t.Fatal(err)
	}
	mentions, _ = svc.ListMentions(ctx, "alice", "", 50)
	if mentions.UnreadCount != 1 {
		t.Fatal("thread read cleared mention")
	}
	_, err = svc.CreateChannel(ctx, "admin", domain.Channel{ID: "secret", Name: "Secret", Type: domain.ChannelPrivate, Members: []string{"alice"}})
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := svc.PostMessage(ctx, "alice", "secret", "private")
	if _, err := svc.PostThreadMessage(ctx, "admin", "secret", secret.Seq, "private reply"); err != nil {
		t.Fatal(err)
	}
	for _, call := range []func() error{
		func() error {
			_, err := svc.GetThreadMessages(ctx, "bob", "secret", secret.Seq, storage.MessageQuery{})
			return err
		},
		func() error { _, err := svc.PostThreadMessage(ctx, "bob", "secret", secret.Seq, "hidden"); return err },
		func() error { _, err := svc.ListThreads(ctx, "bob", "secret", false, false, "", 50); return err },
		func() error { return svc.MarkThreadRead(ctx, "bob", "secret", secret.Seq, 2) },
	} {
		if err := call(); !errors.Is(err, service.ErrNotFound) {
			t.Fatalf("private access=%v", err)
		}
	}
	list, err = svc.ListThreads(ctx, "bob", "", false, false, "", 50)
	if err != nil || len(list.Threads) != 1 || list.Threads[0].ChannelID != "general" {
		t.Fatalf("leaked private list=%+v %v", list, err)
	}
	if err := svc.ArchiveChannel(ctx, "admin", "general"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostThreadMessage(ctx, "alice", "general", root.Seq, "archived"); err == nil {
		t.Fatal("archived write")
	}
}
func TestThreadListCursorAndDefaultUnreadPage(t *testing.T) {
	svc := setup(t)
	ctx := context.Background()
	_, err := svc.CreateChannel(ctx, "admin", domain.Channel{ID: "general", Name: "General", Type: domain.ChannelPublic, Members: []string{"alice", "bob"}})
	if err != nil {
		t.Fatal(err)
	}
	root, _ := svc.PostMessage(ctx, "alice", "general", "first")
	for i := 0; i < 60; i++ {
		if _, err := svc.PostThreadMessage(ctx, "bob", "general", root.Seq, "reply"); err != nil {
			t.Fatal(err)
		}
	}
	page, err := svc.GetThreadMessages(ctx, "alice", "general", root.Seq, storage.MessageQuery{Limit: 10})
	if err != nil || page.Messages[0].Seq != 2 || page.NextAfter == nil {
		t.Fatalf("first unread=%+v %v", page, err)
	}
	second, _ := svc.PostMessage(ctx, "alice", "general", "second")
	if _, err := svc.PostThreadMessage(ctx, "bob", "general", second.Seq, "reply"); err != nil {
		t.Fatal(err)
	}
	list, err := svc.ListThreads(ctx, "alice", "", true, true, "", 1)
	if err != nil || len(list.Threads) != 1 || list.Threads[0].RootSeq != second.Seq || list.NextCursor == "" {
		t.Fatalf("list=%+v %v", list, err)
	}
	next, err := svc.ListThreads(ctx, "alice", "", true, true, list.NextCursor, 1)
	if err != nil || len(next.Threads) != 1 || next.Threads[0].RootSeq != root.Seq {
		t.Fatalf("next=%+v %v", next, err)
	}
}

func TestNonParticipatingThreadDoesNotTriggerThreadUpdates(t *testing.T) {
	svc := setup(t)
	ctx := context.Background()
	_, err := svc.CreateUsers(ctx, "admin", []service.NewUser{{ID: "charlie", Name: "Charlie", Password: "password123"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateChannel(ctx, "admin", domain.Channel{ID: "general", Name: "General", Type: domain.ChannelPublic, Members: []string{"alice", "bob", "charlie"}})
	if err != nil {
		t.Fatal(err)
	}

	// alice posts a root message
	root, err := svc.PostMessage(ctx, "alice", "general", "root question")
	if err != nil {
		t.Fatal(err)
	}

	// bob replies to alice's root message
	reply1, err := svc.PostThreadMessage(ctx, "bob", "general", root.Seq, "bob's reply")
	if err != nil {
		t.Fatal(err)
	}

	// alice (root author) should have ThreadUpdates = true
	aliceStatus, err := svc.GetReadStatus(ctx, "alice", "general")
	if err != nil || !aliceStatus.ThreadUpdates {
		t.Fatalf("expected alice ThreadUpdates=true, got %+v (err=%v)", aliceStatus, err)
	}

	// bob (reply author, own reply) should have ThreadUpdates = false
	bobStatus, err := svc.GetReadStatus(ctx, "bob", "general")
	if err != nil || bobStatus.ThreadUpdates {
		t.Fatalf("expected bob ThreadUpdates=false, got %+v (err=%v)", bobStatus, err)
	}

	// charlie has not participated in this thread; charlie should have ThreadUpdates = false!
	charlieStatus, err := svc.GetReadStatus(ctx, "charlie", "general")
	if err != nil || charlieStatus.ThreadUpdates {
		t.Fatalf("expected non-participating charlie ThreadUpdates=false, got %+v (err=%v)", charlieStatus, err)
	}

	// charlie now replies to the thread, becoming a participant
	charlieReply, err := svc.PostThreadMessage(ctx, "charlie", "general", root.Seq, "charlie joins in")
	if err != nil {
		t.Fatal(err)
	}

	// charlie marks the thread read up to the posted reply (as the client does when viewing/posting)
	if err := svc.MarkThreadRead(ctx, "charlie", "general", root.Seq, charlieReply.Seq); err != nil {
		t.Fatal(err)
	}

	// charlie has read up to own reply; ThreadUpdates should be false
	charlieStatus, err = svc.GetReadStatus(ctx, "charlie", "general")
	if err != nil || charlieStatus.ThreadUpdates {
		t.Fatalf("expected charlie ThreadUpdates=false after reading up to own reply, got %+v", charlieStatus)
	}

	// alice posts another reply
	_, err = svc.PostThreadMessage(ctx, "alice", "general", root.Seq, "alice follow-up")
	if err != nil {
		t.Fatal(err)
	}

	// Now charlie is a participant, so charlie should have ThreadUpdates = true!
	charlieStatus, err = svc.GetReadStatus(ctx, "charlie", "general")
	if err != nil || !charlieStatus.ThreadUpdates {
		t.Fatalf("expected participating charlie ThreadUpdates=true after new reply, got %+v", charlieStatus)
	}

	_ = reply1
}

func TestLateJoinerHasNoUnreadThreadUpdates(t *testing.T) {
	svc := setup(t)
	ctx := context.Background()
	_, err := svc.CreateUsers(ctx, "admin", []service.NewUser{{ID: "dave", Name: "Dave", Password: "password123"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateChannel(ctx, "admin", domain.Channel{ID: "discussion", Name: "Discussion", Type: domain.ChannelPublic, Members: []string{"alice", "bob"}})
	if err != nil {
		t.Fatal(err)
	}

	// Active discussion before dave joins
	root, err := svc.PostMessage(ctx, "alice", "discussion", "Initial topic")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := svc.PostThreadMessage(ctx, "bob", "discussion", root.Seq, "reply"); err != nil {
			t.Fatal(err)
		}
	}

	// dave joins the channel afterwards
	if _, err := svc.JoinChannel(ctx, "dave", "discussion"); err != nil {
		t.Fatal(err)
	}

	status, err := svc.GetReadStatus(ctx, "dave", "discussion")
	if err != nil {
		t.Fatal(err)
	}
	if status.UnreadCount != 0 {
		t.Fatalf("expected dave UnreadCount=0, got %d", status.UnreadCount)
	}
	if status.ThreadUpdates {
		t.Fatalf("expected dave ThreadUpdates=false for past thread replies, got true")
	}
}

func TestChangeChannelMembersInitializesReadState(t *testing.T) {
	svc := setup(t)
	ctx := context.Background()
	_, err := svc.CreateChannel(ctx, "admin", domain.Channel{ID: "private-chat", Name: "Private Chat", Type: domain.ChannelPrivate, Members: []string{"alice"}})
	if err != nil {
		t.Fatal(err)
	}

	// alice posts messages before bob is invited
	for i := 0; i < 3; i++ {
		if _, err := svc.PostMessage(ctx, "alice", "private-chat", "hello"); err != nil {
			t.Fatal(err)
		}
	}

	// admin adds bob to the private channel
	_, err = svc.ChangeChannelMembers(ctx, "admin", "private-chat", service.ChannelMemberChanges{AddUsers: []string{"bob"}})
	if err != nil {
		t.Fatal(err)
	}

	status, err := svc.GetReadStatus(ctx, "bob", "private-chat")
	if err != nil {
		t.Fatal(err)
	}
	if status.UnreadCount != 0 {
		t.Fatalf("expected newly added bob UnreadCount=0, got %d (lastReadSeq=%d, latestSeq=%d)", status.UnreadCount, status.LastReadSeq, status.LatestSeq)
	}
	if status.ThreadUpdates {
		t.Fatalf("expected newly added bob ThreadUpdates=false, got true")
	}
}
