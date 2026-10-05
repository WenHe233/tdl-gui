package catalog

import (
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"github.com/local/tdl-gui/internal/domain"
	"testing"
)

func TestFolderRules(t *testing.T) {
	user := &tg.InputPeerUser{UserID: 1}
	other := &tg.InputPeerUser{UserID: 2}
	cases := []struct {
		name string
		f    tg.DialogFilter
		m    dialogMeta
		want bool
	}{
		{"contact", tg.DialogFilter{Contacts: true}, dialogMeta{peer: user, chat: domain.Chat{Type: "private"}, contact: true}, true},
		{"non contact", tg.DialogFilter{Contacts: true}, dialogMeta{peer: user, chat: domain.Chat{Type: "private"}}, false},
		{"bot category", tg.DialogFilter{NonContacts: true}, dialogMeta{peer: user, chat: domain.Chat{Type: "private"}, bot: true}, false},
		{"bots", tg.DialogFilter{Bots: true}, dialogMeta{peer: user, chat: domain.Chat{Type: "private"}, bot: true}, true},
		{"groups", tg.DialogFilter{Groups: true}, dialogMeta{chat: domain.Chat{Type: "group"}}, true},
		{"channels", tg.DialogFilter{Broadcasts: true}, dialogMeta{chat: domain.Chat{Type: "channel"}}, true},
		{"muted", tg.DialogFilter{Groups: true, ExcludeMuted: true}, dialogMeta{chat: domain.Chat{Type: "group"}, muted: true}, false},
		{"muted mention", tg.DialogFilter{Groups: true, ExcludeMuted: true}, dialogMeta{chat: domain.Chat{Type: "group"}, muted: true, mentioned: true, unread: true}, true},
		{"archived muted mention", tg.DialogFilter{Groups: true, ExcludeMuted: true}, dialogMeta{chat: domain.Chat{Type: "group"}, muted: true, mentioned: true, unread: true, archived: true}, false},
		{"read", tg.DialogFilter{Groups: true, ExcludeRead: true}, dialogMeta{chat: domain.Chat{Type: "group"}}, false},
		{"unread", tg.DialogFilter{Groups: true, ExcludeRead: true}, dialogMeta{chat: domain.Chat{Type: "group"}, unread: true}, true},
		{"archived", tg.DialogFilter{Groups: true, ExcludeArchived: true}, dialogMeta{chat: domain.Chat{Type: "group"}, archived: true}, false},
		{"explicit include", tg.DialogFilter{ExcludeMuted: true, IncludePeers: []tg.InputPeerClass{user}}, dialogMeta{peer: user, muted: true}, true},
		{"explicit archived include", tg.DialogFilter{ExcludeArchived: true, IncludePeers: []tg.InputPeerClass{user}}, dialogMeta{peer: user, archived: true}, true},
		{"archived group included", tg.DialogFilter{Groups: true}, dialogMeta{chat: domain.Chat{Type: "group"}, archived: true}, true},
		{"explicit exclude", tg.DialogFilter{Contacts: true, ExcludePeers: []tg.InputPeerClass{user}}, dialogMeta{peer: user, chat: domain.Chat{Type: "private"}, contact: true}, false},
		{"pinned", tg.DialogFilter{PinnedPeers: []tg.InputPeerClass{user}}, dialogMeta{peer: user}, true},
		{"different peer", tg.DialogFilter{IncludePeers: []tg.InputPeerClass{other}}, dialogMeta{peer: user}, false},
		{"typed peer", tg.DialogFilter{IncludePeers: []tg.InputPeerClass{&tg.InputPeerChannel{ChannelID: 1}}}, dialogMeta{peer: user}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := folderMatches(&c.f, c.m); got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestDirectoryRequestsExplicitArchiveFolder(t *testing.T) {
	for _, id := range []int{0, 1} {
		req := directoryDialogRequest(id, dialogs.Request{Limit: 100, OffsetPeer: &tg.InputPeerEmpty{}})
		folder, ok := req.GetFolderID()
		if !ok || folder != id {
			t.Fatal("missing folder flag", id, folder, ok)
		}
	}
}
