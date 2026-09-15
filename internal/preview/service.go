package preview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
	"github.com/iyear/tdl/core/dcpool"
	"github.com/iyear/tdl/core/storage"
	"github.com/iyear/tdl/core/tmedia"
	"github.com/iyear/tdl/core/util/tutil"
	"github.com/iyear/tdl/pkg/kv"
	"github.com/iyear/tdl/pkg/tclient"
	"github.com/local/tdl-gui/internal/domain"
	tdlrunner "github.com/local/tdl-gui/internal/tdl"
)

type Store interface {
	Account(context.Context, string) (domain.Account, error)
	SetThumbnail(context.Context, string, string, string, string) error
}
type Service struct {
	store                          Store
	storagePath, cache, proxy, ntp string
	maxBytes                       int64
	gate                           *tdlrunner.Runner
}
type Ref struct {
	ChatID    string `json:"chatId"`
	MessageID string `json:"messageId"`
}

func New(store Store, storagePath, cache, proxy, ntp string, maxBytes int64, gate *tdlrunner.Runner) *Service {
	return &Service{store: store, storagePath: storagePath, cache: cache, proxy: proxy, ntp: ntp, maxBytes: maxBytes, gate: gate}
}

func (s *Service) Thumbnail(ctx context.Context, accountID, chatID, messageID string) (string, error) {
	result, err := s.Thumbnails(ctx, accountID, []Ref{{ChatID: chatID, MessageID: messageID}})
	if err != nil {
		return "", err
	}
	if result[messageID] == "" {
		return "", errors.New("media has no downloadable thumbnail")
	}
	return result[messageID], nil
}

// Avatars downloads and caches the current profile photo for the requested
// chats. Missing photos and individual Telegram errors are intentionally
// omitted so one inaccessible peer does not prevent the rest of the list.
func (s *Service) Avatars(ctx context.Context, accountID string, chatIDs []string) (map[string]string, error) {
	s.gate.Acquire()
	defer s.gate.Release()
	a, err := s.store.Account(ctx, accountID)
	if err != nil {
		return nil, err
	}
	storageFile, err := kv.New(kv.DriverFile, map[string]any{"path": s.storagePath})
	if err != nil {
		return nil, err
	}
	defer storageFile.Close()
	kvd, err := storageFile.Open(a.Namespace)
	if err != nil {
		return nil, err
	}
	client, err := tclient.New(ctx, tclient.Options{KV: kvd, Proxy: s.proxy, NTP: s.ntp, ReconnectTimeout: 5 * time.Minute}, false)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	err = client.Run(ctx, func(ctx context.Context) error {
		status, e := client.Auth().Status(ctx)
		if e != nil {
			return e
		}
		if !status.Authorized {
			return errors.New("account is not authorized")
		}
		manager := peers.Options{Storage: storage.NewPeers(kvd)}.Build(client.API())
		pool := dcpool.NewPool(client, 2)
		defer pool.Close()
		for _, chatID := range chatIDs {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			dir := filepath.Join(s.cache, "avatars", accountID)
			if e = os.MkdirAll(dir, 0o700); e != nil {
				continue
			}
			path := filepath.Join(dir, chatID+".jpg")
			if st, statErr := os.Stat(path); statErr == nil && st.Size() > 0 {
				result[chatID] = path
				continue
			}
			peer, peerErr := tutil.GetInputPeer(ctx, manager, chatID)
			if peerErr != nil {
				continue
			}
			photo, ok, photoErr := peer.Photo(ctx)
			if photoErr != nil || !ok || photo == nil {
				continue
			}
			avatar, ok := photoThumb(photo)
			if !ok {
				continue
			}
			if _, e = downloader.NewDownloader().Download(pool.Client(ctx, avatar.DC), avatar.InputFileLoc).ToPath(ctx, path); e != nil {
				_ = os.Remove(path)
				continue
			}
			result[chatID] = path
		}
		return nil
	})
	return result, err
}

