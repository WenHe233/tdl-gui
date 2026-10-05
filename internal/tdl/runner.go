package tdl

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type Engine interface {
	Path(context.Context) (string, error)
}
type Runner struct {
	executable func(context.Context) (string, error)
	storage    string
	proxy      string
	ntp        string
	home       string
	gate       chan struct{}
	networkMu  sync.RWMutex
}

func New(executable func(context.Context) (string, error), storage, proxy, ntp string) *Runner {
	return &Runner{executable: executable, storage: storage, proxy: proxy, ntp: ntp, home: filepath.Join(filepath.Dir(storage), "home"), gate: make(chan struct{}, 1)}
}
func (r *Runner) Acquire() { r.gate <- struct{}{} }
func (r *Runner) Release() { <-r.gate }
func (r *Runner) AcquireContext(ctx context.Context) error {
	select {
	case r.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			r.Release()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (r *Runner) Args(namespace string, args ...string) []string {
	r.networkMu.RLock()
	defer r.networkMu.RUnlock()
	base := []string{"--storage", "type=file,path=" + r.storage, "--ns", namespace, "--disable-progress-ps"}
	if r.proxy != "" {
		base = append(base, "--proxy", r.proxy)
	}
	if r.ntp != "" {
		base = append(base, "--ntp", r.ntp)
	}
	return append(base, args...)
}
func (r *Runner) SetProxy(proxy string) {
	r.networkMu.Lock()
	defer r.networkMu.Unlock()
	r.proxy = proxy
}
func (r *Runner) Command(ctx context.Context, namespace string, args ...string) (*exec.Cmd, error) {
	path, err := r.executable(ctx)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(r.home, 0o700); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, r.Args(namespace, args...)...)
	env := make([]string, 0, len(os.Environ())+1)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(value), "USERPROFILE=") {
			env = append(env, value)
		}
	}
	cmd.Env = append(env, "USERPROFILE="+r.home)
	return cmd, nil
}
func (r *Runner) Run(ctx context.Context, namespace string, args ...string) ([]byte, error) {
	if err := r.AcquireContext(ctx); err != nil {
		return nil, err
	}
	defer r.Release()
	cmd, err := r.Command(ctx, namespace, args...)
	if err != nil {
		return nil, err
	}
	var stdout, stderr bytes.Buffer
	configureBackground(cmd)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		return stdout.Bytes(), fmt.Errorf("tdl: %w: %s", err, detail)
	}
	return stdout.Bytes(), nil
}
func (r *Runner) Interactive(ctx context.Context, namespace string, args ...string) error {
	if err := r.AcquireContext(ctx); err != nil {
		return err
	}
	defer r.Release()
	cmd, err := r.Command(ctx, namespace, args...)
	if err != nil {
		return err
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type Process struct {
	cmd     *exec.Cmd
	release func()
	once    sync.Once
}

func (p *Process) Wait() error { err := p.cmd.Wait(); p.once.Do(p.release); return err }
func (r *Runner) Stream(ctx context.Context, namespace string, stdout, stderr io.Writer, args ...string) (*Process, error) {
	if err := r.AcquireContext(ctx); err != nil {
		return nil, err
	}
	cmd, err := r.Command(ctx, namespace, args...)
	if err != nil {
		r.Release()
		return nil, err
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	configureBackground(cmd)
	if err = cmd.Start(); err != nil {
		r.Release()
		return nil, err
	}
	return &Process{cmd: cmd, release: r.Release}, nil
}
