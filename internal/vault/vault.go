package vault

import (
	"fmt"
	"os"
)

type Vault struct{ plain, protected string }

func New(plain, protected string) *Vault { return &Vault{plain: plain, protected: protected} }
func (v *Vault) Open() error {
	if _, err := os.Stat(v.plain); err == nil {
		return nil
	}
	encrypted, err := os.ReadFile(v.protected)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	data, err := unprotect(encrypted)
	if err != nil {
		return fmt.Errorf("unlock Telegram session: %w", err)
	}
	return os.WriteFile(v.plain, data, 0o600)
}
func (v *Vault) Seal() error {
	data, err := os.ReadFile(v.plain)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	encrypted, err := protect(data)
	if err != nil {
		return fmt.Errorf("protect Telegram session: %w", err)
	}
	tmp := v.protected + ".new"
	if err = os.WriteFile(tmp, encrypted, 0o600); err != nil {
		return err
	}
	_ = os.Remove(v.protected)
	if err = os.Rename(tmp, v.protected); err != nil {
		return err
	}
	return os.Remove(v.plain)
}
