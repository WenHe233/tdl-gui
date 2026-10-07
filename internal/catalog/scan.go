package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
	"github.com/iyear/tdl/core/storage"
	"github.com/iyear/tdl/core/tmedia"
	"github.com/iyear/tdl/core/util/tutil"
	"github.com/iyear/tdl/pkg/kv"
	"github.com/local/tdl-gui/internal/domain"
	"github.com/local/tdl-gui/internal/store"
	"github.com/local/tdl-gui/internal/tdl"
	"github.com/local/tdl-gui/internal/tgclient"
)

var errScanIdle = errors.New("扫描连接连续 2 分钟未响应，请检查网络、代理或校时设置后重试")

// Bound startup and every page, without imposing a total limit on large histories.
func withScanDeadline(ctx context.Context, timeout time.Duration, run func(context.Context, func()) error) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	timer := time.AfterFunc(timeout, func() { cancel(errScanIdle) })
	defer timer.Stop()
	err := run(ctx, func() { timer.Reset(timeout) })
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return err
}

// ScanTelegram shares the HTTPS-corrected clock and session gate used by chat refresh.
// Message payloads stay in memory; only parsed media is persisted by Service.Scan.
func ScanTelegram(ctx context.Context, st *store.Store, gate *tdl.Runner, path, proxy, ntp string, a domain.Account, o ScanOptions, cursor int64, reconnect ...time.Duration) ([]domain.Media, int64, error) {
	timeout := 5 * time.Minute
	if len(reconnect) > 0 {
		timeout = reconnect[0]
	}
	if err := gate.AcquireContext(ctx); err != nil {
		return nil, 0, err
	}
	defer gate.Release()
	a, err := st.Account(ctx, a.ID)
	if err != nil {
		return nil, 0, err
	}
	if a.Removed {
		return nil, 0, fmt.Errorf("账户登录信息已删除")
	}
	topic := 0
	if o.TopicID != "" {
		topic, err = strconv.Atoi(o.TopicID)
		if err != nil || topic < 0 {
			return nil, 0, fmt.Errorf("无效的话题 ID")
		}
	}
	file, err := kv.New(kv.DriverFile, map[string]any{"path": path})
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	db, err := file.Open(a.Namespace)
	if err != nil {
		return nil, 0, err
	}
	var items []domain.Media
	var maxID int64
	err = withScanDeadline(ctx, 2*time.Minute, func(ctx context.Context, progress func()) error {
		client, err := tgclient.New(ctx, tgclient.Options{KV: db, Proxy: proxy, NTP: ntp, ReconnectTimeout: timeout}, false)
		if err != nil {
			return err
		}
		return client.Run(ctx, func(ctx context.Context) error {
			manager := peers.Options{Storage: storage.NewPeers(db)}.Build(client.API())
			peer, err := tutil.GetInputPeer(ctx, manager, o.ChatID)
			if err != nil {
				return err
			}
			progress()
			var q messages.Query = query.NewQuery(client.API()).Messages().GetHistory(peer.InputPeer())
			if topic != 0 {
				q = query.NewQuery(client.API()).Messages().GetReplies(peer.InputPeer()).MsgID(topic)
			}
			items, maxID, err = scanPages(ctx, q, a.ID, o, cursor, progress)
			return err
		})
	})
	return items, maxID, err
}

func scanPages(ctx context.Context, q messages.Query, accountID string, o ScanOptions, cursor int64, progress func()) ([]domain.Media, int64, error) {
	items := []domain.Media{}
	maxID := cursor
	offset := 0
	date := 0
	if !o.To.IsZero() {
		date = int(o.To.Unix()) + 1
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		result, err := q.Query(ctx, messages.Request{OffsetID: offset, OffsetDate: date, Limit: 100})
		if err != nil {
			return nil, 0, err
		}
		progress()
		var page []tg.MessageClass
		terminal := false
		switch r := result.(type) {
		case *tg.MessagesMessages:
			page, terminal = r.Messages, true
		case *tg.MessagesMessagesSlice:
			page = r.Messages
		case *tg.MessagesChannelMessages:
			page = r.Messages
		default:
			return nil, 0, fmt.Errorf("无法读取媒体分页：%T", result)
		}
		if len(page) == 0 {
			return items, maxID, nil
		}
		sort.SliceStable(page, func(i, j int) bool { return page[i].GetID() > page[j].GetID() })
		next := page[len(page)-1].GetID()
		if next <= 0 || (offset > 0 && next >= offset) {
			return nil, 0, fmt.Errorf("媒体分页未前进")
		}
		for _, raw := range page {
			id := raw.GetID()
			if offset > 0 && id >= offset {
				continue
			}
			if int64(id) <= cursor {
				return items, maxID, nil
			}
			if int64(id) > maxID {
				maxID = int64(id)
			}
			msg, ok := raw.(*tg.Message)
			if !ok {
				continue
			}
			if !o.From.IsZero() && int64(msg.Date) < o.From.Unix() {
				return items, maxID, nil
			}
			if !o.To.IsZero() && int64(msg.Date) > o.To.Unix() {
				continue
			}
			media, ok := tmedia.GetMedia(msg)
			if !ok {
				continue
			}
			b, err := json.Marshal(msg)
			if err != nil {
				return nil, 0, err
			}
			dec := json.NewDecoder(bytes.NewReader(b))
			dec.UseNumber()
			var fields map[string]any
			if err = dec.Decode(&fields); err != nil {
				return nil, 0, err
			}
			m, ok := parseMedia(accountID, o.ChatID, strconv.Itoa(id), media.Name, int64(msg.Date), msg.Message, fields)
			if !ok {
				continue
			}
			if o.TopicID != "" {
				m.TopicID = o.TopicID
			}
			items = append(items, m)
			if o.LastN > 0 && len(items) >= o.LastN {
				return items, maxID, nil
			}
		}
		if terminal || len(page) < 100 {
			return items, maxID, nil
		}
		offset, date = next, 0
	}
}
