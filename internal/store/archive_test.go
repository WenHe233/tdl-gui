package store

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/local/tdl-gui/internal/domain"
)

func TestArchiveBrowsingAndPersistentFolders(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	chats := []domain.Chat{
		{ID: "1", VisibleName: "Main", PinnedOrder: 1},
		{ID: "2", VisibleName: "Zulu", Archived: true, PinnedOrder: 2},
		{ID: "3", VisibleName: "Alpha", Username: "saved", Archived: true, LastMessageAt: time.Unix(300, 0)},
		{ID: "4", VisibleName: "Beta", Archived: true, PinnedOrder: 1},
		{ID: "5", VisibleName: "Gamma", Archived: true, LastMessageAt: time.Unix(200, 0)},
	}
	folders := []domain.ChatFolder{
		{ID: "work", Title: "工作", ChatIDs: []string{"1", "2", "3"}, PinnedIDs: []string{"3", "1"}},
		{ID: "all", Title: "全部聊天", ChatIDs: []string{"stale"}},
		{ID: "archived", Title: "stale", ChatIDs: []string{"stale"}},
		{ID: "empty", Title: "空分组", ChatIDs: []string{}, PinnedIDs: []string{}},
	}
	if err = s.SaveDirectory(ctx, "a", chats, folders); err != nil {
		t.Fatal(err)
	}
	if err = s.SetSetting(ctx, "ui.folder.a", "archived"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, tc := range []struct {
		folder, order, query string
		ids                  []string
	}{
		{"", "name", "", []string{"3", "4", "5", "1", "2"}},
		{"all", "recent", "", []string{"1"}},
		{"archived", "recent", "", []string{"4", "2", "3", "5"}},
		{"archived", "name", "", []string{"3", "4", "5", "2"}},
		{"archived", "recent", "SAVED", []string{"3"}},
		{"all", "recent", "saved", []string{}},
		{"work", "recent", "", []string{"3", "1", "2"}},
		{"empty", "recent", "", []string{}},
	} {
		got, e := s.BrowseChats(ctx, "a", tc.query, tc.folder, tc.order)
		if e != nil {
			t.Fatal(e)
		}
		ids := []string{}
		for _, c := range got {
			ids = append(ids, c.ID)
		}
		if !reflect.DeepEqual(ids, tc.ids) {
			t.Fatalf("%+v: got %v", tc, ids)
		}
	}
	got, err := s.Folders(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || got[0].ID != "work" || got[1].ID != "all" || got[2].ID != "archived" || got[3].ID != "empty" {
		t.Fatal(got)
	}
	if !reflect.DeepEqual(got[0], folders[0]) || !reflect.DeepEqual(got[1].ChatIDs, []string{"1"}) || !reflect.DeepEqual(got[2].ChatIDs, []string{"2", "3", "4", "5"}) || !reflect.DeepEqual(got[2].PinnedIDs, []string{"4", "2"}) {
		t.Fatal(got)
	}
	selected, err := s.GetSetting(ctx, "ui.folder.a")
	if err != nil || selected != "archived" {
		t.Fatal(selected, err)
	}
	other, err := s.Folders(ctx, "b")
	if err != nil || len(other) != 2 || len(other[0].ChatIDs) != 0 || len(other[1].ChatIDs) != 0 {
		t.Fatal(other, err)
	}
	if selected, _ := s.GetSetting(ctx, "ui.folder.b"); selected != "" {
		t.Fatal(selected)
	}

	// External archive changes replace membership on the next complete snapshot.
	chats[0].Archived = true
	chats[1].Archived = false
	if err = s.SaveDirectory(ctx, "a", chats, folders); err != nil {
		t.Fatal(err)
	}
	main, err := s.BrowseChats(ctx, "a", "", "all", "recent")
	if err != nil || len(main) != 1 || main[0].ID != "2" {
		t.Fatal(main, err)
	}
}

func TestArchiveLegacyAndEmptySnapshots(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// This is the actual pre-feature JSON shape, with no archived property.
	_, err = s.db.ExecContext(ctx, `INSERT INTO chat_directory VALUES(?,?,?,?)`, "legacy", `[{"id":"1","visibleName":"Legacy"}]`, `[]`, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveAccount(ctx, domain.Account{ID: "sql", Namespace: "legacy", DisplayName: "Legacy"}); err != nil {
		t.Fatal(err)
	}
	if err = s.UpsertChats(ctx, []domain.Chat{{AccountID: "sql", ID: "2", VisibleName: "Legacy SQL"}}); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveDirectory(ctx, "only", []domain.Chat{{ID: "3", Archived: true}}, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveDirectory(ctx, "empty", nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		account       string
		main, archive int
	}{{"legacy", 1, 0}, {"sql", 1, 0}, {"only", 0, 1}, {"empty", 0, 0}, {"missing", 0, 0}} {
		folders, e := s.Folders(ctx, tc.account)
		if e != nil || len(folders) != 2 {
			t.Fatal(tc, folders, e)
		}
		if folders[0].ID != "all" || folders[1].ID != "archived" || len(folders[0].ChatIDs) != tc.main || len(folders[1].ChatIDs) != tc.archive {
			t.Fatal(tc, folders)
		}
		for _, f := range folders {
			if f.ChatIDs == nil || f.PinnedIDs == nil {
				t.Fatal("system folder lists must encode as arrays")
			}
		}
	}
}
