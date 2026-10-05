package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/local/tdl-gui/internal/domain"
	"github.com/local/tdl-gui/internal/tdl"
)

type Store interface {
	UpsertChats(context.Context, []domain.Chat) error
	Chats(context.Context, string, string) ([]domain.Chat, error)
	SaveMedia(context.Context, []domain.Media) error
	ReplaceMedia(context.Context, string, string, []domain.Media) error
	Media(context.Context, string, string) ([]domain.Media, error)
	ScanCursor(context.Context, string, string, string) (string, error)
	SetScanCursor(context.Context, string, string, string, string) error
}
type Service struct {
	store  Store
	runner *tdl.Runner
	cache  string
}

func New(store Store, runner *tdl.Runner, cache string) *Service {
	return &Service{store: store, runner: runner, cache: cache}
}

type tdlChat struct {
	ID          json.Number `json:"id"`
	Type        string      `json:"type"`
	VisibleName string      `json:"visible_name"`
	Username    string      `json:"username"`
	Topics      []struct {
		ID    json.Number `json:"id"`
		Title string      `json:"title"`
	} `json:"topics"`
}

func (s *Service) RefreshChats(ctx context.Context, a domain.Account) ([]domain.Chat, error) {
	out, err := s.runner.Run(ctx, a.Namespace, "chat", "ls", "--output", "json")
	if err != nil {
		return nil, err
	}
	payload := jsonPayload(out, '[', ']')
	dec := json.NewDecoder(strings.NewReader(string(payload)))
	dec.UseNumber()
	var raw []tdlChat
	if err = dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse tdl chat list: %w", err)
	}
	chats := make([]domain.Chat, 0, len(raw))
	for _, r := range raw {
		c := domain.Chat{AccountID: a.ID, ID: r.ID.String(), Type: r.Type, VisibleName: r.VisibleName, Username: r.Username}
		for _, t := range r.Topics {
			c.Topics = append(c.Topics, domain.Topic{ID: t.ID.String(), Title: t.Title})
		}
		chats = append(chats, c)
	}
	if err = s.store.UpsertChats(ctx, chats); err != nil {
		return nil, err
	}
	return chats, nil
}

type exported struct {
	ID       json.Number `json:"id"`
	Messages []struct {
		ID   json.Number    `json:"id"`
		File string         `json:"file"`
		Date int64          `json:"date"`
		Text string         `json:"text"`
		Raw  map[string]any `json:"raw"`
	} `json:"messages"`
}

type ScanOptions struct {
	ChatID, TopicID string
	From, To        time.Time
	LastN           int
	Rescan          bool
}

