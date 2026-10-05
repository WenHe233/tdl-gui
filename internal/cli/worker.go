package cli

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/local/tdl-gui/internal/app"
	"github.com/local/tdl-gui/internal/authui"
	"github.com/local/tdl-gui/internal/catalog"
	"github.com/local/tdl-gui/internal/domain"
	"github.com/local/tdl-gui/internal/jobs"
	"github.com/local/tdl-gui/internal/planner"
	"github.com/local/tdl-gui/internal/preview"
	"github.com/local/tdl-gui/internal/updates"
	"github.com/spf13/cobra"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
	Method  string    `json:"method,omitempty"`
	Params  any       `json:"params,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}
type rpcServer struct {
	updater        *updates.Service
	version        string
	opMu           sync.Mutex
	operations     map[string]*mediaOperation
	requests       sync.WaitGroup
	cancelRequests context.CancelFunc
	previewMu      sync.RWMutex
	mu             sync.Mutex
	enc            *json.Encoder
	app            *app.Application
	auth           *authui.Manager
}

func (r *rpcServer) write(v any) { r.mu.Lock(); defer r.mu.Unlock(); _ = r.enc.Encode(v) }
func (r *rpcServer) event(e jobs.Event) {
	r.write(rpcResponse{JSONRPC: "2.0", Method: "event", Params: e})
}
func (r *rpcServer) loginEvent(e authui.Event) {
	r.write(rpcResponse{JSONRPC: "2.0", Method: "event", Params: e})
}
func (s *rootState) workerCmd() *cobra.Command {
	return &cobra.Command{Use: "worker", Short: "启动 GUI 使用的 JSON-RPC worker", RunE: func(cmd *cobra.Command, args []string) error {
		executable, e := os.Executable()
		if e != nil {
			return e
		}
		srv := &rpcServer{enc: json.NewEncoder(os.Stdout), version: s.version, updater: updates.New(s.version, filepath.Dir(executable))}
		a, e := app.Open(s.dataDir, srv.event)
		if e != nil {
			return e
		}
		defer a.Close()
		if e = a.Store.RecoverJobs(cmd.Context()); e != nil {
			return e
		}
		srv.app = a
		srv.auth = authui.New(a.Store, a.Paths.TDLStorage, srv.loginEvent, a.Runner)
		return srv.serve(cmd.Context(), os.Stdin)
	}}
}
func (r *rpcServer) serve(ctx context.Context, in io.Reader) error {
	ctx, cancel := context.WithCancel(ctx)
	r.cancelRequests = cancel
	defer func() { cancel(); r.requests.Wait(); r.auth.Shutdown(); _ = r.app.Jobs.Shutdown(context.Background()) }()
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scan.Scan() {
		var req rpcRequest
		if err := json.Unmarshal(scan.Bytes(), &req); err != nil {
			r.write(rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "Parse error"}})
			continue
		}
		handle := func(req rpcRequest) {
			result, err := r.call(ctx, req.Method, req.Params)
			res := rpcResponse{JSONRPC: "2.0", ID: req.ID}
			if err != nil {
				res.Error = &rpcError{Code: -32000, Message: err.Error()}
			} else {
				res.Result = result
			}
			r.write(res)
		}
		switch req.Method {
		case "chats.avatars", "media.thumbnail", "media.thumbnails", "chats.refresh", "media.scan", "app.update.check", "app.update.prepare", "cache.clear", "engine.install":
			r.requests.Add(1)
			go func(req rpcRequest) { defer r.requests.Done(); handle(req) }(req)
		default:
			handle(req)
		}
	}
	return scan.Err()
}
func decodeParams(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, v)
}
func (r *rpcServer) call(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "app.bootstrap":
		accounts, e := r.app.Store.Accounts(ctx)
		if e != nil {
			return nil, e
		}
		rules, _ := r.app.Store.Rules(ctx)
		js, _ := r.app.Store.Jobs(ctx)
		var active any
		if a, e := r.app.Store.ActiveAccount(ctx); e == nil {
			active = a
		}
		var eng any
		if v, e := r.app.Engine.Active(ctx); e == nil {
			eng = v
		}
		return map[string]any{"version": r.version, "updateResult": r.updater.Result(), "protocolVersion": domain.ProtocolVersion, "settings": r.app.Settings, "accounts": accounts, "activeAccount": active, "engine": eng, "rules": rules, "jobs": js}, nil
	case "app.shutdown":
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if r.cancelRequests != nil {
			r.cancelRequests()
		}
		r.requests.Wait()
		r.auth.Shutdown()
		return true, r.app.Jobs.Shutdown(shutdownCtx)
	case "app.update.check":
		return r.updater.Check(ctx)
	case "app.update.prepare":
		return r.updater.Prepare(ctx)
	case "app.update.apply":
		var p struct {
			PID  int
			Path string
		}
		if err := decodeParams(raw, &p); err != nil {
			return nil, err
		}
		return true, r.updater.Schedule(p.PID, p.Path)
	case "engine.latest":
		return r.app.Engine.Latest(ctx)
	case "engine.list":
		return r.app.Engine.Installed(ctx)
	case "engine.install":
		var p struct {
			Version string `json:"version"`
		}
		_ = decodeParams(raw, &p)
		return r.app.Engine.Install(ctx, p.Version)
	case "engine.use":
		var p struct {
			Version string `json:"version"`
		}
		if e := decodeParams(raw, &p); e != nil {
			return nil, e
		}
		return true, r.app.Engine.Use(ctx, p.Version)
	case "accounts.list":
		return r.app.Store.Accounts(ctx)
	case "accounts.add":
		var p struct{ Name, Namespace string }
		if e := decodeParams(raw, &p); e != nil {
			return nil, e
		}
		return r.app.Accounts.Add(ctx, p.Name, p.Namespace)
	case "accounts.use":
		var p struct{ ID string }
		if e := decodeParams(raw, &p); e != nil {
			return nil, e
		}
		all, _ := r.app.Store.Jobs(ctx)
		for _, job := range all {
			if job.State == "running" {
				r.app.Jobs.Pause(job.ID)
			}
		}
		return true, r.app.Store.SetActiveAccount(ctx, p.ID)
	case "accounts.loginCommand":
		var p struct{ ID, Method, Desktop string }
		if e := decodeParams(raw, &p); e != nil {
			return nil, e
		}
		a, e := r.app.Store.Account(ctx, p.ID)
		if e != nil {
			return nil, e
		}
		args := r.app.Runner.Args(a.Namespace, "login", "--type", p.Method)
		if p.Method == "desktop" {
			args = r.app.Runner.Args(a.Namespace, "login")
			if p.Desktop != "" {
				args = append(args, "--desktop", p.Desktop)
			}
		}
		eng, e := r.app.Engine.Active(ctx)
		if e != nil {
			return nil, e
		}
		return map[string]any{"executable": eng.Path, "args": args}, nil
	case "accounts.login.start":
		var p authui.StartOptions
		if e := decodeParams(raw, &p); e != nil {
			return nil, e
		}
		var fields map[string]json.RawMessage
		if e := decodeParams(raw, &fields); e != nil {
			return nil, e
		}
		if _, explicit := fields["proxy"]; explicit {
			r.previewMu.Lock()
			e := r.app.SetLoginProxy(ctx, p.Proxy)
			r.previewMu.Unlock()
			if e != nil {
				return nil, e
			}
		} else if p.Proxy == "" {
			p.Proxy = r.app.Settings.Proxy
		}
		if p.NTP == "" {
			p.NTP = r.app.Settings.NTP
		}
		id, e := r.auth.Start(ctx, p)
		return map[string]string{"loginId": id}, e
	case "accounts.login.submit":
		var p struct{ LoginID, Kind, Value string }
		if e := decodeParams(raw, &p); e != nil {
			return nil, e
		}
		return true, r.auth.Submit(p.LoginID, p.Kind, p.Value)
	case "accounts.login.cancel":
		var p struct{ LoginID string }
		if e := decodeParams(raw, &p); e != nil {
			return nil, e
		}
		return r.auth.Cancel(p.LoginID), nil
	case "chats.list":
		var p struct{ AccountID, Query string }
		_ = decodeParams(raw, &p)
		a, e := r.account(ctx, p.AccountID)
		if e != nil {
			return nil, e
		}
		return r.app.Store.Chats(ctx, a.ID, p.Query)
	case "chats.refresh":
		var p struct{ AccountID string }
		_ = decodeParams(raw, &p)
		a, e := r.account(ctx, p.AccountID)
		if e != nil {
			return nil, e
		}
		return r.app.Catalog.RefreshChats(ctx, a)
	case "chats.avatars":
		var p struct {
			AccountID string
			ChatIDs   []string
		}
		if e := decodeParams(raw, &p); e != nil {
			return nil, e
		}
		if len(p.ChatIDs) > 32 {
			p.ChatIDs = p.ChatIDs[:32]
		}
		r.previewMu.RLock()
		previewService := r.app.Preview
		r.previewMu.RUnlock()
		paths, e := previewService.Avatars(ctx, p.AccountID, p.ChatIDs)
		return imageDataMap(paths), e
	case "media.list":
		var p struct {
			AccountID, ChatID, TopicID string
			Offset, Limit              int
		}
		if e := decodeParams(raw, &p); e != nil {
			return nil, e
		}
		a, e := r.account(ctx, p.AccountID)
		if e != nil {
			return nil, e
		}
		if p.Limit <= 0 || p.Limit > 500 {
			p.Limit = 100
		}
		if p.Offset < 0 {
			p.Offset = 0
		}
		items, e := r.app.Store.MediaPage(ctx, a.ID, p.ChatID, p.Offset, p.Limit, p.TopicID)
		items = mediaWithImageData(items)
		return map[string]any{"items": items, "nextOffset": p.Offset + len(items), "hasMore": len(items) == p.Limit}, e
	case "media.scan":
		var p struct {
			AccountID, ChatID, TopicID string
			From, To                   time.Time
			LastN                      int
			Rescan                     bool
		}
		if e := decodeParams(raw, &p); e != nil {
			return nil, e
		}
		a, e := r.account(ctx, p.AccountID)
		if e != nil {
			return nil, e
		}
		items, e := r.app.Catalog.Scan(ctx, a, catalog.ScanOptions{ChatID: p.ChatID, TopicID: p.TopicID, From: p.From, To: p.To, LastN: p.LastN, Rescan: p.Rescan})
		return map[string]any{"scanned": len(items)}, e
	case "media.thumbnail":
		var p struct{ AccountID, ChatID, MessageID string }
		if e := decodeParams(raw, &p); e != nil {
			return nil, e
		}
		r.previewMu.RLock()
		previewService := r.app.Preview
		r.previewMu.RUnlock()
		path, e := previewService.Thumbnail(ctx, p.AccountID, p.ChatID, p.MessageID)
		return map[string]string{"path": imageData(path)}, e
	case "media.thumbnails":
		var p struct {
			AccountID string
			Items     []preview.Ref
		}
		if e := decodeParams(raw, &p); e != nil {
			return nil, e
		}
		r.previewMu.RLock()
		previewService := r.app.Preview
		r.previewMu.RUnlock()
		paths, e := previewService.Thumbnails(ctx, p.AccountID, p.Items)
		return imageDataMap(paths), e
	case "media.preview":
		var p struct{ RuleID string }
		if e := decodeParams(raw, &p); e != nil {
			return nil, e
		}
		rule, e := r.app.Store.Rule(ctx, p.RuleID)
		if e != nil {
			return nil, e
		}
		a, e := r.app.Store.Account(ctx, rule.AccountID)
		if e != nil {
			return nil, e
		}
		chat, e := r.app.Store.Chat(ctx, rule.AccountID, rule.ChatID)
		if e != nil {
			return nil, e
		}
		plan, e := r.app.Planner.Build(ctx, rule, a, chat)
		if e != nil {
			return nil, e
		}
		if e = r.app.Store.SavePlan(ctx, plan); e != nil {
			return nil, e
		}
		return plan, nil
	case "plans.save":
		return r.selectPlan(ctx, raw, true)
	case "plans.select":
		return r.selectPlan(ctx, raw, false)
	case "media.previewSelection":
		return r.selectionPreview(ctx, raw)
	case "media.previewAfter":
		return r.afterPreview(ctx, raw)
	case "media.operation.get":
		return r.mediaOperation(raw, false)
	case "media.operation.cancel":
		return r.mediaOperation(raw, true)
	case "media.scan.start":
		return r.startMediaOperation(ctx, func(ctx context.Context) (*domain.DownloadPlan, error) {
			_, err := r.call(ctx, "media.scan", raw)
			return nil, err
		}), nil
	case "rules.list":
		return r.app.Store.Rules(ctx)
	case "rules.save":
		var p domain.Rule
		if e := decodeParams(raw, &p); e != nil {
			return nil, e
		}
		if e := planner.ValidateRule(p); e != nil {
			return nil, e
		}
		return true, r.app.Store.SaveRule(ctx, p)
	case "rules.delete":
		var p struct{ ID string }
		if e := decodeParams(raw, &p); e != nil {
			return nil, e
		}
		return true, r.app.Store.DeleteRule(ctx, p.ID)
	case "jobs.list":
		return r.app.Store.Jobs(ctx)
	case "jobs.get":
		var p struct{ ID string }
		if e := decodeParams(raw, &p); e != nil {
			return nil, e
		}
		j, it, e := r.app.Store.Job(ctx, p.ID)
		return map[string]any{"job": j, "items": it}, e
	case "jobs.create":
		var p struct{ PlanID string }
		if e := decodeParams(raw, &p); e != nil {
			return nil, e
		}
		return r.app.Jobs.Create(ctx, p.PlanID)
	case "jobs.start":
		var p struct{ ID string }
		if e := decodeParams(raw, &p); e != nil {
			return nil, e
		}
		return true, r.app.Jobs.Start(context.Background(), p.ID)
	case "jobs.retry":
		var p struct{ ID string }
		if e := decodeParams(raw, &p); e != nil {
			return nil, e
		}
		return true, r.app.Jobs.Retry(context.Background(), p.ID)
	case "jobs.pause":
		var p struct{ ID string }
		if e := decodeParams(raw, &p); e != nil {
			return nil, e
		}
		return r.app.Jobs.Pause(p.ID), nil
	case "jobs.cancel":
		var p struct{ ID string }
		if e := decodeParams(raw, &p); e != nil {
			return nil, e
		}
		return true, r.app.Jobs.Cancel(ctx, p.ID)
	case "config.show":
		return r.app.Settings, nil
	case "config.set":
		var p struct{ Key, Value string }
		if e := decodeParams(raw, &p); e != nil {
			return nil, e
		}
		return true, r.app.SetConfig(ctx, p.Key, p.Value)
	case "cache.clear":
		if e := r.app.Runner.AcquireContext(ctx); e != nil {
			return nil, e
		}
		defer r.app.Runner.Release()
		return true, r.app.ClearCache(ctx)
	default:
		return nil, fmt.Errorf("method %q not found", method)
	}
}
func (r *rpcServer) account(ctx context.Context, id string) (domain.Account, error) {
	if id != "" {
		return r.app.Store.Account(ctx, id)
	}
	return r.app.Store.ActiveAccount(ctx)
}

const maxInlineImageBytes = 2 << 20

func imageData(path string) string {
	if path == "" || len(path) >= 5 && path[:5] == "data:" {
		return path
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() <= 0 || info.Size() > maxInlineImageBytes {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	kind := mime.TypeByExtension(filepath.Ext(path))
	if kind == "" {
		kind = "image/jpeg"
	}
	return "data:" + kind + ";base64," + base64.StdEncoding.EncodeToString(b)
}

func imageDataMap(paths map[string]string) map[string]string {
	result := make(map[string]string, len(paths))
	for id, path := range paths {
		if data := imageData(path); data != "" {
			result[id] = data
		}
	}
	return result
}

func mediaWithImageData(items []domain.Media) []domain.Media {
	for i := range items {
		items[i].ThumbPath = imageData(items[i].ThumbPath)
	}
	return items
}
