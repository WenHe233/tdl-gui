package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/local/tdl-gui/internal/app"
	"github.com/local/tdl-gui/internal/catalog"
	"github.com/local/tdl-gui/internal/domain"
	"github.com/local/tdl-gui/internal/tdl"
)

// Run the test binary as a deterministic tdl export process, without Telegram.
func TestMain(m *testing.M) {
	if payload := os.Getenv("TDL_TEST_EXPORT"); payload != "" {
		for i, arg := range os.Args {
			if arg == "--output" && i+1 < len(os.Args) {
				if err := os.WriteFile(os.Args[i+1], []byte(payload), 0600); err != nil {
					os.Exit(2)
				}
				os.Exit(0)
			}
		}
		os.Exit(3)
	}
	os.Exit(m.Run())
}

func selectionServer(t *testing.T) (*rpcServer, domain.Rule, []domain.Media) {
	t.Helper()
	ctx := context.Background()
	a, err := app.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if err = a.Store.SaveAccount(ctx, domain.Account{ID: "a", Namespace: "test", DisplayName: "A", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.UpsertChats(ctx, []domain.Chat{{ID: "10", AccountID: "a", VisibleName: "C"}}); err != nil {
		t.Fatal(err)
	}
	items := make([]domain.Media, 125)
	date := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range items {
		id := fmt.Sprint(i + 1)
		items[i] = domain.Media{AccountID: "a", ChatID: "10", MessageID: id, MediaID: id, TopicID: "5", FileName: id + ".jpg", Kind: "photo", Size: 3, Date: date.Add(time.Duration(i) * time.Second)}
	}
	if err = a.Store.SaveMedia(ctx, items); err != nil {
		t.Fatal(err)
	}
	return &rpcServer{app: a, enc: json.NewEncoder(io.Discard)}, domain.Rule{AccountID: "a", ChatID: "10", RootDir: t.TempDir(), Order: "oldest"}, items
}

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func TestSelectionIsExplicitAndServerOwned(t *testing.T) {
	r, rule, items := selectionServer(t)
	ctx := context.Background()
	rule.Kinds = []string{"video"}
	rule.MaxFiles = 1
	rule.MaxTotalSize = 1
	rule.From = items[124].Date
	value, err := r.selectionPreview(ctx, raw(previewRequest{Rule: rule, MessageIDs: []string{"1", "125", "1"}}))
	if err != nil {
		t.Fatal(err)
	}
	p := value.(domain.DownloadPlan)
	if p.SelectedFiles != 2 || p.SelectedBytes != 6 || p.RuleID != "" {
		t.Fatalf("plan=%+v", p)
	}
	if _, err = r.selectionPreview(ctx, raw(previewRequest{Rule: rule, MessageIDs: []string{"126"}})); err == nil {
		t.Fatal("unknown ID accepted")
	}
	original := p.Items[0].TargetPath
	p.Items[0].TargetPath = filepath.Join(t.TempDir(), "forged")
	p.Items[0].Media.Size = 999
	p.Items[1].Selected = false
	if _, err = r.selectPlan(ctx, raw(p), true); err != nil {
		t.Fatal(err)
	}
	saved, err := r.app.Store.Plan(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Items[0].TargetPath != original || saved.SelectedFiles != 1 || saved.SelectedBytes != 3 {
		t.Fatalf("client changed fixed plan: %+v", saved)
	}
	if _, err = r.selectPlan(ctx, raw(map[string]any{"id": p.ID, "messageIds": []string{"125"}}), false); err != nil {
		t.Fatal(err)
	}
	rule.TopicID = "other"
	if _, err = r.selectionPreview(ctx, raw(previewRequest{Rule: rule, MessageIDs: []string{"1"}})); err == nil {
		t.Fatal("cross-topic selection accepted")
	}
}
func TestAfterPreviewUsesScanSnapshotAndInclusiveTime(t *testing.T) {
	r, rule, items := selectionServer(t)
	ctx := context.Background()
	// The earlier item shares the anchor second and must be included as well.
	items[123].Date = items[124].Date
	if err := r.app.Store.SaveMedia(ctx, items); err != nil {
		t.Fatal(err)
	}
	messages := []any{}
	for _, m := range items[123:] {
		messages = append(messages, map[string]any{"id": json.Number(m.MessageID), "date": m.Date.Unix(), "file": m.FileName, "raw": map[string]any{"media": map[string]any{"document": map[string]any{"id": json.Number(m.MediaID), "size": 3, "mime_type": "image/jpeg"}}}})
	}
	t.Setenv("TDL_TEST_EXPORT", string(raw(map[string]any{"id": 10, "messages": messages})))
	executable, _ := os.Executable()
	runner := tdl.New(func(context.Context) (string, error) { return executable, nil }, r.app.Paths.TDLStorage, "", "")
	r.app.Catalog = catalog.New(r.app.Store, runner, r.app.Paths.Cache)
	rule.TopicID = "5"
	rule.RecentDays = 1
	rule.LastN = 1
	rule.MinMessageID = 999
	rule.MaxMessageID = 1000
	v, err := r.afterPreview(ctx, raw(previewRequest{Rule: rule, AnchorMessageID: "125", To: items[124].Date}))
	if err != nil {
		t.Fatal(err)
	}
	r.requests.Wait()
	result, err := r.mediaOperation(raw(map[string]string{"id": v.(map[string]string)["operationId"]}), false)
	if err != nil {
		t.Fatal(err)
	}
	op := result.(mediaOperation)
	if op.State != "completed" || op.Plan == nil || op.Plan.SelectedFiles != 2 || len(op.Plan.Items) != 2 {
		t.Fatalf("operation=%+v", op)
	}
	if !op.Plan.From.Equal(items[124].Date) || !op.Plan.To.Equal(items[124].Date) {
		t.Fatal("range was altered")
	}
	rules, _ := r.app.Store.Rules(ctx)
	if len(rules) != 0 {
		t.Fatal("temporary action saved a rule")
	}
}
func TestAfterFailureAndCancelNeverPublishAPlan(t *testing.T) {
	r, rule, items := selectionServer(t)
	ctx := context.Background()
	runner := tdl.New(func(context.Context) (string, error) { return "", fmt.Errorf("scan failed") }, r.app.Paths.TDLStorage, "", "")
	r.app.Catalog = catalog.New(r.app.Store, runner, r.app.Paths.Cache)
	v, err := r.afterPreview(ctx, raw(previewRequest{Rule: rule, AnchorMessageID: "1", To: items[124].Date}))
	if err != nil {
		t.Fatal(err)
	}
	r.requests.Wait()
	result, _ := r.mediaOperation(raw(map[string]string{"id": v.(map[string]string)["operationId"]}), false)
	op := result.(mediaOperation)
	if op.State != "failed" || op.Plan != nil {
		t.Fatalf("operation=%+v", op)
	}
	runner.Acquire()
	defer runner.Release()
	v, err = r.afterPreview(ctx, raw(previewRequest{Rule: rule, AnchorMessageID: "1", To: items[124].Date}))
	if err != nil {
		t.Fatal(err)
	}
	id := v.(map[string]string)["operationId"]
	_, err = r.mediaOperation(raw(map[string]string{"id": id}), true)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { r.requests.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel blocked waiting for engine")
	}
	result, _ = r.mediaOperation(raw(map[string]string{"id": id}), false)
	op = result.(mediaOperation)
	if op.State != "cancelled" || op.Plan != nil {
		t.Fatalf("operation=%+v", op)
	}
}
