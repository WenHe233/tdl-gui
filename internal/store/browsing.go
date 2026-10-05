package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/local/tdl-gui/internal/domain"
)

func (s *Store) migrateBrowsing(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info(accounts)")
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, nn, pk int
		var name, typ string
		var def any
		if err = rows.Scan(&cid, &name, &typ, &nn, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		found = found || name == "removed"
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !found {
		if _, err = s.db.ExecContext(ctx, "ALTER TABLE accounts ADD COLUMN removed INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
	}
	_, err = s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS chat_directory(account_id TEXT PRIMARY KEY, chats TEXT NOT NULL, folders TEXT NOT NULL, updated_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS retired_namespaces(namespace TEXT PRIMARY KEY)`)
	return err
}

// Directory snapshots contain chat metadata only, never Telegram message bodies.
func (s *Store) SaveDirectory(ctx context.Context, accountID string, chats []domain.Chat, folders []domain.ChatFolder) error {
	cb, err := json.Marshal(chats)
	if err != nil {
		return err
	}
	fb, err := json.Marshal(folders)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO chat_directory(account_id,chats,folders,updated_at) VALUES(?,?,?,?) ON CONFLICT(account_id) DO UPDATE SET chats=excluded.chats,folders=excluded.folders,updated_at=excluded.updated_at`, accountID, string(cb), string(fb), formatTime(time.Now().UTC()))
	return err
}
func (s *Store) Directory(ctx context.Context, accountID string) ([]domain.Chat, []domain.ChatFolder, error) {
	var cb, fb string
	err := s.db.QueryRowContext(ctx, `SELECT chats,folders FROM chat_directory WHERE account_id=?`, accountID).Scan(&cb, &fb)
	if err != nil {
		return nil, nil, err
	}
	chats := []domain.Chat{}
	folders := []domain.ChatFolder{}
	if err = json.Unmarshal([]byte(cb), &chats); err != nil {
		return nil, nil, err
	}
	err = json.Unmarshal([]byte(fb), &folders)
	return chats, folders, err
}
func (s *Store) Folders(ctx context.Context, accountID string) ([]domain.ChatFolder, error) {
	chats, folders, err := s.Directory(ctx, accountID)
	if err == sql.ErrNoRows {
		chats, err = s.Chats(ctx, accountID, "")
	}
	if err != nil {
		return nil, err
	}
	return directoryFolders(chats, folders), nil
}

// System folders are derived from the snapshot, including older snapshots that
// predate archived metadata. Telegram's custom folder order stays unchanged.
func directoryFolders(chats []domain.Chat, folders []domain.ChatFolder) []domain.ChatFolder {
	all := domain.ChatFolder{ID: "all", Title: "全部聊天", ChatIDs: []string{}, PinnedIDs: []string{}}
	archived := domain.ChatFolder{ID: "archived", Title: "已归档", ChatIDs: []string{}, PinnedIDs: []string{}}
	pinned := append([]domain.Chat(nil), chats...)
	sort.SliceStable(pinned, func(i, j int) bool { return pinned[i].PinnedOrder < pinned[j].PinnedOrder })
	for _, c := range chats {
		f := &all
		if c.Archived {
			f = &archived
		}
		f.ChatIDs = append(f.ChatIDs, c.ID)
	}
	for _, c := range pinned {
		if c.PinnedOrder <= 0 {
			continue
		}
		f := &all
		if c.Archived {
			f = &archived
		}
		f.PinnedIDs = append(f.PinnedIDs, c.ID)
	}
	out := make([]domain.ChatFolder, 0, len(folders)+2)
	allFound := false
	for _, f := range folders {
		switch f.ID {
		case "all":
			if !allFound {
				out = append(out, all, archived)
				allFound = true
			}
		case "archived":
			// Never retain a stale copy of the derived system folder.
		default:
			out = append(out, f)
		}
	}
	if !allFound {
		out = append([]domain.ChatFolder{all, archived}, out...)
	}
	return out
}
func (s *Store) BrowseChats(ctx context.Context, accountID, query, folderID, order string) ([]domain.Chat, error) {
	chats, err := s.Chats(ctx, accountID, query)
	if err != nil {
		return nil, err
	}
	folders, err := s.Folders(ctx, accountID)
	if err != nil {
		return nil, err
	}
	pins := map[string]int{}
	members := map[string]bool{}
	systemFolder := folderID == "" || folderID == "all" || folderID == "archived"
	if !systemFolder {
		for _, f := range folders {
			if f.ID == folderID {
				for _, id := range f.ChatIDs {
					members[id] = true
				}
				for i, id := range f.PinnedIDs {
					pins[id] = i + 1
				}
			}
		}
	}
	out := []domain.Chat{}
	for _, c := range chats {
		if folderID == "" || folderID == "all" && !c.Archived || folderID == "archived" && c.Archived || !systemFolder && members[c.ID] {
			if systemFolder {
				pins[c.ID] = c.PinnedOrder
			}
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if order == "recent" {
			pa, pb := pins[a.ID], pins[b.ID]
			if pa != pb && (pa > 0 || pb > 0) {
				return pa > 0 && (pb == 0 || pa < pb)
			}
			if !a.LastMessageAt.Equal(b.LastMessageAt) {
				return a.LastMessageAt.After(b.LastMessageAt)
			}
		}
		an, bn := strings.ToLower(a.VisibleName), strings.ToLower(b.VisibleName)
		if an != bn {
			return an < bn
		}
		return a.ID < b.ID
	})
	return out, nil
}
func filterChats(chats []domain.Chat, query string) []domain.Chat {
	out := []domain.Chat{}
	q := strings.ToLower(query)
	for _, c := range chats {
		if q == "" || strings.Contains(strings.ToLower(c.VisibleName), q) || strings.Contains(strings.ToLower(c.Username), q) {
			out = append(out, c)
		}
	}
	return out
}

func (s *Store) RemoveAccount(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO retired_namespaces(namespace) SELECT namespace FROM accounts WHERE id=?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE accounts SET removed=1,active=0,updated_at=? WHERE id=?`, formatTime(time.Now().UTC()), id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE accounts SET active=1 WHERE id=(SELECT id FROM accounts WHERE removed=0 ORDER BY display_name,id LIMIT 1) AND NOT EXISTS(SELECT 1 FROM accounts WHERE active=1 AND removed=0)`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RemovedNamespaces(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT namespace FROM accounts WHERE removed=1 UNION SELECT namespace FROM retired_namespaces`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var ns string
		if err = rows.Scan(&ns); err != nil {
			return nil, err
		}
		out = append(out, ns)
	}
	return out, rows.Err()
}

// Restore the stable account ID after authenticating the same Telegram identity.
// Use the newly authenticated namespace; all old credentials remain deleted.
func (s *Store) RestoreAccount(ctx context.Context, a domain.Account) (domain.Account, error) {
	if a.UserID == "" {
		return a, fmt.Errorf("登录结果缺少 Telegram 用户 ID")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return a, err
	}
	defer tx.Rollback()
	var oldID, oldName string
	err = tx.QueryRowContext(ctx, `SELECT id,display_name FROM accounts WHERE user_id=? AND removed=1 ORDER BY updated_at DESC,id LIMIT 1`, a.UserID).Scan(&oldID, &oldName)
	if err == sql.ErrNoRows {
		return a, nil
	}
	if err != nil {
		return a, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM accounts WHERE id=?`, a.ID); err != nil {
		return a, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE accounts SET namespace=?,username=?,removed=0,active=0,updated_at=? WHERE id=?`, a.Namespace, a.Username, formatTime(time.Now().UTC()), oldID); err != nil {
		return a, err
	}
	if err = tx.Commit(); err != nil {
		return a, err
	}
	a.ID = oldID
	a.DisplayName = oldName
	a.Removed = false
	return a, nil
}
