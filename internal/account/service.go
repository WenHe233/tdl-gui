package account

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/local/tdl-gui/internal/domain"
	"github.com/local/tdl-gui/internal/idgen"
	"github.com/local/tdl-gui/internal/tdl"
)

type Store interface {
	SaveAccount(context.Context, domain.Account) error
	Accounts(context.Context) ([]domain.Account, error)
	Account(context.Context, string) (domain.Account, error)
	ActiveAccount(context.Context) (domain.Account, error)
	SetActiveAccount(context.Context, string) error
	DeleteAccount(context.Context, string) error
}
type Service struct {
	store  Store
	runner *tdl.Runner
}

func New(store Store, runner *tdl.Runner) *Service { return &Service{store: store, runner: runner} }

var namespaceRE = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func (s *Service) Add(ctx context.Context, name, namespace string) (domain.Account, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.Account{}, fmt.Errorf("display name is required")
	}
	if namespace == "" {
		namespace = namespaceRE.ReplaceAllString(strings.ToLower(name), "-")
	}
	if namespace == "" {
		namespace = idgen.New("account")
	}
	now := time.Now().UTC()
	a := domain.Account{ID: idgen.New("account"), Namespace: namespace, DisplayName: name, CreatedAt: now, UpdatedAt: now}
	list, _ := s.store.Accounts(ctx)
	a.Active = len(list) == 0
	if err := s.store.SaveAccount(ctx, a); err != nil {
		return domain.Account{}, err
	}
	return a, nil
}
func (s *Service) Login(ctx context.Context, id, method, desktopPath, passcode string) error {
	a, err := s.store.Account(ctx, id)
	if err != nil {
		return err
	}
	args := []string{"login"}
	switch method {
	case "qr", "code":
		args = append(args, "--type", method)
	case "desktop", "":
		if desktopPath != "" {
			args = append(args, "--desktop", desktopPath)
		}
		if passcode != "" {
			args = append(args, "--passcode", passcode)
		}
	default:
		return fmt.Errorf("unknown login method %q", method)
	}
	if err = s.runner.Interactive(ctx, a.Namespace, args...); err != nil {
		return err
	}
	return s.store.SetActiveAccount(ctx, id)
}
func (s *Service) Verify(ctx context.Context, id string) error {
	a, err := s.store.Account(ctx, id)
	if err != nil {
		return err
	}
	_, err = s.runner.Run(ctx, a.Namespace, "chat", "ls", "--output", "json")
	return err
}
