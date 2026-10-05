package catalog

import (
	"context"
	"fmt"

	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
)

// The gotd v0.140 iterator queries once more after consuming a terminal page,
// before it checks lastBatch. End locally instead of sending the same cursor
// again and mistaking normal completion for a pagination cycle.
type directoryPager struct {
	fetch   dialogs.QueryFunc
	done    bool
	visited map[string]bool
	seen    map[string]bool
}

func newDirectoryIterator(fetch dialogs.QueryFunc) *dialogs.Iterator {
	return dialogs.NewIterator(&directoryPager{fetch: fetch, visited: map[string]bool{}, seen: map[string]bool{}}, 100)
}

func (p *directoryPager) Query(ctx context.Context, offset dialogs.Request) (tg.MessagesDialogsClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.done {
		return &tg.MessagesDialogs{}, nil
	}
	key := fmt.Sprintf("%d/%d/%s", offset.OffsetDate, offset.OffsetID, peerKey(offset.OffsetPeer))
	if p.visited[key] {
		return nil, fmt.Errorf("聊天分页未前进")
	}
	p.visited[key] = true
	result, err := p.fetch(ctx, offset)
	if err != nil {
		return nil, err
	}
	switch page := result.(type) {
	case *tg.MessagesDialogs:
		p.done = true
	case *tg.MessagesDialogsSlice:
		for _, dialog := range page.Dialogs {
			// Include inaccessible peers in the server count without retaining messages.
			peer := dialog.GetPeer()
			p.seen[fmt.Sprintf("%T/%v", peer, peer)] = true
		}
		p.done = len(page.Dialogs) == 0 || (page.Count > 0 && len(p.seen) >= page.Count)
		if p.done {
			// No offset peer is needed for the final page, which may end in an
			// inaccessible chat or an empty/deleted top message.
			return &tg.MessagesDialogs{Dialogs: page.Dialogs, Messages: page.Messages, Chats: page.Chats, Users: page.Users}, nil
		}
	}
	return result, nil
}
