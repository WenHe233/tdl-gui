package app

import (
	"context"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/local/tdl-gui/internal/store"
)

func TestConfigDefaultsLegacyAndAtomicUpdate(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	a, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	initial := a.SettingsSnapshot()
	if initial.FileThreads != 8 || initial.FileConcurrency != 4 || initial.PoolSize != 8 {
		t.Fatal(initial)
	}
	updated, err := a.UpdateConfig(ctx, map[string]string{"file.threads": "12", "file.concurrency": "3", "pool.size": "0", "proxy": "", "ntp": "time.example.org", "reconnect.timeout": "30s", "task.delay": "500ms", "cache.max.bytes": "1000"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.FileThreads != 12 || updated.FileConcurrency != 3 || updated.PoolSize != 0 || a.Runner.Proxy() != "" {
		t.Fatal(updated)
	}
	for key, value := range map[string]string{"file.threads": "0", "file.concurrency": "-1", "pool.size": "-1", "retries": "1.5", "task.delay": "-1s", "reconnect.timeout": "oops", "proxy": "invalid", "cache.max.bytes": "-1", "min.free.bytes": "9223372036854775808", "download.root": "relative", "unknown": "x"} {
		values := map[string]string{"retries": "7"}
		values[key] = value
		if _, err := a.UpdateConfig(ctx, values); err == nil {
			t.Fatalf("accepted %s", key)
		}
		if !reflect.DeepEqual(a.SettingsSnapshot(), updated) {
			t.Fatal("partial in-memory save")
		}
	}
	if v, _ := a.Store.GetSetting(ctx, "retries"); v != "" {
		t.Fatalf("partial database save: %q", v)
	}
	reloaded := loadSettings(a.Store, a.Paths)
	if !reflect.DeepEqual(reloaded, updated) {
		t.Fatalf("restart mismatch: %+v != %+v", reloaded, updated)
	}
	if err := a.SetConfig(ctx, "retries", "0"); err != nil {
		t.Fatal(err)
	}
	if a.SettingsSnapshot().Retries != 0 {
		t.Fatal("single-key update was stale")
	}
}

func TestOldSettingsPreservedAndInvalidLegacyValuesUseDefaults(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err = st.SetSettings(context.Background(), map[string]string{"file.concurrency": "2", "file.threads": "-1", "task.delay": "invalid", "proxy": ""}); err != nil {
		t.Fatal(err)
	}
	cfg := loadSettings(st, Paths{})
	if cfg.FileConcurrency != 2 || cfg.FileThreads != 8 || cfg.TaskDelay != "0s" || cfg.Proxy != "" {
		t.Fatal(cfg)
	}
}

func TestSettingsReadsDuringSave(t *testing.T) {
	a, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for n := 0; n < 100; n++ {
			_ = a.SettingsSnapshot()
			_ = a.PreviewService()
			_ = a.Runner.Args("test")
		}
	}()
	for n := 0; n < 10; n++ {
		if _, err = a.UpdateConfig(context.Background(), map[string]string{"file.threads": "8", "proxy": ""}); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}
