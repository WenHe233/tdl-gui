package app

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/local/tdl-gui/internal/account"
	"github.com/local/tdl-gui/internal/catalog"
	"github.com/local/tdl-gui/internal/domain"
	"github.com/local/tdl-gui/internal/engine"
	"github.com/local/tdl-gui/internal/jobs"
	"github.com/local/tdl-gui/internal/planner"
	"github.com/local/tdl-gui/internal/preview"
	"github.com/local/tdl-gui/internal/store"
	tdlrunner "github.com/local/tdl-gui/internal/tdl"
	"github.com/local/tdl-gui/internal/vault"
)

type Application struct {
	Paths      Paths
	Store      *store.Store
	Settings   domain.Settings
	settingsMu sync.RWMutex
	Engine     *engine.Manager
	Runner     *tdlrunner.Runner
	Accounts   *account.Service
	Catalog    *catalog.Service
	Planner    *planner.Planner
	Jobs       *jobs.Service
	Vault      *vault.Vault
}

func Open(dataDir string, events func(jobs.Event)) (*Application, error) {
	paths, err := ResolvePaths(dataDir)
	if err != nil {
		return nil, err
	}
	vaultStore := vault.New(paths.TDLStorage, paths.TDLProtected)
	if err := vaultStore.Open(); err != nil {
		return nil, err
	}
	st, err := store.Open(paths.Database)
	if err != nil {
		return nil, err
	}
	namespaces, err := st.RemovedNamespaces(context.Background())
	if err != nil {
		st.Close()
		return nil, err
	}
	for _, ns := range namespaces {
		if err = vaultStore.RemoveNamespace(ns); err != nil {
			st.Close()
			return nil, err
		}
	}
	settings := loadSettings(st, paths)
	em := engine.New(paths.Engines, st)
	runner := tdlrunner.New(func(ctx context.Context) (string, error) { v, err := em.Active(ctx); return v.Path, err }, paths.TDLStorage, settings.Proxy, settings.NTP)
	a := &Application{Paths: paths, Store: st, Settings: settings, Engine: em, Runner: runner, Vault: vaultStore}
	a.Accounts = account.New(st, runner)
	a.Catalog = catalog.New(st, runner, paths.Cache)
	a.Catalog.Directory = func(ctx context.Context, account domain.Account) ([]domain.Chat, error) {
		cfg := a.SettingsSnapshot()
		return catalog.RefreshDirectory(ctx, st, runner, paths.TDLStorage, cfg.Proxy, cfg.NTP, account, reconnectDuration(cfg))
	}
	a.Catalog.ScanSource = func(ctx context.Context, account domain.Account, options catalog.ScanOptions, cursor int64) ([]domain.Media, int64, error) {
		cfg := a.SettingsSnapshot()
		return catalog.ScanTelegram(ctx, st, runner, paths.TDLStorage, cfg.Proxy, cfg.NTP, account, options, cursor, reconnectDuration(cfg))
	}
	a.Planner = planner.New(st)
	a.Jobs = jobs.New(st, runner, paths.Staging, settings.Retries, settings.FileConcurrency, settings.TaskDelay, settings.MinFreeBytes, events)
	a.Jobs.SettingsSource = a.SettingsSnapshot
	runner.SetNetwork(settings.Proxy, settings.NTP, reconnectDuration(settings))
	return a, nil
}
func (a *Application) Close() error {
	sealErr := a.Vault.Seal()
	dbErr := a.Store.Close()
	if sealErr != nil {
		return sealErr
	}
	return dbErr
}

func (a *Application) SettingsSnapshot() domain.Settings {
	a.settingsMu.RLock()
	defer a.settingsMu.RUnlock()
	return a.Settings
}
func reconnectDuration(s domain.Settings) time.Duration {
	d, _ := time.ParseDuration(s.ReconnectTimeout)
	return d
}
func (a *Application) PreviewService() *preview.Service {
	s := a.SettingsSnapshot()
	return preview.New(a.Store, a.Paths.TDLStorage, a.Paths.Cache, s.Proxy, s.NTP, s.CacheMaxBytes, a.Runner, reconnectDuration(s))
}
func (a *Application) SetLoginProxy(ctx context.Context, proxy string) error {
	return a.SetConfig(ctx, "proxy", proxy)
}
func (a *Application) ClearCache(ctx context.Context) error {
	if err := os.RemoveAll(a.Paths.Cache); err != nil {
		return err
	}
	if err := os.MkdirAll(a.Paths.Cache, 0o700); err != nil {
		return err
	}
	return a.Store.ClearThumbnails(ctx)
}

func IsNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }
