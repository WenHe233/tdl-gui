package jobs

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/local/tdl-gui/internal/domain"
)

type progressSnapshot struct {
	job   domain.Job
	items []domain.JobItem
}
type speedSample struct {
	at    time.Time
	bytes int64
}
type speedMeter struct {
	samples []speedSample
	last    int64
	total   int64
}

func (m *speedMeter) reset(at time.Time, n int64) {
	m.last = n
	m.total = 0
	m.samples = []speedSample{{at: at}}
}
func (m *speedMeter) sample(at time.Time, n int64) float64 {
	if len(m.samples) == 0 || n < m.last {
		m.reset(at, n)
		return 0
	}
	m.total += n - m.last
	m.last = n
	m.samples = append(m.samples, speedSample{at, m.total})
	cutoff := at.Add(-2 * time.Second)
	for len(m.samples) > 2 && !m.samples[1].at.After(cutoff) {
		m.samples = m.samples[1:]
	}
	first := m.samples[0]
	elapsed := at.Sub(first.at).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return float64(m.total-first.bytes) / elapsed
}

// Match only the current manifest. Temporary and final names represent one file.
// Missing/unreadable entries are absent, so a transient failure cannot reset a meter.
func readProgressFiles(dir string, items []domain.JobItem) (map[string]int64, map[string]bool) {
	wanted := map[string]int64{}
	for _, it := range items {
		wanted[it.MessageID] = it.Size
	}
	values, finished := map[string]int64{}, map[string]bool{}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		id, _, ok := strings.Cut(entry.Name(), "_")
		limit, want := wanted[id]
		if !ok || !want {
			continue
		}
		n, err := liveFileSize(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".tmp") && n == limit {
			finished[id] = true
		}
		n = min(n, limit)
		if previous, exists := values[id]; !exists || n > previous {
			values[id] = n
		}
	}
	return values, finished
}
func itemBytes(dir string, items []domain.JobItem) map[string]int64 {
	values, _ := readProgressFiles(dir, items)
	return values
}

func (s *Service) sampleProgress(dir string, job domain.Job, items []domain.JobItem, meters map[string]*speedMeter, at time.Time) {
	values, finished := readProgressFiles(dir, items)
	snapshot := progressSnapshot{job: job, items: append([]domain.JobItem(nil), items...)}
	snapshot.job.SpeedBytesPerSecond = 0
	var transferred int64
	for i := range snapshot.items {
		it := &snapshot.items[i]
		meter := meters[it.MessageID]
		n, sampled := values[it.MessageID]
		if !sampled {
			n = meter.last
		}
		it.DownloadedBytes = n
		it.SpeedBytesPerSecond = meter.sample(at, n)
		transferred += meter.total
		if n == 0 {
			it.State = "queued"
		} else {
			it.State = "downloading"
		}
		if finished[it.MessageID] {
			it.State = "verifying"
			it.SpeedBytesPerSecond = 0
		}
		snapshot.job.DoneBytes += n
	}
	// Keep recently completed file bytes in the aggregate speed window.
	total := meters[""]
	if total == nil {
		total = &speedMeter{}
		start := at
		for id, m := range meters {
			if id != "" && len(m.samples) > 0 && m.samples[0].at.Before(start) {
				start = m.samples[0].at
			}
		}
		total.reset(start, 0)
		meters[""] = total
	}
	snapshot.job.SpeedBytesPerSecond = total.sample(at, transferred)
	snapshot.job.DoneBytes = min(snapshot.job.DoneBytes, snapshot.job.TotalBytes)
	s.progressMu.Lock()
	if s.progress == nil {
		s.progress = map[string]progressSnapshot{}
	}
	s.progress[job.ID] = snapshot
	s.progressMu.Unlock()
	s.emit(Event{Type: "job.progress", JobID: job.ID, AccountID: job.AccountID, Job: &snapshot.job, Items: snapshot.items})
}
func (s *Service) clearProgress(id string) {
	s.progressMu.Lock()
	delete(s.progress, id)
	s.progressMu.Unlock()
}
func (s *Service) WithProgress(job domain.Job, items []domain.JobItem) (domain.Job, []domain.JobItem) {
	s.progressMu.RLock()
	snapshot, ok := s.progress[job.ID]
	s.progressMu.RUnlock()
	if ok && job.State == "running" {
		job.DoneBytes = snapshot.job.DoneBytes
		job.SpeedBytesPerSecond = snapshot.job.SpeedBytesPerSecond
		byID := map[string]domain.JobItem{}
		for _, it := range snapshot.items {
			byID[it.MessageID] = it
		}
		for i, it := range items {
			if v, ok := byID[it.MessageID]; ok {
				items[i].DownloadedBytes = v.DownloadedBytes
				items[i].SpeedBytesPerSecond = v.SpeedBytesPerSecond
				items[i].State = v.State
			}
		}
	}
	for i := range items {
		if items[i].State == "done" {
			items[i].DownloadedBytes = items[i].Size
		}
	}
	return job, items
}
func (s *Service) waitWithProgress(process interface{ Wait() error }, dir string, job domain.Job, items []domain.JobItem, meters map[string]*speedMeter) error {
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	defer func() {
		s.progressMu.Lock()
		snapshot, ok := s.progress[job.ID]
		if ok {
			snapshot.job.SpeedBytesPerSecond = 0
			snapshot.items = append([]domain.JobItem(nil), snapshot.items...)
			for i := range snapshot.items {
				snapshot.items[i].SpeedBytesPerSecond = 0
			}
			s.progress[job.ID] = snapshot
		}
		s.progressMu.Unlock()
		if ok {
			s.emit(Event{Type: "job.progress", JobID: job.ID, AccountID: job.AccountID, Job: &snapshot.job, Items: snapshot.items})
		}
		s.clearProgress(job.ID)
	}()
	for {
		select {
		case err := <-done:
			s.sampleProgress(dir, job, items, meters, time.Now())
			return err
		case at := <-ticker.C:
			s.sampleProgress(dir, job, items, meters, at)
		}
	}
}

func newProgressMeters(dir string, items []domain.JobItem) map[string]*speedMeter {
	initial := itemBytes(dir, items)
	meters := map[string]*speedMeter{}
	now := time.Now()
	for _, it := range items {
		meter := &speedMeter{}
		meter.reset(now, initial[it.MessageID])
		meters[it.MessageID] = meter
	}
	return meters
}