func (s *Service) Scan(ctx context.Context, a domain.Account, o ScanOptions) ([]domain.Media, error) {
	if !o.From.IsZero() && !o.To.IsZero() && o.From.After(o.To) {
		return nil, fmt.Errorf("开始时间不能晚于结束时间")
	}
	if o.ChatID == "" {
		return nil, fmt.Errorf("chat id is required")
	}
	tmp, err := os.CreateTemp(s.cache, "scan-*.json")
	if err != nil {
		return nil, err
	}
	path := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(path)
	args := []string{"chat", "export", "--chat", o.ChatID, "--output", path, "--with-content", "--raw"}
	if o.TopicID != "" {
		args = append(args, "--topic", o.TopicID)
	}
	incremental := false
	if o.LastN > 0 {
		args = append(args, "--type", "last", "--input", strconv.Itoa(o.LastN))
	} else if !o.From.IsZero() || !o.To.IsZero() {
		from := int64(0)
		to := time.Now().Unix()
		if !o.From.IsZero() {
			from = o.From.Unix()
		}
		if !o.To.IsZero() {
			to = o.To.Unix()
		}
		args = append(args, "--type", "time", "--input", fmt.Sprintf("%d,%d", from, to))
	} else if !o.Rescan {
		if cursor, e := s.store.ScanCursor(ctx, a.ID, o.ChatID, o.TopicID); e == nil {
			if last, e := strconv.ParseInt(cursor, 10, 64); e == nil && last > 0 {
				args = append(args, "--type", "id", "--input", strconv.FormatInt(last+1, 10))
				incremental = true
			}
		}
	}
	if _, err = s.runner.Run(ctx, a.Namespace, args...); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.UseNumber()
	var data exported
	if err = dec.Decode(&data); err != nil {
		return nil, fmt.Errorf("parse tdl media export: %w", err)
	}
	items := make([]domain.Media, 0, len(data.Messages))
	maxID := int64(0)
	for _, msg := range data.Messages {
		if id, e := msg.ID.Int64(); e == nil && id > maxID {
			maxID = id
		}
		m, ok := parseMedia(a.ID, o.ChatID, msg.ID.String(), msg.File, msg.Date, msg.Text, msg.Raw)
		if ok {
			if o.TopicID != "" {
				m.TopicID = o.TopicID
			}
			items = append(items, m)
		}
	}
	fullRescan := o.Rescan && o.TopicID == "" && o.From.IsZero() && o.To.IsZero() && o.LastN == 0
	if fullRescan {
		err = s.store.ReplaceMedia(ctx, a.ID, o.ChatID, items)
	} else {
		err = s.store.SaveMedia(ctx, items)
	}
	if err != nil {
		return nil, err
	}
	if o.From.IsZero() && o.To.IsZero() && o.LastN == 0 {
		if maxID == 0 && incremental {
			if cursor, e := s.store.ScanCursor(ctx, a.ID, o.ChatID, o.TopicID); e == nil {
				maxID, _ = strconv.ParseInt(cursor, 10, 64)
			}
		}
		if maxID > 0 {
			_ = s.store.SetScanCursor(ctx, a.ID, o.ChatID, o.TopicID, strconv.FormatInt(maxID, 10))
		}
	}
	return items, nil
}

func parseMedia(accountID, chatID, messageID, fileName string, date int64, caption string, raw map[string]any) (domain.Media, bool) {
	if date == 0 {
		date = numberAt(raw, "date")
	}
	if caption == "" {
		caption = stringAt(raw, "message")
	}
	media := mapAt(raw, "media")
	if len(media) == 0 {
		return domain.Media{}, false
	}
	doc := mapAt(media, "document")
	photo := mapAt(media, "photo")
	kind := "document"
	source := doc
	if len(photo) > 0 {
		kind = "photo"
		source = photo
	}
	if len(source) == 0 {
		return domain.Media{}, false
	}
	mediaID := stringNumberAt(source, "id")
	if mediaID == "" {
		mediaID = chatID + ":" + messageID
	}
	size := numberAt(source, "size")
	mime := stringAt(source, "mime_type")
	duration, width, height := 0, 0, 0
	attrs := sliceAt(source, "attributes")
	for _, v := range attrs {
		a, _ := v.(map[string]any)
		t := strings.ToLower(stringAt(a, "_") + stringAt(a, "type"))
		if n := stringAt(a, "file_name"); n != "" {
			fileName = n
		}
		if d := numberAt(a, "duration"); d > 0 {
			duration = int(d)
		}
		if w := numberAt(a, "w"); w > 0 {
			width = int(w)
		}
		if h := numberAt(a, "h"); h > 0 {
			height = int(h)
		}
		switch {
		case boolAt(a, "voice"):
			kind = "voice"
		case strings.Contains(t, "sticker") || strings.Contains(mime, "tgsticker") || (mime == "image/webp" && stringAt(a, "alt") != ""):
			kind = "sticker"
		case strings.Contains(t, "animated") || mime == "image/gif":
			kind = "animation"
		case strings.Contains(t, "video") || mime != "" && strings.HasPrefix(mime, "video/"):
			kind = "video"
		case strings.Contains(t, "audio") || strings.HasPrefix(mime, "audio/"):
			kind = "audio"
		case strings.Contains(t, "sticker"):
			kind = "sticker"
		case strings.Contains(t, "animated"):
			kind = "animation"
		}
	}
	if kind == "document" {
		kind = kindFromFile(mime, fileName)
	}
	if len(photo) > 0 {
		sizes := sliceAt(photo, "sizes")
		for _, v := range sizes {
			x, _ := v.(map[string]any)
			n := numberAt(x, "size")
			for _, progressive := range sliceAt(x, "sizes") {
				if p := numberValue(progressive); p > n {
					n = p
				}
			}
			if n > size {
				size = n
			}
			if w := numberAt(x, "w"); w > int64(width) {
				width = int(w)
			}
			if h := numberAt(x, "h"); h > int64(height) {
				height = int(h)
			}
		}
	}
	if fileName == "" {
		ext := extensionFor(mime, kind)
		fileName = kind + "_" + messageID + ext
	}
	ext := strings.ToLower(filepath.Ext(fileName))
	topic := "0"
	reply := mapAt(raw, "reply_to")
	if boolAt(reply, "forum_topic") {
		topic = stringNumberAt(reply, "reply_to_top_id")
		if topic == "" || topic == "0" {
			topic = stringNumberAt(reply, "reply_to_msg_id")
		}
	}
	return domain.Media{TopicID: topic, AccountID: accountID, ChatID: chatID, MessageID: messageID, MediaID: mediaID, GroupedID: stringNumberAt(raw, "grouped_id"), Kind: kind, FileName: fileName, Extension: ext, MIME: mime, Size: size, Caption: caption, Date: time.Unix(date, 0), Duration: duration, Width: width, Height: height}, true
}

