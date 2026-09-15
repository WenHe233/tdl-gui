package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/local/tdl-gui/internal/domain"
)

func TestStorePersistsCoreWorkflow(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a := domain.Account{ID: "a", Namespace: "one", DisplayName: "账户", Active: true}
	if err = s.SaveAccount(ctx, a); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ActiveAccount(ctx); err != nil || got.ID != "a" {
		t.Fatalf("account=%+v err=%v", got, err)
	}
	chat := domain.Chat{AccountID: "a", ID: "10", Type: "channel", VisibleName: "频道", Topics: []domain.Topic{{ID: "1", Title: "主题"}}}
	if err = s.UpsertChats(ctx, []domain.Chat{chat}); err != nil {
		t.Fatal(err)
	}
	m := domain.Media{AccountID: "a", ChatID: "10", MessageID: "2", MediaID: "m", Kind: "photo", FileName: "图.jpg", Size: 4, Date: time.Now()}
	if err = s.SaveMedia(ctx, []domain.Media{m}); err != nil {
		t.Fatal(err)
	}
	if err = s.SetScanCursor(ctx, "a", "10", "", "42"); err != nil {
		t.Fatal(err)
	}
	if cursor, err := s.ScanCursor(ctx, "a", "10", ""); err != nil || cursor != "42" {
		t.Fatalf("cursor=%s err=%v", cursor, err)
	}
	media, err := s.Media(ctx, "a", "10")
	if err != nil || len(media) != 1 || media[0].MediaID != "m" {
		t.Fatalf("media=%+v err=%v", media, err)
	}
	r := domain.Rule{ID: "r", AccountID: "a", ChatID: "10", RootDir: t.TempDir()}
	if err = s.SaveRule(ctx, r); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Rule(ctx, "r"); err != nil || got.ChatID != "10" {
		t.Fatalf("rule=%+v err=%v", got, err)
	}
	p := domain.DownloadPlan{ID: "p", AccountID: "a", ChatID: "10", CreatedAt: time.Now()}
	if err = s.SavePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	j := domain.Job{ID: "j", PlanID: "p", AccountID: "a", ChatID: "10", State: "queued", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	it := domain.JobItem{JobID: "j", MediaID: "m", ChatID: "10", MessageID: "2", TargetPath: "x", State: "queued", Size: 4}
	if err = s.CreateJob(ctx, j, []domain.JobItem{it}); err != nil {
		t.Fatal(err)
	}
	got, items, err := s.Job(ctx, "j")
	if err != nil || got.State != "queued" || len(items) != 1 {
		t.Fatalf("job=%+v items=%+v err=%v", got, items, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if accounts, readErr := reopened.Accounts(ctx); readErr != nil || len(accounts) != 1 || accounts[0].ID != "a" {
		t.Fatalf("reopened accounts=%+v err=%v", accounts, readErr)
	}
	if persisted, readErr := reopened.Media(ctx, "a", "10"); readErr != nil || len(persisted) != 1 || persisted[0].MediaID != "m" {
		t.Fatalf("reopened media=%+v err=%v", persisted, readErr)
	}
}

func TestReplaceMediaRemovesStaleAndPreservesMatchingDownloads(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now()
	old := []domain.Media{
		{AccountID: "a", ChatID: "c", MessageID: "1", MediaID: "same", Kind: "photo", FileName: "one.jpg", Size: 10, Date: now, ThumbPath: "thumb.jpg", LocalPath: "local.jpg", Downloaded: true},
		{AccountID: "a", ChatID: "c", MessageID: "2", MediaID: "deleted", Kind: "photo", FileName: "two.jpg", Size: 20, Date: now},
	}
	if err = s.SaveMedia(ctx, old); err != nil {
		t.Fatal(err)
	}
	fresh := []domain.Media{{AccountID: "a", ChatID: "c", MessageID: "1", MediaID: "same", Kind: "photo", FileName: "renamed.jpg", Size: 10, Date: now}}
	if err = s.ReplaceMedia(ctx, "a", "c", fresh); err != nil {
		t.Fatal(err)
	}
	got, err := s.Media(ctx, "a", "c")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].MessageID != "1" || got[0].ThumbPath != "thumb.jpg" || got[0].LocalPath != "local.jpg" || !got[0].Downloaded {
		t.Fatalf("unexpected replacement: %+v", got)
	}

	changed := []domain.Media{{AccountID: "a", ChatID: "c", MessageID: "1", MediaID: "new", Kind: "photo", FileName: "new.jpg", Size: 11, Date: now}}
	if err = s.SaveMedia(ctx, changed); err != nil {
		t.Fatal(err)
	}
	got, err = s.Media(ctx, "a", "c")
	if err != nil || len(got) != 1 {
		t.Fatalf("media=%+v err=%v", got, err)
	}
	if got[0].ThumbPath != "" || got[0].LocalPath != "" || got[0].Downloaded {
		t.Fatalf("changed media retained stale local state: %+v", got[0])
	}
}