func (s *Service) Thumbnails(ctx context.Context, accountID string, refs []Ref) (map[string]string, error) {
	s.gate.Acquire()
	defer s.gate.Release()
	a, err := s.store.Account(ctx, accountID)
	if err != nil {
		return nil, err
	}
	storageFile, err := kv.New(kv.DriverFile, map[string]any{"path": s.storagePath})
	if err != nil {
		return nil, err
	}
	defer storageFile.Close()
	kvd, err := storageFile.Open(a.Namespace)
	if err != nil {
		return nil, err
	}
	client, err := tclient.New(ctx, tclient.Options{KV: kvd, Proxy: s.proxy, NTP: s.ntp, ReconnectTimeout: 5 * time.Minute}, false)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	err = client.Run(ctx, func(ctx context.Context) error {
		status, e := client.Auth().Status(ctx)
		if e != nil {
			return e
		}
		if !status.Authorized {
			return errors.New("account is not authorized")
		}
		manager := peers.Options{Storage: storage.NewPeers(kvd)}.Build(client.API())
		pool := dcpool.NewPool(client, 2)
		defer pool.Close()
		peerCache := make(map[string]peers.Peer)
		for _, ref := range refs {
			dir := filepath.Join(s.cache, "thumbs", accountID, ref.ChatID)
			if e := os.MkdirAll(dir, 0o700); e != nil {
				continue
			}
			path := filepath.Join(dir, ref.MessageID+".jpg")
			if st, e := os.Stat(path); e == nil && st.Size() > 0 {
				result[ref.MessageID] = path
				continue
			}
			peer := peerCache[ref.ChatID]
			if peer == nil {
				peer, e = tutil.GetInputPeer(ctx, manager, ref.ChatID)
				if e != nil {
					continue
				}
				peerCache[ref.ChatID] = peer
			}
			id, e := strconv.Atoi(ref.MessageID)
			if e != nil {
				continue
			}
			msg, e := tutil.GetSingleMessage(ctx, client.API(), peer.InputPeer(), id)
			if e != nil {
				continue
			}
			thumb, ok := thumbnailMedia(msg)
			if !ok {
				continue
			}
			if _, e = downloader.NewDownloader().Download(pool.Client(ctx, thumb.DC), thumb.InputFileLoc).ToPath(ctx, path); e != nil {
				_ = os.Remove(path)
				continue
			}
			result[ref.MessageID] = path
			_ = s.store.SetThumbnail(ctx, accountID, ref.ChatID, ref.MessageID, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.prune()
	return result, nil
}

func thumbnailMedia(msg *tg.Message) (*tmedia.Media, bool) {
	switch m := msg.Media.(type) {
	case *tg.MessageMediaDocument:
		if d, ok := m.Document.(*tg.Document); ok {
			return tmedia.GetDocumentThumb(d)
		}
	case *tg.MessageMediaPhoto:
		if p, ok := m.Photo.(*tg.Photo); ok {
			return photoThumb(p)
		}
	}
	return nil, false
}
func photoThumb(p *tg.Photo) (*tmedia.Media, bool) {
	var typ string
	size := 0
	for _, raw := range p.Sizes {
		switch x := raw.(type) {
		case *tg.PhotoSize:
			if x.W <= 640 && x.Size >= size {
				typ, size = x.Type, x.Size
			}
		case *tg.PhotoSizeProgressive:
			if x.W <= 640 && len(x.Sizes) > 0 && x.Sizes[len(x.Sizes)-1] >= size {
				typ, size = x.Type, x.Sizes[len(x.Sizes)-1]
			}
		}
	}
	if typ == "" {
		typ, size, _ = tmedia.GetPhotoSize(p.Sizes)
	}
	if typ == "" || size == 0 {
		return nil, false
	}
	return &tmedia.Media{InputFileLoc: &tg.InputPhotoFileLocation{ID: p.ID, AccessHash: p.AccessHash, FileReference: p.FileReference, ThumbSize: typ}, Name: "thumb.jpg", Size: int64(size), DC: p.DCID, Date: int64(p.Date)}, true
}
func (s *Service) prune() {
	if s.maxBytes <= 0 {
		return
	}
	root := filepath.Join(s.cache, "thumbs")
	type entry struct {
		path string
		size int64
		mod  time.Time
	}
	var all []entry
	var total int64
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			all = append(all, entry{path: path, size: info.Size(), mod: info.ModTime()})
			total += info.Size()
		}
		return nil
	})
	if total <= s.maxBytes {
		return
	}
	sort.Slice(all, func(i, j int) bool { return all[i].mod.Before(all[j].mod) })
	for _, e := range all {
		if total <= s.maxBytes {
			break
		}
		if os.Remove(e.path) == nil {
			total -= e.size
		}
	}
}
