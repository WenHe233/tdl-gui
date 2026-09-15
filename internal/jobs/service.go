package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/local/tdl-gui/internal/domain"
	"github.com/local/tdl-gui/internal/idgen"
	"github.com/local/tdl-gui/internal/tdl"
)

type Store interface {
	Plan(context.Context, string) (domain.DownloadPlan, error)
	CreateJob(context.Context, domain.Job, []domain.JobItem) error
	UpdateJob(context.Context, domain.Job) error
	UpdateJobItem(context.Context, domain.JobItem) error
	Jobs(context.Context) ([]domain.Job, error)
	Job(context.Context, string) (domain.Job, []domain.JobItem, error)
	Account(context.Context, string) (domain.Account, error)
	MarkDownloaded(context.Context, string, string, string, string, int64) error
}

type Event struct {
	Type    string          `json:"type"`
	JobID   string          `json:"jobId"`
	Message string          `json:"message,omitempty"`
	Job     *domain.Job     `json:"job,omitempty"`
	Item    *domain.JobItem `json:"item,omitempty"`
}
type Service struct {
	store       Store
	runner      *tdl.Runner
	staging     string
	retries     int
	concurrency int
	delay       string
	minFree     int64
	mu          sync.Mutex
	cancel      map[string]context.CancelFunc
	cancelled   map[string]bool
	events      func(Event)
}

func New(store Store, runner *tdl.Runner, staging string, retries, concurrency int, delay string, minFree int64, events func(Event)) *Service {
	if retries < 0 {
		retries = 0
	}
	if concurrency < 1 {
		concurrency = 2
	}
	return &Service{store: store, runner: runner, staging: staging, retries: retries, concurrency: concurrency, delay: delay, minFree: minFree, cancel: map[string]context.CancelFunc{}, cancelled: map[string]bool{}, events: events}
}
func (s *Service) emit(e Event) {
	if s.events != nil {
		s.events(e)
	}
}

func (s *Service) Create(ctx context.Context, planID string) (domain.Job, error) {
	p, err := s.store.Plan(ctx, planID)
	if err != nil {
		return domain.Job{}, err
	}
	now := time.Now().UTC()
	j := domain.Job{ID: idgen.New("job"), PlanID: p.ID, AccountID: p.AccountID, ChatID: p.ChatID, State: "queued", TotalFiles: p.SelectedFiles, TotalBytes: p.SelectedBytes, CreatedAt: now, UpdatedAt: now}
	var items []domain.JobItem
	for _, pi := range p.Items {
		if !pi.Selected {
			continue
		}
		items = append(items, domain.JobItem{JobID: j.ID, MediaID: pi.Media.MediaID, ChatID: pi.Media.ChatID, MessageID: pi.Media.MessageID, TargetPath: pi.TargetPath, State: "queued", Size: pi.Media.Size})
	}
	if err = s.store.CreateJob(ctx, j, items); err != nil {
		return domain.Job{}, err
	}
	return j, nil
}

