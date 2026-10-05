package vault

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSealAndOpen(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "session.json")
	protected := filepath.Join(dir, "session.dpapi")
	original := []byte(`{"default":{"session":"secret"}}`)
	if err := os.WriteFile(plain, original, 0o600); err != nil {
		t.Fatal(err)
	}
	v := New(plain, protected)
	if err := v.Seal(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(plain); !os.IsNotExist(err) {
		t.Fatalf("plaintext session still exists: %v", err)
	}
	encrypted, err := os.ReadFile(protected)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" && bytes.Equal(encrypted, original) {
		t.Fatal("protected data equals plaintext")
	}
	if err = v.Open(); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(plain)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored, original) {
		t.Fatalf("restored=%q", restored)
	}
}

func TestRemoveNamespaceUpdatesEncryptedCopy(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "sessions.json")
	protected := filepath.Join(dir, "sessions.dpapi")
	v := New(plain, protected)
	if err := os.WriteFile(plain, []byte(`{"a":{"session":"remove"},"b":{"session":"keep"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := v.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := v.Open(); err != nil {
		t.Fatal(err)
	}
	if err := v.RemoveNamespace("a"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(plain); err != nil {
		t.Fatal(err)
	}
	if err := v.Open(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(plain)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"b":{"session":"keep"}}` {
		t.Fatal("removed credentials resurrected")
	}
	if err := v.RemoveNamespace("b"); err != nil {
		t.Fatal(err)
	}
	if err := v.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := v.Open(); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(plain)
	if string(got) != "{}" {
		t.Fatal("last namespace remained")
	}
}
