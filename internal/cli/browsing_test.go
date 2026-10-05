package cli

import (
	"context"
	"fmt"
	"github.com/local/tdl-gui/internal/authui"
	"github.com/local/tdl-gui/internal/domain"
	"os"
	"testing"
	"time"
)

func TestBrowseMediaBothDirectionsAndRecentSubset(t *testing.T) {
	r, _, items := selectionServer(t)
	ctx := context.Background()
	for i := 126; i <= 250; i++ {
		m := items[0]
		m.MessageID = fmt.Sprint(i)
		m.MediaID = m.MessageID
		m.Date = items[0].Date
		items = append(items, m)
	}
	for i := range items {
		items[i].Date = items[0].Date
	}
	if err := r.app.Store.SaveMedia(ctx, items); err != nil {
		t.Fatal(err)
	}
	for _, order := range []string{"oldest", "newest"} {
		seen := map[string]bool{}
		for offset := 0; offset < 250; offset += 100 {
			got, err := r.call(ctx, "media.list", raw(map[string]any{"accountId": "a", "chatId": "10", "topicId": "5", "order": order, "offset": offset, "limit": 100}))
			if err != nil {
				t.Fatal(err)
			}
			page := got.(map[string]any)
			ms := page["items"].([]domain.Media)
			for i, m := range ms {
				want := offset + i + 1
				if order == "newest" {
					want = 250 - offset - i
				}
				if m.MessageID != fmt.Sprint(want) || seen[m.MessageID] {
					t.Fatal(order, offset, m.MessageID, want)
				}
				seen[m.MessageID] = true
			}
		}
		if len(seen) != 250 {
			t.Fatal(len(seen))
		}
	}
	got, err := r.call(ctx, "media.list", raw(map[string]any{"accountId": "a", "chatId": "10", "order": "oldest", "browseRule": domain.Rule{LastN: 5}}))
	if err != nil {
		t.Fatal(err)
	}
	page := got.(map[string]any)
	ms := page["items"].([]domain.Media)
	if len(ms) != 5 || ms[0].MessageID != "246" || ms[4].MessageID != "250" {
		t.Fatal(ms)
	}
	if _, err = r.call(ctx, "media.list", raw(map[string]any{"order": "invalid"})); err == nil {
		t.Fatal("invalid order accepted")
	}
}

func TestRemoveLoginCancelsRefreshAndKeepsMedia(t *testing.T) {
	r, _, _ := selectionServer(t)
	r.auth = authui.New(r.app.Store, r.app.Paths.TDLStorage, func(authui.Event) {}, r.app.Runner)
	if err := os.WriteFile(r.app.Paths.TDLStorage, []byte(`{"test":{"session":"old"},"other":{"session":"keep"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	r.app.Catalog.Directory = func(ctx context.Context, a domain.Account) ([]domain.Chat, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	finished := make(chan error, 1)
	go func() {
		_, err := r.call(context.Background(), "chats.refresh", raw(map[string]any{"accountId": "a"}))
		finished <- err
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := r.call(ctx, "accounts.remove", raw(map[string]any{"id": "a"})); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err == nil {
		t.Fatal("refresh was not cancelled")
	}
	accounts, _ := r.app.Store.Accounts(ctx)
	if len(accounts) != 0 {
		t.Fatal(accounts)
	}
	media, _ := r.app.Store.Media(ctx, "a", "10")
	if len(media) != 125 {
		t.Fatal("media removed", len(media))
	}
	if _, err := r.call(ctx, "chats.list", raw(map[string]any{"accountId": "a"})); err == nil {
		t.Fatal("removed account accessible")
	}
	b, err := os.ReadFile(r.app.Paths.TDLStorage)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"other":{"session":"keep"}}` {
		t.Fatal("namespace cleanup incorrect")
	}
	if _, err := r.call(ctx, "accounts.remove", raw(map[string]any{"id": "a"})); err != nil {
		t.Fatal("repeat removal", err)
	}
}
