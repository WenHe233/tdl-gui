package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSettingsTransactionRollsBackOnWriteFailure(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.SetSetting(ctx, "file.threads", "8"); err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`CREATE TRIGGER reject_setting BEFORE INSERT ON settings WHEN NEW.key='retries' BEGIN SELECT RAISE(FAIL,'test failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetSettings(ctx, map[string]string{"file.threads": "12", "retries": "7"}); err == nil {
		t.Fatal("expected write failure")
	}
	got, err := s.GetSetting(ctx, "file.threads")
	if err != nil || got != "8" {
		t.Fatalf("partial write: %q %v", got, err)
	}
}
