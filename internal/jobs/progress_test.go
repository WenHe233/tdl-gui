package jobs

import (
	"github.com/local/tdl-gui/internal/domain"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSpeedWindowAndRetry(t *testing.T) {
	now := time.Unix(0, 0)
	m := &speedMeter{}
	m.reset(now, 500)
	if n := m.sample(now.Add(time.Second), 1500); n != 1000 {
		t.Fatal(n)
	}
	if n := m.sample(now.Add(2*time.Second), 2500); n != 1000 {
		t.Fatal(n)
	}
	m.sample(now.Add(3*time.Second), 2500)
	if n := m.sample(now.Add(4*time.Second), 2500); n != 0 {
		t.Fatal("stalled speed", n)
	}
	if n := m.sample(now.Add(5*time.Second), 0); n != 0 {
		t.Fatal("retry spike", n)
	}
	if n := m.sample(now.Add(6*time.Second), 100); n != 100 {
		t.Fatal(n)
	}
}
func TestProgressUsesOnlyManifestAndDoesNotCountRenameTwice(t *testing.T) {
	dir := t.TempDir()
	items := []domain.JobItem{{JobID: "j", MessageID: "1", Size: 100}, {JobID: "j", MessageID: "2", Size: 200}}
	for name, n := range map[string]int{"1_a.tmp": 50, "1_a": 50, "2_b.tmp": 100, "9_old": 300, "manifest-0.json": 80} {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, n), 0600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Unix(0, 0)
	meters := map[string]*speedMeter{"1": {}, "2": {}}
	for _, m := range meters {
		m.reset(now, 0)
	}
	s := &Service{}
	job := domain.Job{ID: "j", AccountID: "a", State: "running", TotalBytes: 400, DoneBytes: 100}
	s.sampleProgress(dir, job, items, meters, now.Add(time.Second))
	got, details := s.WithProgress(job, append([]domain.JobItem(nil), items...))
	if got.DoneBytes != 250 || got.SpeedBytesPerSecond != 150 || details[1].SpeedBytesPerSecond != 100 {
		t.Fatal(got, details)
	}
	os.Remove(filepath.Join(dir, "1_a.tmp"))
	s.sampleProgress(dir, job, items, meters, now.Add(2*time.Second))
	got, _ = s.WithProgress(job, nil)
	if got.DoneBytes != 250 {
		t.Fatal("rename changed progress", got)
	}
	s.clearProgress(job.ID)
	got, details = s.WithProgress(job, items)
	if got.SpeedBytesPerSecond != 0 || details[0].SpeedBytesPerSecond != 0 {
		t.Fatal("stale speed", got, details)
	}
}

func TestCompletedSmallFilesKeepAggregateSpeed(t *testing.T) {
	dir := t.TempDir()
	now := time.Unix(0, 0)
	items := []domain.JobItem{{MessageID: "1", Size: 100}, {MessageID: "2", Size: 200}}
	meters := map[string]*speedMeter{"1": {}, "2": {}}
	for _, m := range meters {
		m.reset(now, 0)
	}
	for name, n := range map[string]int{"1_a": 100, "2_b": 200} {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, n), 0600); err != nil {
			t.Fatal(err)
		}
	}
	svc := &Service{}
	job := domain.Job{ID: "j", State: "running", TotalBytes: 300}
	svc.sampleProgress(dir, job, items, meters, now.Add(time.Second))
	got, details := svc.WithProgress(job, append([]domain.JobItem(nil), items...))
	if got.SpeedBytesPerSecond != 300 || got.DoneBytes != 300 {
		t.Fatal(got)
	}
	if details[0].State != "verifying" || details[0].SpeedBytesPerSecond != 0 {
		t.Fatal(details)
	}
	// Missing samples must not roll progress back or create a recovery spike.
	if err := os.Remove(filepath.Join(dir, "1_a")); err != nil {
		t.Fatal(err)
	}
	svc.sampleProgress(dir, job, items, meters, now.Add(2*time.Second))
	got, _ = svc.WithProgress(job, nil)
	if got.DoneBytes != 300 {
		t.Fatal(got)
	}
	svc.sampleProgress(dir, job, items, meters, now.Add(4*time.Second))
	got, _ = svc.WithProgress(job, nil)
	if got.SpeedBytesPerSecond != 0 {
		t.Fatal(got)
	}
}

func TestBoundedLogTailIncludesUnterminatedEngineError(t *testing.T) {
	var lines []string
	w := &eventWriter{emit: func(line string) { lines = append(lines, line) }}
	_, _ = w.Write([]byte(strings.Repeat("x", 100000)))
	if len(w.buf) > 65536 {
		t.Fatal("unbounded pending log")
	}
	_, _ = w.Write([]byte("\nnot authorized. please login first"))
	w.Flush()
	if len(w.Tail()) > 8192 || !strings.HasSuffix(w.Tail(), "not authorized. please login first") || len(lines) != 2 {
		t.Fatal("missing bounded diagnostic")
	}
}
