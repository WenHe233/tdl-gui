package catalog

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
	"github.com/local/tdl-gui/internal/domain"
	"github.com/local/tdl-gui/internal/store"
)

func scanMessage(id int, media bool) *tg.Message {
	m := &tg.Message{ID: id, Date: id, PeerID: &tg.PeerUser{UserID: 9}, Message: "fixture"}
	if media {
		m.SetMedia(&tg.MessageMediaDocument{Document: &tg.Document{ID: int64(id), Size: 12, MimeType: "video/mp4", Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeFilename{FileName: fmt.Sprintf("%d.mp4", id)}}}})
	}
	return m
}

func TestScanPagesBoundariesAndMediaSelection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		opts   ScanOptions
		cursor int64
		ids    []string
		max    int64
	}{
		{"all", ScanOptions{}, 0, []string{"5", "3", "1"}, 6},
		{"incremental", ScanOptions{}, 3, []string{"5"}, 6},
		{"no new media advances text cursor", ScanOptions{}, 5, nil, 6},
		{"nothing new", ScanOptions{}, 6, nil, 6},
		{"last media", ScanOptions{LastN: 2}, 0, []string{"5", "3"}, 6},
		{"inclusive dates", ScanOptions{From: time.Unix(3, 0), To: time.Unix(5, 0)}, 0, []string{"5", "3"}, 6},
		{"topic", ScanOptions{TopicID: "7"}, 0, []string{"5", "3", "1"}, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			q := messages.QueryFunc(func(_ context.Context, r messages.Request) (tg.MessagesMessagesClass, error) {
				calls++
				if calls > 1 {
					t.Fatal("requested beyond terminal page")
				}
				if !tc.opts.To.IsZero() && r.OffsetDate != 6 {
					t.Fatal(r)
				}
				page := []tg.MessageClass{}
				for i := 1; i <= 6; i++ {
					page = append(page, scanMessage(i, i%2 == 1))
				}
				return &tg.MessagesMessages{Messages: page}, nil
			})
			items, maxID, err := scanPages(context.Background(), q, "a", tc.opts, tc.cursor, func() {})
			if err != nil || maxID != tc.max || len(items) != len(tc.ids) {
				t.Fatal(len(items), maxID, err)
			}
			for i, m := range items {
				if m.MessageID != tc.ids[i] || m.Kind != "video" || m.Size != 12 || (tc.opts.TopicID != "" && m.TopicID != tc.opts.TopicID) {
					t.Fatal(m)
				}
			}
		})
	}
}

func TestScanPagesLargeHistoryAndDeletedPage(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		calls := 0
		q := messages.QueryFunc(func(_ context.Context, r messages.Request) (tg.MessagesMessagesClass, error) {
			calls++
			if calls > 3 {
				t.Fatal("too many requests")
			}
			hi := 251
			if r.OffsetID > 0 {
				hi = r.OffsetID
			}
			page := []tg.MessageClass{}
			for i := hi - 1; i > 0 && len(page) < 100; i-- {
				if deleted && calls == 1 {
					page = append(page, &tg.MessageEmpty{ID: i})
				} else {
					page = append(page, scanMessage(i, true))
				}
			}
			return &tg.MessagesChannelMessages{Messages: page, Count: 250}, nil
		})
		items, maxID, err := scanPages(context.Background(), q, "a", ScanOptions{}, 0, func() {})
		want := 250
		if deleted {
			want = 150
		}
		if err != nil || len(items) != want || maxID != 250 || calls != 3 {
			t.Fatal(len(items), maxID, calls, err)
		}
		for i := 1; i < len(items); i++ {
			if items[i].MessageID == items[i-1].MessageID {
				t.Fatal("duplicate")
			}
		}
	}
}