func (s *Service) Run(parent context.Context, id string) error {
	j, items, err := s.store.Job(parent, id)
	if err != nil {
		return err
	}
	a, err := s.store.Account(parent, j.AccountID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	s.mu.Lock()
	if _, ok := s.cancel[id]; ok {
		s.mu.Unlock()
		cancel()
		return fmt.Errorf("job %s is already running", id)
	}
	s.cancel[id] = cancel
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); delete(s.cancel, id); delete(s.cancelled, id); s.mu.Unlock() }()
	j.State = "running"
	j.Error = ""
	_ = s.store.UpdateJob(context.Background(), j)
	s.emit(Event{Type: "job.updated", JobID: id, Job: &j})
	jobDir := filepath.Join(s.staging, id)
	if err = os.MkdirAll(jobDir, 0o700); err != nil {
		return s.finishError(j, err)
	}
	pending := make([]domain.JobItem, 0, len(items))
	for _, it := range items {
		if it.State == "done" {
			continue
		}
		it.State = "queued"
		it.Error = ""
		_ = s.store.UpdateJobItem(context.Background(), it)
		pending = append(pending, it)
	}
	for attempt := 0; attempt <= s.retries && len(pending) > 0; attempt++ {
		if ctx.Err() != nil {
			j.State, j.Error = s.stoppedState(id)
			_ = s.store.UpdateJob(context.Background(), j)
			s.emit(Event{Type: "job.updated", JobID: id, Job: &j})
			return nil
		}
		for n := range pending {
			pending[n].State = "downloading"
			pending[n].Attempts++
			_ = s.store.UpdateJobItem(context.Background(), pending[n])
			s.emit(Event{Type: "item.updated", JobID: id, Item: &pending[n]})
		}
		manifest := filepath.Join(jobDir, fmt.Sprintf("manifest-%d.json", attempt))
		if err = writeManifest(manifest, j.ChatID, pending); err != nil {
			return s.finishError(j, err)
		}
		var logs bytes.Buffer
		writer := io.MultiWriter(&logs, &eventWriter{emit: func(line string) { s.emit(Event{Type: "job.log", JobID: id, Message: line}) }})
		if diskErr := checkDisk(pending, s.minFree); diskErr != nil {
			j.State = "paused"
			j.Error = diskErr.Error()
			_ = s.store.UpdateJob(context.Background(), j)
			s.emit(Event{Type: "job.updated", JobID: id, Job: &j})
			return diskErr
		}
		downloadArgs := []string{"download", "--file", manifest, "--dir", jobDir, "--template", `{{ .MessageID }}_{{ filenamify .FileName }}`, "--threads", "1", "--limit", strconv.Itoa(s.concurrency), "--continue"}
		if s.delay != "" && s.delay != "0s" {
			downloadArgs = append(downloadArgs, "--delay", s.delay)
		}
		cmd, startErr := s.runner.Stream(ctx, a.Namespace, writer, writer, downloadArgs...)
		if startErr == nil {
			err = s.waitWithProgress(cmd, jobDir, j)
		} else {
			err = startErr
		}
		var failed []domain.JobItem
		for _, it := range pending {
			found := findDownload(jobDir, it.MessageID, it.Size)
			if found != "" {
				target := uniqueTarget(it.TargetPath, it.MessageID, it.Size)
				if moveErr := moveComplete(found, target); moveErr == nil {
					it.State = "done"
					it.StagingPath = ""
					it.Error = ""
					j.DoneFiles++
					j.DoneBytes += it.Size
					_ = s.store.MarkDownloaded(context.Background(), j.AccountID, j.ChatID, it.MediaID, target, it.Size)
				} else {
					it.State = "failed"
					it.Error = moveErr.Error()
					failed = append(failed, it)
				}
			} else {
				it.State = "failed"
				if err != nil {
					it.Error = err.Error()
				} else {
					it.Error = "tdl 未生成预期大小的文件"
				}
				failed = append(failed, it)
			}
			_ = s.store.UpdateJobItem(context.Background(), it)
			s.emit(Event{Type: "item.updated", JobID: id, Item: &it})
		}
		pending = failed
		j.FailedFiles = len(pending)
		_ = s.store.UpdateJob(context.Background(), j)
		if len(pending) > 0 && attempt < s.retries {
			wait := time.Duration(1<<attempt) * time.Second
			select {
			case <-ctx.Done():
			case <-time.After(wait):
			}
		}
	}
	if ctx.Err() != nil {
		j.State, j.Error = s.stoppedState(id)
	} else if len(pending) > 0 {
		j.State = "failed"
		j.Error = fmt.Sprintf("%d 个文件下载失败", len(pending))
	} else {
		j.State = "completed"
		j.Error = ""
		j.FailedFiles = 0
	}
	_ = s.store.UpdateJob(context.Background(), j)
	s.emit(Event{Type: "job.updated", JobID: id, Job: &j})
	if j.State == "failed" {
		return errors.New(j.Error)
	}
	return nil
}

func (s *Service) waitWithProgress(process interface{ Wait() error }, dir string, job domain.Job) error {
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-ticker.C:
			snapshot := job
			snapshot.DoneBytes += stagingBytes(dir)
			if snapshot.DoneBytes > snapshot.TotalBytes { snapshot.DoneBytes = snapshot.TotalBytes }
			s.emit(Event{Type: "job.updated", JobID: job.ID, Job: &snapshot})
		}
	}
}
func stagingBytes(dir string) int64 { entries,_:=os.ReadDir(dir);var total int64;for _,entry:=range entries{if entry.IsDir()||strings.HasPrefix(entry.Name(),"manifest-"){continue};if info,err:=entry.Info();err==nil{total+=info.Size()}};return total }

