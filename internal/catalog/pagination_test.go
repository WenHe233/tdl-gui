package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
)

func dialogPage(start, n, total int) *tg.MessagesDialogsSlice {
	p := &tg.MessagesDialogsSlice{Count: total}
	for i := start; i < start+n; i++ {
		peer := &tg.PeerChat{ChatID: int64(i)}
		p.Dialogs = append(p.Dialogs, &tg.Dialog{Peer: peer, TopMessage: i})
		p.Chats = append(p.Chats, &tg.Chat{ID: int64(i), Title: "test"})
		p.Messages = append(p.Messages, &tg.Message{ID: i, Date: 10000 - i, PeerID: peer})
	}
	return p
}

func fullDialogPage(start, n int) *tg.MessagesDialogs {
	p := dialogPage(start, n, n)
	return &tg.MessagesDialogs{Dialogs: p.Dialogs, Messages: p.Messages, Chats: p.Chats}
}

func TestDirectoryPaginationStopsWithoutAnotherRequest(t *testing.T) {
	for _, tc := range []struct {
		name  string
		pages []tg.MessagesDialogsClass
		want  int
	}{
		{"single full page", []tg.MessagesDialogsClass{fullDialogPage(1, 3)}, 3},
		{"empty full page", []tg.MessagesDialogsClass{fullDialogPage(1, 0)}, 0},
		{"250 dialogs and short final slice", []tg.MessagesDialogsClass{dialogPage(1, 100, 250), dialogPage(101, 100, 250), dialogPage(201, 50, 250)}, 250},
		{"full page after a slice", []tg.MessagesDialogsClass{dialogPage(1, 100, 101), fullDialogPage(101, 1)}, 101},
		{"empty last slice", []tg.MessagesDialogsClass{dialogPage(1, 100, 200), dialogPage(101, 0, 200)}, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			iter := newDirectoryIterator(func(ctx context.Context, r dialogs.Request) (tg.MessagesDialogsClass, error) {
				if calls >= len(tc.pages) {
					return nil, fmt.Errorf("unexpected request after last page")
				}
				if calls > 0 && r.OffsetID == 0 {
					t.Fatal("offset did not advance")
				}
				page := tc.pages[calls]
				calls++
				return page, nil
			})
			n := 0
			for iter.Next(context.Background()) {
				n++
			}
			if iter.Err() != nil || n != tc.want || calls != len(tc.pages) {
				t.Fatalf("items=%d requests=%d err=%v", n, calls, iter.Err())
			}
		})
	}
}

func TestDirectoryPaginationFinalPageNeedsNoOffsetPeer(t *testing.T) {
	p := dialogPage(1, 1, 1)
	p.Chats = nil
	p.Messages = nil
	calls := 0
	iter := newDirectoryIterator(func(context.Context, dialogs.Request) (tg.MessagesDialogsClass, error) { calls++; return p, nil })
	if !iter.Next(context.Background()) || !iter.Value().Deleted() {
		t.Fatal("missing peer was not represented")
	}
	if iter.Next(context.Background()) || iter.Err() != nil || calls != 1 {
		t.Fatal("terminal missing peer failed", calls, iter.Err())
	}
}

func TestDirectoryPaginationStillRejectsRealCyclesAndNetworkErrors(t *testing.T) {
	calls := 0
	iter := newDirectoryIterator(func(context.Context, dialogs.Request) (tg.MessagesDialogsClass, error) {
		calls++
		return dialogPage(1, 1, 3), nil
	})
	for iter.Next(context.Background()) {
	}
	if iter.Err() == nil || !strings.Contains(iter.Err().Error(), "分页未前进") || calls != 2 {
		t.Fatal("cycle not bounded", calls, iter.Err())
	}
	want := errors.New("network interrupted")
	calls = 0
	iter = newDirectoryIterator(func(context.Context, dialogs.Request) (tg.MessagesDialogsClass, error) {
		calls++
		if calls == 1 {
			return dialogPage(1, 1, 3), nil
		}
		return nil, want
	})
	for iter.Next(context.Background()) {
	}
	if !errors.Is(iter.Err(), want) {
		t.Fatal(iter.Err())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	iter = newDirectoryIterator(func(context.Context, dialogs.Request) (tg.MessagesDialogsClass, error) {
		t.Fatal("cancelled request sent")
		return nil, nil
	})
	if iter.Next(ctx) || !errors.Is(iter.Err(), context.Canceled) {
		t.Fatal(iter.Err())
	}
}

func TestUpstreamTerminalPageReproduces030Cycle(t *testing.T) {
	visited := map[string]bool{}
	iter := dialogs.NewIterator(dialogs.QueryFunc(func(ctx context.Context, r dialogs.Request) (tg.MessagesDialogsClass, error) {
		key := fmt.Sprintf("%d/%d/%s", r.OffsetDate, r.OffsetID, peerKey(r.OffsetPeer))
		if visited[key] {
			return nil, fmt.Errorf("聊天分页未前进")
		}
		visited[key] = true
		return fullDialogPage(1, 3), nil
	}), 100)
	for iter.Next(context.Background()) {
	}
	if iter.Err() == nil || !strings.Contains(iter.Err().Error(), "分页未前进") {
		t.Fatal("0.3.0 failure was not reproduced", iter.Err())
	}
}
