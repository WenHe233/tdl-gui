package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/local/tdl-gui/internal/domain"
	"github.com/local/tdl-gui/internal/planner"
)

func (r *rpcServer) browseMedia(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		AccountID, ChatID, TopicID, Order string
		Offset, Limit                     int
		BrowseRule                        *domain.Rule
	}
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Order == "" {
		p.Order = "newest"
	}
	if p.Order != "oldest" && p.Order != "newest" {
		return nil, fmt.Errorf("无效消息顺序")
	}
	a, err := r.account(ctx, p.AccountID)
	if err != nil {
		return nil, err
	}
	items, err := r.app.Store.Media(ctx, a.ID, p.ChatID)
	if err != nil {
		return nil, err
	}
	indexedCount := len(items)
	rule := domain.Rule{TopicID: p.TopicID}
	if p.BrowseRule != nil {
		rule = *p.BrowseRule
		rule.TopicID = p.TopicID
	}
	items = planner.BrowseMedia(items, rule, p.Order)
	if p.Offset < 0 {
		p.Offset = 0
	}
	if p.Limit <= 0 || p.Limit > 500 {
		p.Limit = 100
	}
	if p.Offset > len(items) {
		p.Offset = len(items)
	}
	end := p.Offset + p.Limit
	if end > len(items) {
		end = len(items)
	}
	return map[string]any{"indexedCount": indexedCount, "items": mediaWithImageData(items[p.Offset:end]), "nextOffset": end, "hasMore": end < len(items)}, nil
}

type accountRequest struct {
	accountID string
	cancel    context.CancelFunc
	done      chan struct{}
}

func (r *rpcServer) call(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "chats.refresh", "chats.avatars", "media.thumbnail", "media.thumbnails", "media.scan":
		var p struct{ AccountID string }
		if err := decodeParams(raw, &p); err != nil {
			return nil, err
		}
		a, err := r.account(ctx, p.AccountID)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithCancel(ctx)
		request := &accountRequest{accountID: a.ID, cancel: cancel, done: make(chan struct{})}
		r.accountMu.Lock()
		if r.removing[a.ID] {
			r.accountMu.Unlock()
			cancel()
			return nil, fmt.Errorf("正在删除账户登录信息")
		}
		if r.accountRequests == nil {
			r.accountRequests = map[*accountRequest]bool{}
		}
		r.accountRequests[request] = true
		r.accountMu.Unlock()
		defer func() {
			cancel()
			r.accountMu.Lock()
			delete(r.accountRequests, request)
			r.accountMu.Unlock()
			close(request.done)
		}()
		return r.dispatch(ctx, method, raw)
	default:
		return r.dispatch(ctx, method, raw)
	}
}

func (r *rpcServer) removeAccount(ctx context.Context, id string) (any, error) {
	_, err := r.app.Store.Account(ctx, id)
	if err != nil {
		return nil, err
	}
	r.accountMu.Lock()
	if r.removing == nil {
		r.removing = map[string]bool{}
	}
	if r.removing[id] {
		r.accountMu.Unlock()
		return nil, fmt.Errorf("正在删除账户登录信息")
	}
	r.removing[id] = true
	pending := []*accountRequest{}
	for request := range r.accountRequests {
		if request.accountID == id {
			request.cancel()
			pending = append(pending, request)
		}
	}
	r.accountMu.Unlock()
	defer func() { r.accountMu.Lock(); delete(r.removing, id); r.accountMu.Unlock() }()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err = r.app.Store.RemoveAccount(ctx, id); err != nil {
		return nil, err
	}
	if err = r.auth.CancelAccount(ctx, id); err != nil {
		return nil, err
	}
	r.opMu.Lock()
	for _, op := range r.operations {
		if op.AccountID == id && op.State == "running" {
			op.Cancel()
		}
	}
	r.opMu.Unlock()
	if err = r.app.Jobs.PauseAccount(ctx, id); err != nil {
		return nil, err
	}
	for _, request := range pending {
		select {
		case <-request.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err = r.app.Runner.AcquireContext(ctx); err != nil {
		return nil, err
	}
	defer r.app.Runner.Release()
	// Startup also purges retired namespaces after a crash.
	namespaces, err := r.app.Store.RemovedNamespaces(ctx)
	if err != nil {
		return nil, err
	}
	for _, ns := range namespaces {
		if err = r.app.Vault.RemoveNamespace(ns); err != nil {
			return nil, err
		}
	}
	return true, nil
}

// Keep recent/name selection explicit at the RPC boundary.
func validChatOrder(order string) bool { return order == "" || order == "name" || order == "recent" }
