package planner

import (
	"github.com/local/tdl-gui/internal/domain"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFilterBeforeDedupeAndExactMidnight(t *testing.T) {
	d := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	items := []domain.Media{{MessageID: "1", MediaID: "same", FileName: "old.jpg", Date: d.Add(-time.Second)}, {MessageID: "10", MediaID: "same", FileName: "new.jpg", Date: d}, {MessageID: "2", MediaID: "other", FileName: "2.jpg", Date: d}, {MessageID: "11", MediaID: "later", FileName: "11.jpg", Date: d.Add(time.Second)}}
	p, err := BuildMedia(items, domain.Rule{From: d, To: d, RootDir: t.TempDir(), Order: "oldest"}, domain.Account{}, domain.Chat{})
	if err != nil {
		t.Fatal(err)
	}
	if p.SelectedFiles != 2 || p.Items[0].Status != "filtered" || p.Items[1].Media.MessageID != "2" || p.Items[2].Media.MessageID != "10" || p.Items[3].Selected {
		t.Fatalf("plan=%+v", p)
	}
}
func TestUntrackedSameSizeFileIsNotSkipped(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "x.bin")
	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := BuildMedia([]domain.Media{{MessageID: "1", MediaID: "m", FileName: "x.bin", Size: 3}}, domain.Rule{RootDir: root, Template: "{{.OriginalName}}"}, domain.Account{}, domain.Chat{})
	if err != nil {
		t.Fatal(err)
	}
	if p.SelectedFiles != 1 {
		t.Fatalf("untracked file was trusted: %+v", p)
	}
}
func TestPlanSelectionRejectsUnknownAndIneligible(t *testing.T) {
	p := domain.DownloadPlan{Items: []domain.PlanItem{{Media: domain.Media{MessageID: "1", Size: 3}, Status: "selected", TargetPath: "fixed"}, {Media: domain.Media{MessageID: "2"}, Status: "duplicate"}}}
	for _, ids := range [][]string{{"2"}, {"3"}} {
		if _, err := Select(p, ids); err == nil {
			t.Fatal("invalid selection accepted")
		}
	}
	empty, err := Select(p, nil)
	if err != nil || empty.SelectedFiles != 0 || empty.Items[0].Status != "excluded" {
		t.Fatal(empty, err)
	}
	restored, err := Select(empty, []string{"1", "1"})
	if err != nil || restored.SelectedFiles != 1 || restored.SelectedBytes != 3 || restored.Items[0].TargetPath != "fixed" {
		t.Fatal(restored, err)
	}
}
