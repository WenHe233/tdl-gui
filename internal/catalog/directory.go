package catalog

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"github.com/iyear/tdl/core/storage"
	"github.com/iyear/tdl/pkg/kv"
	"github.com/local/tdl-gui/internal/domain"
	"github.com/local/tdl-gui/internal/store"
	"github.com/local/tdl-gui/internal/tdl"
	"github.com/local/tdl-gui/internal/tgclient"
)

type dialogMeta struct {
	chat                                             domain.Chat
	peer                                             tg.InputPeerClass
	contact, bot, muted, unread, archived, mentioned bool
}

func peerKey(p tg.InputPeerClass) string {
	switch v := p.(type) {
	case *tg.InputPeerUser:
		return "u" + strconv.FormatInt(v.UserID, 10)
	case *tg.InputPeerChat:
		return "g" + strconv.FormatInt(v.ChatID, 10)
	case *tg.InputPeerChannel:
		return "c" + strconv.FormatInt(v.ChannelID, 10)
	case *tg.InputPeerSelf:
		return "self"
	}
	return ""
}
func containsPeer(peers []tg.InputPeerClass, p tg.InputPeerClass) bool {
	key := peerKey(p)
	for _, v := range peers {
		if peerKey(v) == key {
			return true
		}
	}
	return false
}
func folderMatches(f *tg.DialogFilter, m dialogMeta) bool {
	if containsPeer(f.ExcludePeers, m.peer) {
		return false
	}
	if containsPeer(f.IncludePeers, m.peer) || containsPeer(f.PinnedPeers, m.peer) {
		return true
	}
	if f.ExcludeMuted && m.muted && !(m.mentioned && !m.archived) || f.ExcludeRead && !m.unread || f.ExcludeArchived && m.archived {
		return false
	}
	switch m.chat.Type {
	case "group":
		return f.Groups
	case "channel":
		return f.Broadcasts
	case "private":
		if m.bot {
			return f.Bots
		}
		if m.contact {
			return f.Contacts
		}
		return f.NonContacts
	}
	return false
}

