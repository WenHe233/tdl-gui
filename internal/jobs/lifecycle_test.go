package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/local/tdl-gui/internal/domain"
	"github.com/local/tdl-gui/internal/store"
	"github.com/local/tdl-gui/internal/tdl"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("TDL_JOB_TEST"); mode != "" {
		if path := os.Getenv("TDL_JOB_ARGS"); path != "" {
			f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				os.Exit(3)
			}
			_ = json.NewEncoder(f).Encode(os.Args)
			f.Close()
		}
		manifest, dir := "", ""
		for i, arg := range os.Args {
			if i+1 < len(os.Args) {
				if arg == "--file" {
					manifest = os.Args[i+1]
				}
				if arg == "--dir" {
					dir = os.Args[i+1]
				}
			}
		}
		var payload struct {
			Messages []struct {
				ID int `json:"id"`
			}
		}
		b, _ := os.ReadFile(manifest)
		_ = json.Unmarshal(b, &payload)
		for _, item := range payload.Messages {
			if mode == "slow" {
				path := filepath.Join(dir, fmt.Sprintf("%d_test.bin.tmp", item.ID))
				f, err := os.Create(path)
				if err != nil {
					os.Exit(4)
				}
				for n := 0; n < 3; n++ {
					_, _ = f.Write([]byte("x"))
					time.Sleep(600 * time.Millisecond)
				}
				f.Close()
				if err = os.Rename(path, strings.TrimSuffix(path, ".tmp")); err != nil {
					os.Exit(5)
				}
				continue
			}
			if mode == "partial" && item.ID != 1 {
				continue
			}
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d_test.bin", item.ID)), []byte("new"), 0600); err != nil {
				os.Exit(2)
			}
		}
		if mode == "partial" {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func jobFixture(t *testing.T) (*Service, *store.Store, *tdl.Runner, domain.Job, string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err = st.SaveAccount(ctx, domain.Account{ID: "a", Namespace: "test"}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "downloads", "same.bin")
	p := domain.DownloadPlan{ID: "p", AccountID: "a", ChatID: "10", SelectedFiles: 2, SelectedBytes: 6}
	for _, id := range []string{"1", "2"} {
		p.Items = append(p.Items, domain.PlanItem{Selected: true, Status: "selected", TargetPath: target, Media: domain.Media{AccountID: "a", ChatID: "10", MessageID: id, MediaID: id, Size: 3}})
	}
	if err = st.SavePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	runner := tdl.New(func(context.Context) (string, error) { return exe, nil }, filepath.Join(root, "tdl"), "", "")
	svc := New(st, runner, filepath.Join(root, "staging"), 0, 1, "", 0, nil)
	j, err := svc.Create(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return svc, st, runner, j, target
}

func TestRetryPreservesCompletedAndRecoversMissingFile(t *testing.T) {
	t.Setenv("TDL_JOB_TEST", "partial")
	svc, st, _, j, target := jobFixture(t)
	ctx := context.Background()
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := svc.Run(ctx, j.ID); err == nil {
		t.Fatal("expected partial failure")
	}
	got, items, err := st.Job(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DoneFiles != 1 || got.FailedFiles != 1 || items[0].TargetPath == target {
		t.Fatalf("job=%+v items=%+v", got, items)
	}
	old, _ := os.ReadFile(target)
	if string(old) != "old" {
		t.Fatal("untracked same-sized file changed")
	}
	t.Setenv("TDL_JOB_TEST", "success")
	if err = svc.Retry(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Wait without requesting shutdown/cancellation.
	for {
		svc.mu.Lock()
		active := len(svc.cancel)
		svc.mu.Unlock()
		if active == 0 {
			break
		}
		select {
		case <-waitCtx.Done():
			t.Fatal("retry timed out")
		case <-time.After(10 * time.Millisecond):
		}
	}
	got, items, err = st.Job(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "completed" || got.DoneFiles != 2 || got.DoneBytes != 6 || items[0].Attempts != 1 || items[1].Attempts != 2 {
		t.Fatalf("job=%+v items=%+v", got, items)
	}
	if err = os.Remove(items[0].TargetPath); err != nil {
		t.Fatal(err)
	}
	if err = svc.Run(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	got, items, _ = st.Job(ctx, j.ID)
	if got.DoneFiles != 2 || got.DoneBytes != 6 || items[0].Attempts != 2 {
		t.Fatalf("missing file not restored: %+v %+v", got, items)
	}
	if _, err = os.Stat(items[0].TargetPath); err != nil {
		t.Fatal(err)
	}
}

func TestDuplicateStartAndCancelWhileWaitingForEngine(t *testing.T) {
	t.Setenv("TDL_JOB_TEST", "success")
	svc, st, runner, j, _ := jobFixture(t)
	ctx := context.Background()
	runner.Acquire()
	defer runner.Release()
	if err := svc.Start(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Start(ctx, j.ID); err == nil {
		t.Fatal("duplicate start accepted")
	}
	if err := svc.Cancel(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := svc.Shutdown(waitCtx); err != nil {
		t.Fatal("cancel blocked", err)
	}
	got, _, err := st.Job(ctx, j.ID)
	if err != nil || got.State != "cancelled" {
		t.Fatalf("job=%+v err=%v", got, err)
	}
	if err = svc.Start(ctx, j.ID); err == nil {
		t.Fatal("cancelled task restarted")
	}
}

func TestMoveNeverReplacesSameSizedFile(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "source"), filepath.Join(dir, "target")
	_ = os.WriteFile(src, []byte("new"), 0600)
	_ = os.WriteFile(dst, []byte("old"), 0600)
	if err := moveComplete(src, dst); err == nil {
		t.Fatal("existing destination replaced")
	}
	b, _ := os.ReadFile(dst)
	if string(b) != "old" {
		t.Fatal("destination changed")
	}
}

func TestChildProcessReportsProgressBeforeFileCloses(t *testing.T) {
	t.Setenv("TDL_JOB_TEST", "slow")
	svc, st, _, job, _ := jobFixture(t)
	var sawLive, sawAggregate, sawZero bool
	svc.events = func(e Event) {
		if e.Type == "job.progress" && e.Job != nil {
			if e.Job.SpeedBytesPerSecond > 0 {
				sawAggregate = true
			}
			if sawAggregate && e.Job.SpeedBytesPerSecond == 0 {
				sawZero = true
			}
			for _, it := range e.Items {
				if it.State == "downloading" && it.DownloadedBytes > 0 && it.DownloadedBytes < it.Size && it.SpeedBytesPerSecond > 0 {
					sawLive = true
				}
			}
		}
	}
	if err := svc.Run(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	if !sawLive || !sawAggregate || !sawZero {
		t.Fatalf("live=%v aggregate=%v reset=%v", sawLive, sawAggregate, sawZero)
	}
	result, _, _ := st.Job(context.Background(), job.ID)
	if result.State != "completed" || result.DoneBytes != result.TotalBytes {
		t.Fatal(result)
	}
}

func TestSettingsSnapshotSurvivesUpdatesAndAutomaticRetries(t *testing.T) {
	t.Setenv("TDL_JOB_TEST", "partial")
	path := filepath.Join(t.TempDir(), "args.jsonl")
	t.Setenv("TDL_JOB_ARGS", path)
	svc, _, _, job, _ := jobFixture(t)
	cfg := domain.Settings{FileThreads: 8, FileConcurrency: 4, PoolSize: 8, Retries: 1, TaskDelay: "0s", ReconnectTimeout: "5m"}
	svc.SettingsSource = func() domain.Settings { return cfg }
	svc.events = func(e Event) {
		if e.Type == "job.updated" {
			cfg.FileThreads = 2
			cfg.FileConcurrency = 1
			cfg.Proxy = "socks5://localhost:1080"
			cfg.Retries = 0
		}
	}
	if err := svc.Run(context.Background(), job.ID); err == nil {
		t.Fatal("expected partial failure")
	}
	t.Setenv("TDL_JOB_TEST", "success")
	if err := svc.Run(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected two attempts and a resume, got %d", len(lines))
	}
	for i, line := range lines {
		var args []string
		if err := json.Unmarshal([]byte(line), &args); err != nil {
			t.Fatal(err)
		}
		flags := map[string]string{}
		for n := 0; n+1 < len(args); n++ {
			if strings.HasPrefix(args[n], "--") {
				flags[args[n]] = args[n+1]
			}
		}
		threads, limit, proxy := "8", "4", ""
		if i == 2 {
			threads, limit, proxy = "2", "1", "socks5://localhost:1080"
		}
		if flags["--threads"] != threads || flags["--limit"] != limit || flags["--proxy"] != proxy || flags["--pool"] != "8" || flags["--reconnect-timeout"] != "5m" || flags["--delay"] != "0s" {
			t.Fatalf("attempt %d: %v", i, flags)
		}
	}
}
