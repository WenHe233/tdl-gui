package jobs

import (
	"os"
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

// Match only the current manifest. The .tmp and final names represent one file.
func itemBytes(dir string, items []domain.JobItem) map[string]int64 {
	wanted := map[string]int64{}
	for _, it := range items {
		wanted[it.MessageID] = it.Size
	}
	out := map[string]int64{}
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
		info, err := entry.Info()
		if err != nil {
			continue
		}
		n := info.Size()
		if n > limit {
			n = limit
		}
		if n > out[id] {
			out[id] = n
		}
	}
	return out
}

func (s *Service) sampleProgress(dir string, job domain.Job, items []domain.JobItem, meters map[string]*speedMeter, at time.Time) {
	values := itemBytes(dir, items)
	finished := map[string]bool{}
	entries, _ := os.ReadDir(dir)
	wanted := map[string]int64{}
	for _, it := range items {
		wanted[it.MessageID] = it.Size
	}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}
		id, _, ok := strings.Cut(entry.Name(), "_")
		size, match := wanted[id]
		if !ok || !match {
			continue
		}
		if info, err := entry.Info(); err == nil && info.Size() == size {
			finished[id] = true
		}
	}
	snapshot := progressSnapshot{job: job, items: append([]domain.JobItem(nil), items...)}
	snapshot.job.SpeedBytesPerSecond = 0
	for i := range snapshot.items {
		it := &snapshot.items[i]
		n := values[it.MessageID]
		it.DownloadedBytes = n
		it.SpeedBytesPerSecond = meters[it.MessageID].sample(at, n)
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
		snapshot.job.SpeedBytesPerSecond += it.SpeedBytesPerSecond
	}
	if snapshot.job.DoneBytes > snapshot.job.TotalBytes {
		snapshot.job.DoneBytes = snapshot.job.TotalBytes
	}
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
func (s *Service) waitWithProgress(process interface{ Wait() error }, dir string, job domain.Job, items []domain.JobItem) error {
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	defer func() {
		s.progressMu.Lock()
		snapshot, ok := s.progress[job.ID]
		if ok {
			snapshot.job.SpeedBytesPerSecond = 0
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
	initial := itemBytes(dir, items)
	meters := map[string]*speedMeter{}
	now := time.Now()
	for _, it := range items {
		m := &speedMeter{}
		m.reset(now, initial[it.MessageID])
		meters[it.MessageID] = m
	}
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
