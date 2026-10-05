package authui

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gotd/td/tg"
	"github.com/local/tdl-gui/internal/domain"
	"github.com/local/tdl-gui/internal/store"
	"github.com/local/tdl-gui/internal/tdl"
)

func TestLoginIdentityCannotReplaceExistingAccount(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	original := domain.Account{ID: "a", Namespace: "old", UserID: "1", Active: true}
	if err = st.SaveAccount(ctx, original); err != nil {
		t.Fatal(err)
	}
	m := New(st, filepath.Join(root, "session.json"), func(Event) {}, tdl.New(nil, "", "", ""))
	candidate := original
	candidate.Namespace = "isolated"
	if err = m.complete(ctx, "login", candidate, &tg.User{ID: 2}); err == nil {
		t.Fatal("wrong identity accepted")
	}
	got, _ := st.Account(ctx, "a")
	if got.Namespace != "old" || got.UserID != "1" {
		t.Fatal("old credentials replaced", got)
	}
	if err = m.complete(ctx, "login", candidate, &tg.User{ID: 1}); err != nil {
		t.Fatal(err)
	}
	retired, _ := st.RemovedNamespaces(ctx)
	if len(retired) != 1 || retired[0] != "old" {
		t.Fatal("old session not retired", retired)
	}
	if err = st.RemoveAccount(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if err = m.complete(ctx, "late", candidate, &tg.User{ID: 1}); err == nil {
		t.Fatal("removed account resurrected")
	}
}

func TestCancelAccountWaitsForLoginAndClearsOnlyTemporaryNamespace(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	st, err := store.Open(filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_ = st.SaveAccount(ctx, domain.Account{ID: "a", Namespace: "old"})
	gate := tdl.New(nil, "", "", "")
	gate.Acquire()
	defer gate.Release()
	path := filepath.Join(root, "sessions.json")
	m := New(st, path, func(Event) {}, gate)
	if _, err = m.Start(ctx, StartOptions{AccountID: "a"}); err != nil {
		t.Fatal(err)
	}
	timeout, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err = m.CancelAccount(timeout, "a"); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	remaining := len(m.sessions)
	m.mu.Unlock()
	if remaining != 0 {
		t.Fatal("login remains active")
	}
	if err = os.WriteFile(path, []byte(`{"old":{"key":"b2xk"},"temporary":{"key":"bmV3"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = m.clearNamespace("temporary"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != `{"old":{"key":"b2xk"}}` {
		t.Fatal("unrelated credentials changed", err)
	}
}