func TestScanPagesEmptyCycleFailureAndCancellation(t *testing.T) {
	items, cursor, err := scanPages(context.Background(), messages.QueryFunc(func(context.Context, messages.Request) (tg.MessagesMessagesClass, error) {
		return &tg.MessagesMessages{}, nil
	}), "a", ScanOptions{}, 42, func() {})
	if err != nil || len(items) != 0 || cursor != 42 {
		t.Fatal(items, cursor, err)
	}
	calls := 0
	q := messages.QueryFunc(func(context.Context, messages.Request) (tg.MessagesMessagesClass, error) {
		calls++
		p := []tg.MessageClass{}
		for i := 100; i > 0; i-- {
			p = append(p, scanMessage(i, true))
		}
		return &tg.MessagesMessagesSlice{Messages: p, Count: 200}, nil
	})
	_, _, err = scanPages(context.Background(), q, "a", ScanOptions{}, 0, func() {})
	if err == nil || !strings.Contains(err.Error(), "分页未前进") || calls != 2 {
		t.Fatal(calls, err)
	}
	want := errors.New("network failure")
	_, _, err = scanPages(context.Background(), messages.QueryFunc(func(context.Context, messages.Request) (tg.MessagesMessagesClass, error) { return nil, want }), "a", ScanOptions{}, 0, func() {})
	if !errors.Is(err, want) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = scanPages(ctx, q, "a", ScanOptions{}, 0, func() {})
	if !errors.Is(err, context.Canceled) || calls != 2 {
		t.Fatal(calls, err)
	}
}

func TestScanDeadlineStopsHungConnectionAndAllowsProgress(t *testing.T) {
	err := withScanDeadline(context.Background(), 10*time.Millisecond, func(ctx context.Context, _ func()) error { <-ctx.Done(); return ctx.Err() })
	if !errors.Is(err, errScanIdle) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	err = withScanDeadline(ctx, time.Minute, func(ctx context.Context, _ func()) error { cancel(); <-ctx.Done(); return ctx.Err() })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// A progressing scan can run longer than the idle interval.
	err = withScanDeadline(context.Background(), 500*time.Millisecond, func(ctx context.Context, progress func()) error {
		for i := 0; i < 8; i++ {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
				progress()
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestScanSourcePersistsOnlySuccessfulResults(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	s := New(st, nil, t.TempDir())
	a := domain.Account{ID: "a"}
	o := ScanOptions{ChatID: "c"}
	m := domain.Media{AccountID: "a", ChatID: "c", MessageID: "4", MediaID: "m", Date: time.Now(), Size: 12, Downloaded: true, LocalPath: "keep.mp4"}
	s.ScanSource = func(_ context.Context, _ domain.Account, _ ScanOptions, cursor int64) ([]domain.Media, int64, error) {
		if cursor != 0 {
			t.Fatal(cursor)
		}
		return []domain.Media{m}, 6, nil
	}
	if _, err = s.Scan(ctx, a, o); err != nil {
		t.Fatal(err)
	} // No cursor yet is a valid first scan.
	s.ScanSource = func(_ context.Context, _ domain.Account, _ ScanOptions, cursor int64) ([]domain.Media, int64, error) {
		if cursor != 6 {
			t.Fatal(cursor)
		}
		return nil, 0, nil
	}
	if _, err = s.Scan(ctx, a, o); err != nil {
		t.Fatal(err)
	}
	if cursor, _ := st.ScanCursor(ctx, "a", "c", ""); cursor != "6" {
		t.Fatal(cursor)
	}
	o.Rescan = true
	s.ScanSource = func(context.Context, domain.Account, ScanOptions, int64) ([]domain.Media, int64, error) {
		return nil, 100, errScanIdle
	}
	if _, err = s.Scan(ctx, a, o); !errors.Is(err, errScanIdle) {
		t.Fatal(err)
	}
	items, _ := st.Media(ctx, "a", "c")
	if len(items) != 1 || !items[0].Downloaded {
		t.Fatal(items)
	}
	if cursor, _ := st.ScanCursor(ctx, "a", "c", ""); cursor != "6" {
		t.Fatal(cursor)
	}
	s.ScanSource = func(context.Context, domain.Account, ScanOptions, int64) ([]domain.Media, int64, error) {
		fresh := m
		fresh.Downloaded = false
		fresh.LocalPath = ""
		return []domain.Media{fresh}, 8, nil
	}
	if _, err = s.Scan(ctx, a, o); err != nil {
		t.Fatal(err)
	}
	items, _ = st.Media(ctx, "a", "c")
	if len(items) != 1 || !items[0].Downloaded || items[0].LocalPath != "keep.mp4" {
		t.Fatal(items)
	}
}
