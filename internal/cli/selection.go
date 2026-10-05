package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/local/tdl-gui/internal/catalog"
	"github.com/local/tdl-gui/internal/domain"
	"github.com/local/tdl-gui/internal/idgen"
	"github.com/local/tdl-gui/internal/planner"
)

type previewRequest struct {
	Rule            domain.Rule `json:"rule"`
	MessageIDs      []string    `json:"messageIds"`
	AnchorMessageID string      `json:"anchorMessageId"`
	To              time.Time   `json:"to"`
}

type mediaOperation struct {
	ID     string               `json:"operationId"`
	State  string               `json:"state"`
	Plan   *domain.DownloadPlan `json:"plan,omitempty"`
	Error  string               `json:"error,omitempty"`
	Cancel context.CancelFunc   `json:"-"`
}

func (r *rpcServer) previewContext(ctx context.Context, rule domain.Rule) (domain.Account, domain.Chat, error) {
	if err := planner.ValidateRule(rule); err != nil {
		return domain.Account{}, domain.Chat{}, err
	}
	a, err := r.app.Store.Account(ctx, rule.AccountID)
	if err != nil {
		return a, domain.Chat{}, err
	}
	c, err := r.app.Store.Chat(ctx, a.ID, rule.ChatID)
	return a, c, err
}

func (r *rpcServer) selectionPreview(ctx context.Context, raw json.RawMessage) (any, error) {
	var req previewRequest
	if err := decodeParams(raw, &req); err != nil {
		return nil, err
	}
	// Explicit selections are independent of saved filters and quotas.
	rule := domain.Rule{AccountID: req.Rule.AccountID, ChatID: req.Rule.ChatID, TopicID: req.Rule.TopicID, RootDir: req.Rule.RootDir, Template: req.Rule.Template, Order: req.Rule.Order, Timezone: req.Rule.Timezone}
	a, c, err := r.previewContext(ctx, rule)
	if err != nil {
		return nil, err
	}
	if len(req.MessageIDs) == 0 {
		return nil, fmt.Errorf("请先选择媒体")
	}
	all, err := r.app.Store.Media(ctx, a.ID, c.ID)
	if err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	for _, id := range req.MessageIDs {
		wanted[id] = true
	}
	items := make([]domain.Media, 0, len(wanted))
	for _, m := range all {
		if wanted[m.MessageID] && (rule.TopicID == "" || m.TopicID == rule.TopicID) {
			items = append(items, m)
			delete(wanted, m.MessageID)
		}
	}
	if len(wanted) > 0 {
		return nil, fmt.Errorf("部分所选消息已失效或不属于当前话题，请刷新后重选")
	}
	p, err := planner.BuildMedia(items, rule, a, c)
	if err != nil {
		return nil, err
	}
	return p, r.app.Store.SavePlan(ctx, p)
}

func (r *rpcServer) afterPreview(ctx context.Context, raw json.RawMessage) (any, error) {
	var req previewRequest
	if err := decodeParams(raw, &req); err != nil {
		return nil, err
	}
	rule := req.Rule
	rule.ID = ""
	rule.RecentDays = 0
	rule.LastN = 0
	rule.MinMessageID = 0
	rule.MaxMessageID = 0
	rule.From = time.Time{}
	rule.To = time.Time{}
	a, c, err := r.previewContext(ctx, rule)
	if err != nil {
		return nil, err
	}
	all, err := r.app.Store.Media(ctx, a.ID, c.ID)
	if err != nil {
		return nil, err
	}
	found := false
	for _, m := range all {
		if m.MessageID == req.AnchorMessageID && (rule.TopicID == "" || rule.TopicID == m.TopicID) {
			rule.From = m.Date
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("起始消息不存在或不属于当前话题")
	}
	rule.To = req.To
	if rule.To.IsZero() || rule.To.After(time.Now()) {
		rule.To = time.Now().UTC()
	}
	if err = planner.ValidateRule(rule); err != nil {
		return nil, err
	}
	return r.startMediaOperation(ctx, func(ctx context.Context) (*domain.DownloadPlan, error) {
		items, err := r.app.Catalog.Scan(ctx, a, catalog.ScanOptions{ChatID: c.ID, TopicID: rule.TopicID, From: rule.From, To: rule.To})
		if err != nil {
			return nil, err
		}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		// Use refreshed local metadata, but only for messages returned by this scan.
		stored, err := r.app.Store.Media(ctx, a.ID, c.ID)
		if err != nil {
			return nil, err
		}
		indexed := map[string]domain.Media{}
		for _, m := range stored {
			indexed[m.MessageID] = m
		}
		for i, m := range items {
			if local, ok := indexed[m.MessageID]; ok {
				items[i] = local
			}
		}
		p, err := planner.BuildMedia(items, rule, a, c)
		if err != nil {
			return nil, err
		}
		if err = r.app.Store.SavePlan(ctx, p); err != nil {
			return nil, err
		}
		return &p, nil
	}), nil
}

func (r *rpcServer) startMediaOperation(parent context.Context, fn func(context.Context) (*domain.DownloadPlan, error)) map[string]string {
	id := idgen.New("operation")
	ctx, cancel := context.WithCancel(parent)
	r.opMu.Lock()
	if r.operations == nil {
		r.operations = map[string]*mediaOperation{}
	}
	// Finished operations are only needed until the next user action.
	for key, op := range r.operations {
		if op.State != "running" {
			delete(r.operations, key)
		}
	}
	r.operations[id] = &mediaOperation{ID: id, State: "running", Cancel: cancel}
	r.opMu.Unlock()
	r.requests.Add(1)
	go func() {
		defer r.requests.Done()
		defer cancel()
		p, err := fn(ctx)
		r.opMu.Lock()
		op := r.operations[id]
		if ctx.Err() != nil {
			op.State = "cancelled"
			op.Error = "操作已取消"
		} else if err != nil {
			op.State = "failed"
			op.Error = err.Error()
		} else {
			op.State = "completed"
			op.Plan = p
		}
		result := *op
		r.opMu.Unlock()
		r.write(rpcResponse{JSONRPC: "2.0", Method: "event", Params: map[string]any{"type": "media.operation", "operation": result}})
	}()
	return map[string]string{"operationId": id}
}

func (r *rpcServer) mediaOperation(raw json.RawMessage, cancel bool) (any, error) {
	var p struct{ ID string }
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()
	op := r.operations[p.ID]
	if op == nil {
		return nil, fmt.Errorf("操作不存在")
	}
	if cancel && op.State == "running" {
		op.Cancel()
	}
	return *op, nil
}

func (r *rpcServer) selectPlan(ctx context.Context, raw json.RawMessage, legacy bool) (any, error) {
	var p struct {
		ID         string
		MessageIDs []string
	}
	if legacy {
		var submitted domain.DownloadPlan
		if err := decodeParams(raw, &submitted); err != nil {
			return nil, err
		}
		p.ID = submitted.ID
		for _, it := range submitted.Items {
			if it.Selected {
				p.MessageIDs = append(p.MessageIDs, it.Media.MessageID)
			}
		}
	} else if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	original, err := r.app.Store.Plan(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	updated, err := planner.Select(original, p.MessageIDs)
	if err != nil {
		return nil, err
	}
	return true, r.app.Store.SavePlan(ctx, updated)
}
