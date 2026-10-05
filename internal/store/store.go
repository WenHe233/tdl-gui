package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/local/tdl-gui/internal/domain"
)

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	// The desktop process can exit immediately after asking the worker to stop.
	// Merge committed WAL frames first so the next process never depends on an
	// orphaned WAL/SHM pair left behind by Windows process teardown.
	_, checkpointErr := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	closeErr := s.db.Close()
	if checkpointErr != nil {
		return checkpointErr
	}
	return closeErr
}

func (s *Store) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS accounts (
 id TEXT PRIMARY KEY, namespace TEXT NOT NULL UNIQUE, display_name TEXT NOT NULL,
 username TEXT NOT NULL DEFAULT '', phone TEXT NOT NULL DEFAULT '', user_id TEXT NOT NULL DEFAULT '',
 active INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS chats (
 account_id TEXT NOT NULL, id TEXT NOT NULL, type TEXT NOT NULL, visible_name TEXT NOT NULL,
 username TEXT NOT NULL DEFAULT '', topics_json TEXT NOT NULL DEFAULT '[]', updated_at TEXT NOT NULL,
 PRIMARY KEY(account_id,id), FOREIGN KEY(account_id) REFERENCES accounts(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS media (
 account_id TEXT NOT NULL, chat_id TEXT NOT NULL, message_id TEXT NOT NULL, media_id TEXT NOT NULL,
 grouped_id TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL, file_name TEXT NOT NULL, extension TEXT NOT NULL DEFAULT '',
 mime TEXT NOT NULL DEFAULT '', size INTEGER NOT NULL, caption TEXT NOT NULL DEFAULT '', message_date TEXT NOT NULL,
 duration INTEGER NOT NULL DEFAULT 0, width INTEGER NOT NULL DEFAULT 0, height INTEGER NOT NULL DEFAULT 0,
 thumb_path TEXT NOT NULL DEFAULT '', local_path TEXT NOT NULL DEFAULT '', downloaded INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(account_id,chat_id,message_id)
);
CREATE INDEX IF NOT EXISTS media_filter_idx ON media(account_id,chat_id,message_date,message_id);
CREATE INDEX IF NOT EXISTS media_dedupe_idx ON media(account_id,chat_id,media_id);
CREATE TABLE IF NOT EXISTS rules (id TEXT PRIMARY KEY, payload TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS plans (id TEXT PRIMARY KEY, payload TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS jobs (
 id TEXT PRIMARY KEY, plan_id TEXT NOT NULL, account_id TEXT NOT NULL, chat_id TEXT NOT NULL, state TEXT NOT NULL,
 total_files INTEGER NOT NULL, done_files INTEGER NOT NULL, failed_files INTEGER NOT NULL,
 total_bytes INTEGER NOT NULL, done_bytes INTEGER NOT NULL, error TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS job_items (
 job_id TEXT NOT NULL, media_id TEXT NOT NULL, chat_id TEXT NOT NULL, message_id TEXT NOT NULL,
 target_path TEXT NOT NULL, staging_path TEXT NOT NULL DEFAULT '', state TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0,
 size INTEGER NOT NULL, error TEXT NOT NULL DEFAULT '', PRIMARY KEY(job_id,chat_id,message_id),
 FOREIGN KEY(job_id) REFERENCES jobs(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS scans (
 account_id TEXT NOT NULL, chat_id TEXT NOT NULL, topic_id TEXT NOT NULL DEFAULT '', cursor TEXT NOT NULL DEFAULT '',
 updated_at TEXT NOT NULL, PRIMARY KEY(account_id,chat_id,topic_id)
);`
	_, err := s.db.ExecContext(ctx, schema)
	if err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info(media)")
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var def any
		if err = rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "topic_id" {
			found = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !found {
		_, err = s.db.ExecContext(ctx, "ALTER TABLE media ADD COLUMN topic_id TEXT NOT NULL DEFAULT ''")
	}
	return err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	return v, err
}

func (s *Store) SaveAccount(ctx context.Context, a domain.Account) error {
	now := time.Now().UTC()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	a.UpdatedAt = now
	if a.Active {
		if _, err := s.db.ExecContext(ctx, `UPDATE accounts SET active=0`); err != nil {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO accounts(id,namespace,display_name,username,phone,user_id,active,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET namespace=excluded.namespace,display_name=excluded.display_name,
username=excluded.username,phone=excluded.phone,user_id=excluded.user_id,active=excluded.active,updated_at=excluded.updated_at`,
		a.ID, a.Namespace, a.DisplayName, a.Username, a.Phone, a.UserID, a.Active, formatTime(a.CreatedAt), formatTime(a.UpdatedAt))
	return err
}

func (s *Store) Accounts(ctx context.Context) ([]domain.Account, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,namespace,display_name,username,phone,user_id,active,created_at,updated_at FROM accounts ORDER BY active DESC,display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Account, 0)
	for rows.Next() {
		var a domain.Account
		var active int
		var c, u string
		if err := rows.Scan(&a.ID, &a.Namespace, &a.DisplayName, &a.Username, &a.Phone, &a.UserID, &active, &c, &u); err != nil {
			return nil, err
		}
		a.Active = active == 1
		a.CreatedAt = parseTime(c)
		a.UpdatedAt = parseTime(u)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) ActiveAccount(ctx context.Context) (domain.Account, error) {
	var a domain.Account
	var active int
	var c, u string
	err := s.db.QueryRowContext(ctx, `SELECT id,namespace,display_name,username,phone,user_id,active,created_at,updated_at FROM accounts WHERE active=1 LIMIT 1`).Scan(&a.ID, &a.Namespace, &a.DisplayName, &a.Username, &a.Phone, &a.UserID, &active, &c, &u)
	a.Active = active == 1
	a.CreatedAt = parseTime(c)
	a.UpdatedAt = parseTime(u)
	return a, err
}

func (s *Store) SetActiveAccount(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE accounts SET active=0`); err != nil {
		return err
	}
	r, err := tx.ExecContext(ctx, `UPDATE accounts SET active=1,updated_at=? WHERE id=?`, formatTime(time.Now().UTC()), id)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return fmt.Errorf("account %q not found", id)
	}
	return tx.Commit()
}

func (s *Store) DeleteAccount(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM accounts WHERE id=?`, id)
	return err
}

func (s *Store) UpsertChats(ctx context.Context, chats []domain.Chat) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := formatTime(time.Now().UTC())
	for _, c := range chats {
		b, _ := json.Marshal(c.Topics)
		if _, err = tx.ExecContext(ctx, `INSERT INTO chats(account_id,id,type,visible_name,username,topics_json,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(account_id,id) DO UPDATE SET type=excluded.type,visible_name=excluded.visible_name,username=excluded.username,topics_json=excluded.topics_json,updated_at=excluded.updated_at`, c.AccountID, c.ID, c.Type, c.VisibleName, c.Username, string(b), now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Chats(ctx context.Context, accountID, query string) ([]domain.Chat, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT account_id,id,type,visible_name,username,topics_json FROM chats WHERE account_id=? AND (?='' OR visible_name LIKE '%'||?||'%' OR username LIKE '%'||?||'%') ORDER BY visible_name`, accountID, query, query, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Chat, 0)
	for rows.Next() {
		var c domain.Chat
		var topics string
		if err := rows.Scan(&c.AccountID, &c.ID, &c.Type, &c.VisibleName, &c.Username, &topics); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(topics), &c.Topics)
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) Chat(ctx context.Context, accountID, id string) (domain.Chat, error) {
	var c domain.Chat
	var topics string
	err := s.db.QueryRowContext(ctx, `SELECT account_id,id,type,visible_name,username,topics_json FROM chats WHERE account_id=? AND id=?`, accountID, id).Scan(&c.AccountID, &c.ID, &c.Type, &c.VisibleName, &c.Username, &topics)
	if err == nil {
		_ = json.Unmarshal([]byte(topics), &c.Topics)
	}
	return c, err
}
func (s *Store) Account(ctx context.Context, id string) (domain.Account, error) {
	var a domain.Account
	var active int
	var c, u string
	err := s.db.QueryRowContext(ctx, `SELECT id,namespace,display_name,username,phone,user_id,active,created_at,updated_at FROM accounts WHERE id=?`, id).Scan(&a.ID, &a.Namespace, &a.DisplayName, &a.Username, &a.Phone, &a.UserID, &active, &c, &u)
	a.Active = active == 1
	a.CreatedAt = parseTime(c)
	a.UpdatedAt = parseTime(u)
	return a, err
}

const insertMediaSQL = `INSERT INTO media(account_id,chat_id,message_id,media_id,topic_id,grouped_id,kind,file_name,extension,mime,size,caption,message_date,duration,width,height,thumb_path,local_path,downloaded) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(account_id,chat_id,message_id) DO UPDATE SET media_id=excluded.media_id,topic_id=excluded.topic_id,grouped_id=excluded.grouped_id,kind=excluded.kind,file_name=excluded.file_name,extension=excluded.extension,mime=excluded.mime,size=excluded.size,caption=excluded.caption,message_date=excluded.message_date,duration=excluded.duration,width=excluded.width,height=excluded.height,thumb_path=CASE WHEN excluded.thumb_path<>'' THEN excluded.thumb_path WHEN media.media_id=excluded.media_id AND media.size=excluded.size THEN media.thumb_path ELSE '' END,local_path=CASE WHEN media.media_id=excluded.media_id AND media.size=excluded.size THEN media.local_path ELSE '' END,downloaded=CASE WHEN media.media_id=excluded.media_id AND media.size=excluded.size THEN media.downloaded ELSE 0 END`

func saveMedia(ctx context.Context, tx *sql.Tx, items []domain.Media) error {
	for _, m := range items {
		_, err := tx.ExecContext(ctx, insertMediaSQL, m.AccountID, m.ChatID, m.MessageID, m.MediaID, m.TopicID, m.GroupedID, m.Kind, m.FileName, m.Extension, m.MIME, m.Size, m.Caption, formatTime(m.Date), m.Duration, m.Width, m.Height, m.ThumbPath, m.LocalPath, boolInt(m.Downloaded))
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SaveMedia(ctx context.Context, items []domain.Media) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = saveMedia(ctx, tx, items); err != nil {
		return err
	}
	return tx.Commit()
}

// ReplaceMedia atomically replaces a complete chat scan. It removes messages
// deleted from Telegram while retaining verified local paths and thumbnails
// when the underlying media identity and size did not change.
func (s *Store) ReplaceMedia(ctx context.Context, accountID, chatID string, items []domain.Media) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	type saved struct {
		mediaID, thumbPath, localPath string
		size                          int64
		downloaded                    bool
	}
	previous := make(map[string]saved)
	rows, err := tx.QueryContext(ctx, `SELECT message_id,media_id,size,thumb_path,local_path,downloaded FROM media WHERE account_id=? AND chat_id=?`, accountID, chatID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var messageID string
		var old saved
		var downloaded int
		if err = rows.Scan(&messageID, &old.mediaID, &old.size, &old.thumbPath, &old.localPath, &downloaded); err != nil {
			rows.Close()
			return err
		}
		old.downloaded = downloaded == 1
		previous[messageID] = old
	}
	if err = rows.Close(); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM media WHERE account_id=? AND chat_id=?`, accountID, chatID); err != nil {
		return err
	}
	for i := range items {
		if old, ok := previous[items[i].MessageID]; ok && old.mediaID == items[i].MediaID && old.size == items[i].Size {
			if items[i].ThumbPath == "" {
				items[i].ThumbPath = old.thumbPath
			}
			items[i].LocalPath = old.localPath
			items[i].Downloaded = old.downloaded
		}
	}
	if err = saveMedia(ctx, tx, items); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Media(ctx context.Context, accountID, chatID string) ([]domain.Media, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT account_id,chat_id,message_id,media_id,topic_id,grouped_id,kind,file_name,extension,mime,size,caption,message_date,duration,width,height,thumb_path,local_path,downloaded FROM media WHERE account_id=? AND chat_id=? ORDER BY message_date,CAST(message_id AS INTEGER)`, accountID, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Media, 0)
	for rows.Next() {
		var m domain.Media
		var d string
		var dl int
		if err := rows.Scan(&m.AccountID, &m.ChatID, &m.MessageID, &m.MediaID, &m.TopicID, &m.GroupedID, &m.Kind, &m.FileName, &m.Extension, &m.MIME, &m.Size, &m.Caption, &d, &m.Duration, &m.Width, &m.Height, &m.ThumbPath, &m.LocalPath, &dl); err != nil {
			return nil, err
		}
		m.Date = parseTime(d)
		m.Downloaded = dl == 1
		out = append(out, m)
	}
	return out, rows.Err()
}
func (s *Store) MediaPage(ctx context.Context, accountID, chatID string, offset, limit int, topics ...string) ([]domain.Media, error) {
	topic := ""
	if len(topics) > 0 {
		topic = topics[0]
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT account_id,chat_id,message_id,media_id,topic_id,grouped_id,kind,file_name,extension,mime,size,caption,message_date,duration,width,height,thumb_path,local_path,downloaded FROM media WHERE account_id=? AND chat_id=? AND (?='' OR topic_id=?) ORDER BY message_date DESC,CAST(message_id AS INTEGER) DESC LIMIT ? OFFSET ?`, accountID, chatID, topic, topic, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Media, 0, limit)
	for rows.Next() {
		var m domain.Media
		var d string
		var dl int
		if err := rows.Scan(&m.AccountID, &m.ChatID, &m.MessageID, &m.MediaID, &m.TopicID, &m.GroupedID, &m.Kind, &m.FileName, &m.Extension, &m.MIME, &m.Size, &m.Caption, &d, &m.Duration, &m.Width, &m.Height, &m.ThumbPath, &m.LocalPath, &dl); err != nil {
			return nil, err
		}
		m.Date = parseTime(d)
		m.Downloaded = dl == 1
		out = append(out, m)
	}
	return out, rows.Err()
}
func (s *Store) SetThumbnail(ctx context.Context, accountID, chatID, messageID, path string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE media SET thumb_path=? WHERE account_id=? AND chat_id=? AND message_id=?`, path, accountID, chatID, messageID)
	return err
}
func (s *Store) ClearThumbnails(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE media SET thumb_path=''`)
	return err
}
func (s *Store) ScanCursor(ctx context.Context, accountID, chatID, topicID string) (string, error) {
	var cursor string
	err := s.db.QueryRowContext(ctx, `SELECT cursor FROM scans WHERE account_id=? AND chat_id=? AND topic_id=?`, accountID, chatID, topicID).Scan(&cursor)
	return cursor, err
}
func (s *Store) SetScanCursor(ctx context.Context, accountID, chatID, topicID, cursor string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO scans(account_id,chat_id,topic_id,cursor,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(account_id,chat_id,topic_id) DO UPDATE SET cursor=excluded.cursor,updated_at=excluded.updated_at`, accountID, chatID, topicID, cursor, formatTime(time.Now().UTC()))
	return err
}

func (s *Store) MarkDownloaded(ctx context.Context, accountID, chatID, mediaID, path string, size int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE media SET local_path=?,downloaded=1 WHERE account_id=? AND chat_id=? AND media_id=? AND size=?`, path, accountID, chatID, mediaID, size)
	return err
}

func (s *Store) SaveRule(ctx context.Context, r domain.Rule) error {
	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO rules(id,payload,created_at,updated_at) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload,updated_at=excluded.updated_at`, r.ID, string(b), formatTime(r.CreatedAt), formatTime(r.UpdatedAt))
	return err
}
func (s *Store) Rules(ctx context.Context) ([]domain.Rule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM rules ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Rule, 0)
	for rows.Next() {
		var b string
		var r domain.Rule
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(b), &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) Rule(ctx context.Context, id string) (domain.Rule, error) {
	var b string
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM rules WHERE id=?`, id).Scan(&b)
	var r domain.Rule
	if err == nil {
		err = json.Unmarshal([]byte(b), &r)
	}
	return r, err
}
func (s *Store) DeleteRule(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM rules WHERE id=?`, id)
	return err
}

func (s *Store) SavePlan(ctx context.Context, p domain.DownloadPlan) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO plans(id,payload,created_at) VALUES(?,?,?)`, p.ID, string(b), formatTime(p.CreatedAt))
	return err
}
func (s *Store) Plan(ctx context.Context, id string) (domain.DownloadPlan, error) {
	var b string
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM plans WHERE id=?`, id).Scan(&b)
	var p domain.DownloadPlan
	if err == nil {
		err = json.Unmarshal([]byte(b), &p)
	}
	return p, err
}

func (s *Store) CreateJob(ctx context.Context, j domain.Job, items []domain.JobItem) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO jobs(id,plan_id,account_id,chat_id,state,total_files,done_files,failed_files,total_bytes,done_bytes,error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, j.ID, j.PlanID, j.AccountID, j.ChatID, j.State, j.TotalFiles, j.DoneFiles, j.FailedFiles, j.TotalBytes, j.DoneBytes, j.Error, formatTime(j.CreatedAt), formatTime(j.UpdatedAt))
	if err != nil {
		return err
	}
	for _, it := range items {
		_, err = tx.ExecContext(ctx, `INSERT INTO job_items(job_id,media_id,chat_id,message_id,target_path,staging_path,state,attempts,size,error) VALUES(?,?,?,?,?,?,?,?,?,?)`, it.JobID, it.MediaID, it.ChatID, it.MessageID, it.TargetPath, it.StagingPath, it.State, it.Attempts, it.Size, it.Error)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) UpdateJob(ctx context.Context, j domain.Job) error {
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET state=?,done_files=?,failed_files=?,done_bytes=?,error=?,updated_at=? WHERE id=?`, j.State, j.DoneFiles, j.FailedFiles, j.DoneBytes, j.Error, formatTime(time.Now().UTC()), j.ID)
	return err
}
func (s *Store) UpdateJobItem(ctx context.Context, it domain.JobItem) error {
	_, err := s.db.ExecContext(ctx, `UPDATE job_items SET target_path=?,staging_path=?,state=?,attempts=?,error=? WHERE job_id=? AND chat_id=? AND message_id=?`, it.TargetPath, it.StagingPath, it.State, it.Attempts, it.Error, it.JobID, it.ChatID, it.MessageID)
	return err
}
func (s *Store) Jobs(ctx context.Context) ([]domain.Job, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,plan_id,account_id,chat_id,state,total_files,done_files,failed_files,total_bytes,done_bytes,error,created_at,updated_at FROM jobs ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Job, 0)
	for rows.Next() {
		var j domain.Job
		var c, u string
		if err := rows.Scan(&j.ID, &j.PlanID, &j.AccountID, &j.ChatID, &j.State, &j.TotalFiles, &j.DoneFiles, &j.FailedFiles, &j.TotalBytes, &j.DoneBytes, &j.Error, &c, &u); err != nil {
			return nil, err
		}
		j.CreatedAt = parseTime(c)
		j.UpdatedAt = parseTime(u)
		out = append(out, j)
	}
	return out, rows.Err()
}

// Running processes do not survive a worker restart. Keep their fixed items resumable.
func (s *Store) RecoverJobs(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE job_items SET state='queued',error='' WHERE state='downloading' AND job_id IN (SELECT id FROM jobs WHERE state='running')`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE jobs SET state='paused',error='程序已重新启动，可恢复任务',updated_at=? WHERE state='running'`, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Job(ctx context.Context, id string) (domain.Job, []domain.JobItem, error) {
	var j domain.Job
	var c, u string
	err := s.db.QueryRowContext(ctx, `SELECT id,plan_id,account_id,chat_id,state,total_files,done_files,failed_files,total_bytes,done_bytes,error,created_at,updated_at FROM jobs WHERE id=?`, id).Scan(&j.ID, &j.PlanID, &j.AccountID, &j.ChatID, &j.State, &j.TotalFiles, &j.DoneFiles, &j.FailedFiles, &j.TotalBytes, &j.DoneBytes, &j.Error, &c, &u)
	if err != nil {
		return j, nil, err
	}
	j.CreatedAt = parseTime(c)
	j.UpdatedAt = parseTime(u)
	rows, err := s.db.QueryContext(ctx, `SELECT job_id,media_id,chat_id,message_id,target_path,staging_path,state,attempts,size,error FROM job_items WHERE job_id=? ORDER BY message_id`, id)
	if err != nil {
		return j, nil, err
	}
	defer rows.Close()
	items := make([]domain.JobItem, 0)
	for rows.Next() {
		var it domain.JobItem
		if err := rows.Scan(&it.JobID, &it.MediaID, &it.ChatID, &it.MessageID, &it.TargetPath, &it.StagingPath, &it.State, &it.Attempts, &it.Size, &it.Error); err != nil {
			return j, nil, err
		}
		items = append(items, it)
	}
	return j, items, rows.Err()
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func parseTime(v string) time.Time  { t, _ := time.Parse(time.RFC3339Nano, v); return t }
