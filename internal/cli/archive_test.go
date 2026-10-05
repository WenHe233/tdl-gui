package cli

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/local/tdl-gui/internal/domain"
)

func TestArchiveRPCIncludesCompleteCacheAndMedia(t *testing.T) {
	r, rule, _ := selectionServer(t)
	ctx := context.Background()
	chats := []domain.Chat{{AccountID: "a", ID: "10", VisibleName: "Archived", Archived: true}, {AccountID: "a", ID: "20", VisibleName: "Main"}}
	if err := r.app.Store.SaveDirectory(ctx, "a", chats, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.app.Store.SetSetting(ctx, "ui.folder.a", "archived"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		folder string
		count  int
	}{{"", 2}, {"all", 1}, {"archived", 1}} {
		result, err := r.call(ctx, "chats.list", raw(map[string]any{"accountId": "a", "folderId": tc.folder}))
		if err != nil || len(result.([]domain.Chat)) != tc.count {
			t.Fatal(tc, result, err)
		}
		if tc.folder == "archived" {
			payload, _ := json.Marshal(result)
			var wire []struct {
				Archived bool `json:"archived"`
			}
			if err := json.Unmarshal(payload, &wire); err != nil || !wire[0].Archived {
				t.Fatal(string(payload), err)
			}
		}
	}
	result, err := r.call(ctx, "chats.folders", raw(map[string]any{"accountId": "a"}))
	if err != nil {
		t.Fatal(err)
	}
	folders := result.(map[string]any)
	if folders["selectedFolderId"] != "archived" || len(folders["folders"].([]domain.ChatFolder)) != 2 {
		t.Fatal(folders)
	}
	result, err = r.call(ctx, "media.list", raw(map[string]any{"accountId": "a", "chatId": "10", "topicId": "5"}))
	if err != nil || len(result.(map[string]any)["items"].([]domain.Media)) != 100 {
		t.Fatal(result, err)
	}
	result, err = r.call(ctx, "media.previewSelection", raw(map[string]any{"rule": rule, "messageIds": []string{"1"}}))
	if err != nil {
		t.Fatal(err)
	}
	plan := result.(domain.DownloadPlan)
	if plan.ChatID != "10" || plan.SelectedFiles != 1 {
		t.Fatal(plan)
	}

	r.app.Catalog.Directory = func(context.Context, domain.Account) ([]domain.Chat, error) { return nil, errors.New("offline") }
	if _, err = r.call(ctx, "chats.refresh", raw(map[string]any{"accountId": "a"})); err == nil {
		t.Fatal("refresh should fail")
	}
	result, err = r.call(ctx, "chats.list", raw(map[string]any{"accountId": "a", "folderId": "archived"}))
	if err != nil || len(result.([]domain.Chat)) != 1 || !result.([]domain.Chat)[0].Archived {
		t.Fatal("cached archive lost", result, err)
	}
}