// RefreshDirectory intentionally projects messages to timestamps before persistence.
// Neither raw responses nor message text are written to the catalog or logs.
func RefreshDirectory(ctx context.Context, st *store.Store, gate *tdl.Runner, storagePath, proxy, ntp string, a domain.Account) ([]domain.Chat, error) {
	if err := gate.AcquireContext(ctx); err != nil {
		return nil, err
	}
	defer gate.Release()
	fresh, err := st.Account(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	if fresh.Removed {
		return nil, fmt.Errorf("账户登录信息已删除")
	}
	a = fresh
	file, err := kv.New(kv.DriverFile, map[string]any{"path": storagePath})
	if err != nil {
		return nil, err
	}
	defer file.Close()
	db, err := file.Open(a.Namespace)
	if err != nil {
		return nil, err
	}
	client, err := tgclient.New(ctx, tgclient.Options{KV: db, Proxy: proxy, NTP: ntp, ReconnectTimeout: time.Minute}, false)
	if err != nil {
		return nil, err
	}
	chats := []domain.Chat{}
	folders := []domain.ChatFolder{}
	err = client.Run(ctx, func(ctx context.Context) error {
		api := client.API()
		filters, err := api.MessagesGetDialogFilters(ctx)
		if err != nil {
			return err
		}
		self, err := client.Self(ctx)
		if err != nil {
			return err
		}
		normalize := func(list []tg.InputPeerClass) {
			for i, p := range list {
				if _, ok := p.(*tg.InputPeerSelf); ok {
					list[i] = &tg.InputPeerUser{UserID: self.ID}
				}
			}
		}
		for _, raw := range filters.Filters {
			switch f := raw.(type) {
			case *tg.DialogFilter:
				normalize(f.IncludePeers)
				normalize(f.ExcludePeers)
				normalize(f.PinnedPeers)
			case *tg.DialogFilterChatlist:
				normalize(f.IncludePeers)
				normalize(f.PinnedPeers)
			}
		}
		defaults := map[string]int{}
		for _, entry := range []struct {
			kind string
			peer tg.InputNotifyPeerClass
		}{{"private", &tg.InputNotifyUsers{}}, {"group", &tg.InputNotifyChats{}}, {"channel", &tg.InputNotifyBroadcasts{}}} {
			settings, e := api.AccountGetNotifySettings(ctx, entry.peer)
			if e != nil {
				return e
			}
			defaults[entry.kind] = settings.MuteUntil
		}
		manager := peers.Options{Storage: storage.NewPeers(db)}.Build(api)
		meta := []dialogMeta{}
		seen := map[string]bool{}
		pinOrder := 0
		old, _ := st.Chats(ctx, a.ID, "")
		oldTopics := map[string][]domain.Topic{}
		for _, c := range old {
			oldTopics[c.ID] = c.Topics
		}
		for _, folder := range []int{0, 1} {
			query := dialogs.QueryFunc(func(ctx context.Context, offset dialogs.Request) (tg.MessagesDialogsClass, error) {
				return api.MessagesGetDialogs(ctx, directoryDialogRequest(folder, offset))
			})
			iter := newDirectoryIterator(query)
			for iter.Next(ctx) {
				e := iter.Value()
				d, ok := e.Dialog.(*tg.Dialog)
				if !ok || e.Deleted() {
					continue
				}
				k := peerKey(e.Peer)
				if seen[k] {
					continue
				}
				seen[k] = true
				m := dialogMeta{peer: e.Peer, archived: d.FolderID == 1, unread: d.UnreadCount > 0 || d.UnreadMark || d.UnreadMentionsCount > 0, mentioned: d.UnreadMentionsCount > 0}
				c := domain.Chat{AccountID: a.ID}
				var users []tg.UserClass
				var entities []tg.ChatClass
				forum := false
				switch p := e.Peer.(type) {
				case *tg.InputPeerUser:
					u, ok := e.Entities.User(p.UserID)
					if !ok || u.Deleted {
						continue
					}
					c.ID = strconv.FormatInt(u.ID, 10)
					c.Type = "private"
					c.VisibleName = strings.TrimSpace(u.FirstName + " " + u.LastName)
					c.Username = u.Username
					m.bot = u.Bot
					m.contact = u.Contact
					users = append(users, u)
				case *tg.InputPeerChat:
					g, ok := e.Entities.Chat(p.ChatID)
					if !ok {
						continue
					}
					c.ID = strconv.FormatInt(g.ID, 10)
					c.Type = "group"
					c.VisibleName = g.Title
					entities = append(entities, g)
				case *tg.InputPeerChannel:
					g, ok := e.Entities.Channel(p.ChannelID)
					if !ok {
						continue
					}
					c.ID = strconv.FormatInt(g.ID, 10)
					c.Type = "group"
					if g.Broadcast {
						c.Type = "channel"
					}
					c.VisibleName = g.Title
					c.Username = g.Username
					forum = g.Forum
					entities = append(entities, g)
				default:
					continue
				}
				if err = manager.Apply(ctx, users, entities); err != nil {
					return err
				}
				if e.Last != nil && e.Last.GetID() == d.TopMessage && fmt.Sprint(e.Last.GetPeerID()) == fmt.Sprint(d.Peer) {
					c.LastMessageAt = time.Unix(int64(e.Last.GetDate()), 0).UTC()
				}
				if d.Pinned {
					pinOrder++
					c.PinnedOrder = pinOrder
				}
				mute, ok := d.NotifySettings.GetMuteUntil()
				if !ok {
					mute = defaults[c.Type]
				}
				m.muted = int64(mute) > time.Now().Unix()
				if forum {
					c.Topics, err = directoryTopics(ctx, api, e.Peer)
					if err != nil {
						if ctx.Err() != nil {
							return ctx.Err()
						}
						c.Topics = oldTopics[c.ID]
					}
				}
				m.chat = c
				meta = append(meta, m)
			}
			if err = iter.Err(); err != nil {
				return err
			}
		}
		for _, raw := range filters.Filters {
			f := domain.ChatFolder{ChatIDs: []string{}, PinnedIDs: []string{}}
			var pinned []tg.InputPeerClass
			var matches func(dialogMeta) bool
			switch x := raw.(type) {
			case *tg.DialogFilterDefault:
				f.ID = "all"
				f.Title = "全部聊天"
				matches = func(dialogMeta) bool { return true }
			case *tg.DialogFilter:
				f.ID = strconv.Itoa(x.ID)
				f.Title = x.Title.Text
				f.Emoticon = x.Emoticon
				pinned = x.PinnedPeers
				matches = func(m dialogMeta) bool { return folderMatches(x, m) }
			case *tg.DialogFilterChatlist:
				f.ID = strconv.Itoa(x.ID)
				f.Title = x.Title.Text
				f.Emoticon = x.Emoticon
				pinned = x.PinnedPeers
				matches = func(m dialogMeta) bool {
					return containsPeer(x.IncludePeers, m.peer) || containsPeer(x.PinnedPeers, m.peer)
				}
			default:
				continue
			}
			for i, m := range meta {
				if matches(m) {
					f.ChatIDs = append(f.ChatIDs, m.chat.ID)
					meta[i].chat.FolderIDs = append(meta[i].chat.FolderIDs, f.ID)
				}
			}
			for _, p := range pinned {
				for _, m := range meta {
					if peerKey(p) == peerKey(m.peer) {
						f.PinnedIDs = append(f.PinnedIDs, m.chat.ID)
					}
				}
			}
			folders = append(folders, f)
		}
		allFound := false
		for _, f := range folders {
			allFound = allFound || f.ID == "all"
		}
		if !allFound {
			folders = append([]domain.ChatFolder{{ID: "all", Title: "全部聊天", ChatIDs: []string{}, PinnedIDs: []string{}}}, folders...)
		}
		for _, m := range meta {
			chats = append(chats, m.chat)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = st.SaveDirectory(ctx, a.ID, chats, folders); err != nil {
		return nil, err
	}
	return chats, nil
}

func directoryDialogRequest(folder int, offset dialogs.Request) *tg.MessagesGetDialogsRequest {
	r := &tg.MessagesGetDialogsRequest{OffsetDate: offset.OffsetDate, OffsetID: offset.OffsetID, OffsetPeer: offset.OffsetPeer, Limit: offset.Limit}
	// Folder 0 needs its flag even though its value is zero.
	r.SetFolderID(folder)
	return r
}

func directoryTopics(ctx context.Context, api *tg.Client, p tg.InputPeerClass) ([]domain.Topic, error) {
	out := []domain.Topic{}
	req := &tg.MessagesGetForumTopicsRequest{Peer: p, Limit: 100}
	seen := map[int]bool{}
	for {
		res, err := api.MessagesGetForumTopics(ctx, req)
		if err != nil {
			return nil, err
		}
		if len(res.Topics) == 0 {
			return out, nil
		}
		previous := req.OffsetTopic
		for _, raw := range res.Topics {
			if t, ok := raw.(*tg.ForumTopic); ok {
				if !seen[t.ID] {
					out = append(out, domain.Topic{ID: strconv.Itoa(t.ID), Title: t.Title})
					seen[t.ID] = true
				}
				req.OffsetTopic = t.ID
				req.OffsetID = t.TopMessage
				req.OffsetDate = t.Date
			}
		}
		if len(out) >= res.Count || len(res.Topics) < 100 {
			return out, nil
		}
		if previous == req.OffsetTopic {
			return nil, fmt.Errorf("话题分页未前进")
		}
		for _, raw := range res.Messages {
			if m, ok := raw.AsNotEmpty(); ok && m.GetID() == req.OffsetID {
				req.OffsetDate = m.GetDate()
			}
		}
	}
}
