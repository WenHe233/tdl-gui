package store

import (
	"context"
	"github.com/local/tdl-gui/internal/domain"
	"path/filepath"
	"testing"
	"time"
)

func TestDirectorySnapshotSortingAndIsolation(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	chats := []domain.Chat{{ID: "1", VisibleName: "Zulu", PinnedOrder: 1}, {ID: "2", VisibleName: "Beta", LastMessageAt: time.Unix(200, 0)}, {ID: "3", VisibleName: "Alpha", LastMessageAt: time.Unix(100, 0)}}
	folders := []domain.ChatFolder{{ID: "f", Title: "工作", ChatIDs: []string{"2", "3"}, PinnedIDs: []string{"3"}}}
	if err = s.SaveDirectory(ctx, "a", chats, folders); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ folder, order, want string }{{"all", "recent", "123"}, {"all", "name", "321"}, {"f", "recent", "32"}, {"f", "name", "32"}} {
		list, e := s.BrowseChats(ctx, "a", "", c.folder, c.order)
		if e != nil {
			t.Fatal(e)
		}
		ids := ""
		for _, v := range list {
			ids += v.ID
		}
		if ids != c.want {
			t.Fatal(c, ids)
		}
	}
	list, err := s.BrowseChats(ctx, "b", "", "all", "recent")
	if err != nil || len(list) != 0 {
		t.Fatal(list, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if s.SaveDirectory(cancelled, "a", nil, nil) == nil {
		t.Fatal("cancelled write succeeded")
	}
	list, _, err = s.Directory(ctx, "a")
	if err != nil || len(list) != 3 {
		t.Fatal("old snapshot lost", err)
	}
}

func TestRemovedAccountRestoresHistoryByIdentity(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := domain.Account{ID: "old", Namespace: "old-session", DisplayName: "原账户", UserID: "123", Active: true}
	if err = s.SaveAccount(ctx, a); err != nil {
		t.Fatal(err)
	}
	_ = s.SaveMedia(ctx, []domain.Media{{AccountID: a.ID, ChatID: "c", MessageID: "1", MediaID: "m", LocalPath: "keep.bin", Downloaded: true}})
	_ = s.SaveRule(ctx, domain.Rule{ID: "rule", AccountID: a.ID})
	_ = s.CreateJob(ctx, domain.Job{ID: "job", AccountID: a.ID}, nil)
	if err = s.RemoveAccount(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.Accounts(ctx); len(list) != 0 {
		t.Fatal(list)
	}
	if list, _ := s.Rules(ctx); len(list) != 0 {
		t.Fatal(list)
	}
	if list, _ := s.Jobs(ctx); len(list) != 0 {
		t.Fatal(list)
	}
	if s.SetActiveAccount(ctx, a.ID) == nil {
		t.Fatal("removed account activated")
	}
	b := domain.Account{ID: "new", Namespace: "new-session", DisplayName: "new", UserID: "456"}
	_ = s.SaveAccount(ctx, b)
	b, err = s.RestoreAccount(ctx, b)
	if err != nil || b.ID != "new" {
		t.Fatal("different user restored history", b, err)
	}
	b.UserID = "123"
	b, err = s.RestoreAccount(ctx, b)
	if err != nil || b.ID != "old" || b.Namespace != "new-session" {
		t.Fatal(b, err)
	}
	if list, _ := s.Accounts(ctx); len(list) != 1 || list[0].ID != "old" {
		t.Fatal(list)
	}
	if list, _ := s.Rules(ctx); len(list) != 1 {
		t.Fatal(list)
	}
	if list, _ := s.Jobs(ctx); len(list) != 1 {
		t.Fatal(list)
	}
	if list, _ := s.Media(ctx, "old", "c"); len(list) != 1 || !list[0].Downloaded || list[0].LocalPath != "keep.bin" {
		t.Fatal(list)
	}
}

func TestBrowsingMigrationKeepsOldAccounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	ctx := context.Background()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SaveAccount(ctx, domain.Account{ID: "a", Namespace: "a", Active: true})
	_, err = s.db.Exec(`ALTER TABLE accounts DROP COLUMN removed`)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	list, err := s.Accounts(ctx)
	if err != nil || len(list) != 1 || list[0].ID != "a" {
		t.Fatal(list, err)
	}
}