func kindFromFile(mimeType, fileName string) string {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	ext := strings.ToLower(filepath.Ext(fileName))
	switch {
	case strings.HasPrefix(mimeType, "video/"), extIn(ext, ".mp4", ".mov", ".m4v", ".mkv", ".webm", ".avi", ".wmv", ".flv", ".mpeg", ".mpg", ".3gp"):
		return "video"
	case mimeType == "image/gif", ext == ".gif":
		return "animation"
	case strings.HasPrefix(mimeType, "audio/"), extIn(ext, ".mp3", ".m4a", ".aac", ".flac", ".wav", ".ogg", ".opus", ".wma"):
		return "audio"
	default:
		return "document"
	}
}

func extIn(ext string, values ...string) bool {
	for _, value := range values {
		if ext == value {
			return true
		}
	}
	return false
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func jsonPayload(b []byte, start, end byte) []byte {
	clean := ansiRE.ReplaceAll(b, nil)
	i := strings.IndexByte(string(clean), start)
	j := strings.LastIndexByte(string(clean), end)
	if i >= 0 && j >= i {
		return clean[i : j+1]
	}
	return clean
}
func key(m map[string]any, name string) any {
	for k, v := range m {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return nil
}
func mapAt(m map[string]any, name string) map[string]any {
	v, _ := key(m, name).(map[string]any)
	return v
}
func sliceAt(m map[string]any, name string) []any { v, _ := key(m, name).([]any); return v }
func stringAt(m map[string]any, name string) string {
	v := key(m, name)
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func numberAt(m map[string]any, name string) int64 {
	return numberValue(key(m, name))
}
func numberValue(v any) int64 {
	switch n := v.(type) {
	case json.Number:
		x, _ := n.Int64()
		return x
	case float64:
		return int64(n)
	case int64:
		return n
	case string:
		x, _ := strconv.ParseInt(n, 10, 64)
		return x
	}
	return 0
}
func stringNumberAt(m map[string]any, name string) string {
	v := key(m, name)
	switch n := v.(type) {
	case json.Number:
		return n.String()
	case float64:
		return strconv.FormatInt(int64(n), 10)
	case int64:
		return strconv.FormatInt(n, 10)
	case string:
		return n
	}
	return ""
}
func boolAt(m map[string]any, name string) bool { v, _ := key(m, name).(bool); return v }
func extensionFor(mime, kind string) string {
	switch strings.ToLower(mime) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "video/mp4":
		return ".mp4"
	case "audio/mpeg":
		return ".mp3"
	case "audio/ogg":
		return ".ogg"
	}
	if kind == "photo" {
		return ".jpg"
	}
	return ""
}