func (s *Service) Pause(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.cancel[id]; ok {
		c()
		return true
	}
	return false
}
func (s *Service) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	cancels:=make([]context.CancelFunc,0,len(s.cancel))
	for _,cancel:=range s.cancel{cancels=append(cancels,cancel)}
	s.mu.Unlock()
	for _,cancel:=range cancels{cancel()}
	ticker:=time.NewTicker(50*time.Millisecond);defer ticker.Stop()
	for { s.mu.Lock();running:=len(s.cancel);s.mu.Unlock();if running==0{return nil};select{case<-ctx.Done():return ctx.Err();case<-ticker.C:} }
}
func (s *Service) Cancel(ctx context.Context, id string) error {
	s.mu.Lock()
	s.cancelled[id] = true
	s.mu.Unlock()
	s.Pause(id)
	j, items, err := s.store.Job(ctx, id)
	if err != nil {
		return err
	}
	j.State = "cancelled"
	j.Error = "任务已取消"
	for _, it := range items {
		if it.State != "done" {
			it.State = "cancelled"
			_ = s.store.UpdateJobItem(ctx, it)
		}
	}
	return s.store.UpdateJob(ctx, j)
}
func (s *Service) stoppedState(id string) (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelled[id] {
		return "cancelled", "任务已取消"
	}
	return "paused", "任务已暂停"
}

func checkDisk(items []domain.JobItem, minFree int64) error {
	required := map[string]int64{}
	paths := map[string]string{}
	for _, it := range items {
		volume := filepath.VolumeName(it.TargetPath)
		required[volume] += it.Size
		paths[volume] = filepath.Dir(it.TargetPath)
	}
	for volume, need := range required {
		free, err := freeSpace(paths[volume])
		if err != nil {
			return fmt.Errorf("检查磁盘空间 %s: %w", volume, err)
		}
		if free < uint64(need+minFree) {
			return fmt.Errorf("磁盘 %s 空间不足：需要 %d 字节并保留 %d 字节，当前可用 %d 字节", volume, need, minFree, free)
		}
	}
	return nil
}
func (s *Service) finishError(j domain.Job, err error) error {
	j.State = "failed"
	j.Error = err.Error()
	_ = s.store.UpdateJob(context.Background(), j)
	s.emit(Event{Type: "job.updated", JobID: j.ID, Job: &j})
	return err
}

func writeManifest(path, chatID string, items []domain.JobItem) error {
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid numeric chat id %q", chatID)
	}
	msgs := make([]map[string]any, 0, len(items))
	for _, it := range items {
		mid, err := strconv.Atoi(it.MessageID)
		if err != nil {
			return fmt.Errorf("invalid message id %q", it.MessageID)
		}
		msgs = append(msgs, map[string]any{"id": mid, "type": "message", "file": filepath.Base(it.TargetPath)})
	}
	b, err := json.Marshal(map[string]any{"id": id, "messages": msgs})
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}
func findDownload(dir, messageID string, size int64) string {
	entries, _ := os.ReadDir(dir)
	prefix := messageID + "_"
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), prefix) || strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if st, err := e.Info(); err == nil && st.Size() == size {
			return p
		}
	}
	return ""
}
func uniqueTarget(path, messageID string, size int64) string {
	if st, err := os.Stat(path); err != nil {
		return path
	} else if st.Size() == size {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for n:=1;;n++{suffix:="-"+messageID;if n>1{suffix+=fmt.Sprintf("-%d",n)};candidate:=base+suffix+ext;if st,err:=os.Stat(candidate);err!=nil||st.Size()==size{return candidate}}
}
func moveComplete(src, dst string) error {
	if src == dst {
		return nil
	}
	if source,targetErr:=os.Stat(src);targetErr==nil{if target,destErr:=os.Stat(dst);destErr==nil&&source.Size()==target.Size(){return os.Remove(src)}}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	_ = os.Remove(tmp)
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	out, err := os.Create(tmp)
	if err != nil {
		in.Close()
		return err
	}
	_, cpErr := io.Copy(out, in)
	closeIn := in.Close()
	closeOut := out.Close()
	if cpErr != nil {
		return cpErr
	}
	if closeIn != nil {
		return closeIn
	}
	if closeOut != nil {
		return closeOut
	}
	if err = os.Rename(tmp, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

type eventWriter struct {
	mu   sync.Mutex
	buf  string
	emit func(string)
}

func (w *eventWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf += string(p)
	for {
		n := strings.IndexByte(w.buf, '\n')
		if n < 0 {
			break
		}
		line := strings.TrimSpace(w.buf[:n])
		w.buf = w.buf[n+1:]
		if line != "" {
			w.emit(line)
		}
	}
	return len(p), nil
}
