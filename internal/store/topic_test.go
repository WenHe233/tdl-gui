package store

import (
	"context"
	"fmt"
	"github.com/local/tdl-gui/internal/domain"
	"path/filepath"
	"testing"
	"time"
)

func TestTopicMigrationPagingAndUpdatedTarget(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	items := make([]domain.Media, 125)
	for i := range items {
		id := fmt.Sprint(i + 1)
		items[i] = domain.Media{AccountID: "a", ChatID: "c", MessageID: id, MediaID: id, TopicID: "5", Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	}
	if err = s.SaveMedia(ctx, items); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("ALTER TABLE media DROP COLUMN topic_id"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	page, err := s.MediaPage(ctx, "a", "c", 0, 100, "5")
	if err != nil || len(page) != 0 {
		t.Fatal("unknown records leaked into topic", err)
	}
	all, err := s.Media(ctx, "a", "c")
	if err != nil || len(all) != 125 {
		t.Fatal("migration lost records", err)
	}
	if err = s.SaveMedia(ctx, items); err != nil {
		t.Fatal(err)
	}
	page, err = s.MediaPage(ctx, "a", "c", 0, 100, "5")
	if err != nil || len(page) != 100 || page[0].MessageID != "125" {
		t.Fatal("numeric paging", err)
	}
	page, err = s.MediaPage(ctx, "a", "c", 100, 100, "5")
	if err != nil || len(page) != 25 || page[24].MessageID != "1" {
		t.Fatal("second page", err)
	}
	it := domain.JobItem{JobID: "j", ChatID: "c", MessageID: "1", TargetPath: "old"}
	if err = s.CreateJob(ctx, domain.Job{ID: "j"}, []domain.JobItem{it}); err != nil {
		t.Fatal(err)
	}
	it.TargetPath = "actual"
	if err = s.UpdateJobItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	_, got, err := s.Job(ctx, "j")
	if err != nil || got[0].TargetPath != "actual" {
		t.Fatal("actual target not saved", err)
	}
}

func TestRecoverInterruptedJobs(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	j := domain.Job{ID: "running", State: "running", DoneFiles: 1, DoneBytes: 3, TotalFiles: 2, TotalBytes: 6}
	items := []domain.JobItem{{JobID: j.ID, ChatID: "c", MessageID: "1", State: "done", Size: 3}, {JobID: j.ID, ChatID: "c", MessageID: "2", State: "downloading", Size: 3}}
	if err = s.CreateJob(ctx, j, items); err != nil {
		t.Fatal(err)
	}
	if err = s.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	got, it, err := s.Job(ctx, j.ID)
	if err != nil || got.State != "paused" || got.DoneFiles != 1 || it[0].State != "done" || it[1].State != "queued" {
		t.Fatal(got, it, err)
	}
}
