package planner

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/local/tdl-gui/internal/domain"
)

type fakeSource struct{ items []domain.Media }

func (f fakeSource) Media(context.Context, string, string) ([]domain.Media, error) {
	return f.items, nil
}

func TestRenderSanitizesWindowsPathAndStaysInRoot(t *testing.T) {
	root := t.TempDir()
	got, err := Render(root, DefaultTemplate, TemplateData{AccountName: "AUX", ChatName: `研发:<组>`, Date: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC), MessageID: "42", OriginalName: `report?.pdf`})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.ToLower(got), strings.ToLower(root)+string(filepath.Separator)) {
		t.Fatalf("path escaped root: %s", got)
	}
	if strings.ContainsAny(filepath.Base(got), `<>:"|?*`) {
		t.Fatalf("unsafe filename: %s", got)
	}
	if !strings.Contains(got, "2026-09-14-42-report_.pdf") {
		t.Fatalf("unexpected path: %s", got)
	}
}

func TestPlannerFiltersDeduplicatesAndAppliesLimits(t *testing.T) {
	root := t.TempDir()
	d := time.Date(2026, 9, 14, 12, 0, 0, 0, time.Local)
	items := []domain.Media{{AccountID: "a", ChatID: "c", MessageID: "1", MediaID: "same", Kind: "video", FileName: "one.mp4", Extension: ".mp4", Size: 10, Date: d}, {AccountID: "a", ChatID: "c", MessageID: "2", MediaID: "same", Kind: "video", FileName: "duplicate.mp4", Extension: ".mp4", Size: 10, Date: d}, {AccountID: "a", ChatID: "c", MessageID: "3", MediaID: "x", Kind: "document", FileName: "ignore.zip", Extension: ".zip", Size: 5, Date: d}, {AccountID: "a", ChatID: "c", MessageID: "4", MediaID: "y", Kind: "video", FileName: "two.mp4", Extension: ".mp4", Size: 12, Date: d}}
	p := New(fakeSource{items: items})
	plan, err := p.Build(context.Background(), domain.Rule{ID: "r", AccountID: "a", ChatID: "c", RootDir: root, Template: DefaultTemplate, Kinds: []string{"video"}, MaxTotalSize: 15, Order: "oldest"}, domain.Account{ID: "a", DisplayName: "account"}, domain.Chat{ID: "c", VisibleName: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.MessageCount != 4 || plan.UniqueFiles != 3 || plan.SelectedFiles != 1 || plan.SelectedBytes != 10 {
		t.Fatalf("unexpected summary: %+v", plan)
	}
	want := []string{"selected", "duplicate", "filtered", "limit"}
	for i, x := range want {
		if plan.Items[i].Status != x {
			t.Fatalf("item %d status=%s want=%s", i, plan.Items[i].Status, x)
		}
	}
}

func TestPlannerHandlesFiftyThousandRecords(t *testing.T) {
	items := make([]domain.Media, 50_000)
	now := time.Now()
	for i := range items {
		items[i] = domain.Media{AccountID: "a", ChatID: "c", MessageID: strconv.Itoa(i + 1), MediaID: strconv.Itoa(i + 1), Kind: "photo", FileName: "x.jpg", Extension: ".jpg", Size: 1, Date: now.Add(time.Duration(i) * time.Second)}
	}
	plan, err := New(fakeSource{items}).Build(context.Background(), domain.Rule{AccountID: "a", ChatID: "c", RootDir: t.TempDir(), MaxFiles: 100, Order: "newest"}, domain.Account{DisplayName: "a"}, domain.Chat{ID: "c", VisibleName: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 50_000 || plan.SelectedFiles != 100 {
		t.Fatalf("items=%d selected=%d", len(plan.Items), plan.SelectedFiles)
	}
}

func TestExistingFileIsSkipped(t *testing.T) {
	root := t.TempDir()
	m := domain.Media{AccountID: "a", ChatID: "c", MessageID: "1", MediaID: "m", Kind: "document", FileName: "x.bin", Size: 3, Date: time.Now()}
	target, err := Render(root, DefaultTemplate, TemplateData{AccountName: "a", ChatName: "c", Date: m.Date, MessageID: m.MessageID, OriginalName: m.FileName})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(target, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.LocalPath = target
	m.Downloaded = true
	plan, err := New(fakeSource{[]domain.Media{m}}).Build(context.Background(), domain.Rule{AccountID: "a", ChatID: "c", RootDir: root}, domain.Account{DisplayName: "a"}, domain.Chat{ID: "c", VisibleName: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ExistingFiles != 1 || plan.Items[0].Status != "existing" {
		t.Fatalf("unexpected: %+v", plan)
	}
}

func TestLastNSelectsNewestRegardlessOfDownloadOrder(t *testing.T) {
	now := time.Now()
	items := []domain.Media{{AccountID: "a", ChatID: "c", MessageID: "1", MediaID: "1", Kind: "photo", FileName: "1.jpg", Size: 1, Date: now.Add(-2 * time.Hour)}, {AccountID: "a", ChatID: "c", MessageID: "2", MediaID: "2", Kind: "photo", FileName: "2.jpg", Size: 1, Date: now.Add(-time.Hour)}, {AccountID: "a", ChatID: "c", MessageID: "3", MediaID: "3", Kind: "photo", FileName: "3.jpg", Size: 1, Date: now}}
	plan, err := New(fakeSource{items}).Build(context.Background(), domain.Rule{AccountID: "a", ChatID: "c", RootDir: t.TempDir(), LastN: 2, Order: "oldest"}, domain.Account{DisplayName: "a"}, domain.Chat{VisibleName: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.SelectedFiles != 2 || plan.Items[0].Selected || !plan.Items[1].Selected || !plan.Items[2].Selected {
		t.Fatalf("unexpected selection: %+v", plan.Items)
	}
}
