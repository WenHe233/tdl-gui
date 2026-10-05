package authui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/local/tdl-gui/internal/domain"
	"github.com/local/tdl-gui/internal/tdl"
)

type testStore struct{}

func (testStore) Account(context.Context, string) (domain.Account, error) {
	return domain.Account{ID: "a", Namespace: "test"}, nil
}
func (testStore) SaveAccount(context.Context, domain.Account) error { return nil }
func (testStore) SetActiveAccount(context.Context, string) error    { return nil }

func TestLoginTimeoutWhileWaitingForEngine(t *testing.T) {
	gate := tdl.New(nil, "", "", "")
	gate.Acquire()
	defer gate.Release()
	events := make(chan Event, 8)
	m := New(testStore{}, "", func(e Event) { events <- e }, gate)
	m.startupTimeout = 10 * time.Millisecond
	_, err := m.Start(context.Background(), StartOptions{AccountID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case e := <-events:
			if e.Type == "login.error" {
				if !strings.Contains(e.Error, "超时") {
					t.Fatalf("unexpected error: %s", e.Error)
				}
				return
			}
		case <-time.After(time.Second):
			t.Fatal("blocked login never emitted timeout")
		}
	}
}

func TestCancelledLoginDoesNotBlockNextAttempt(t *testing.T) {
	gate := tdl.New(nil, "", "", "")
	gate.Acquire()
	m := New(testStore{}, "", func(Event) {}, gate)
	id, err := m.Start(context.Background(), StartOptions{AccountID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if !m.Cancel(id) {
		t.Fatal("cancel failed")
	}
	deadline := time.Now().Add(time.Second)
	for {
		m.mu.Lock()
		n := len(m.sessions)
		m.mu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancelled login still waiting for lock")
		}
		time.Sleep(time.Millisecond)
	}
	gate.Release()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := gate.AcquireContext(ctx); err != nil {
		t.Fatal(err)
	}
	gate.Release()
}

func TestLoginPromptDisarmsStartupTimeout(t *testing.T) {
	gate := tdl.New(nil, "", "", "")
	gate.Acquire()
	defer gate.Release()
	m := New(testStore{}, "", func(Event) {}, gate)
	m.startupTimeout = 50 * time.Millisecond
	id, err := m.Start(context.Background(), StartOptions{AccountID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	s := m.sessions[id]
	m.mu.Unlock()
	s.ready()
	time.Sleep(80 * time.Millisecond)
	m.mu.Lock()
	_, exists := m.sessions[id]
	m.mu.Unlock()
	if !exists {
		t.Fatal("startup timeout cancelled a flow already waiting for user input")
	}
	m.Cancel(id)
}
