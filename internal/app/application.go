package app

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
	Paths    Paths
	Store    *store.Store
	Settings domain.Settings
	Engine   *engine.Manager
	Runner   *tdlrunner.Runner
	Accounts *account.Service
	Catalog  *catalog.Service
	Planner  *planner.Planner
	Preview  *preview.Service
	Jobs     *jobs.Service
	Vault    *vault.Vault
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
		return catalog.RefreshDirectory(ctx, st, runner, paths.TDLStorage, runner.Proxy(), settings.NTP, account)
	}
	a.Planner = planner.New(st)
	a.Preview = preview.New(st, paths.TDLStorage, paths.Cache, settings.Proxy, settings.NTP, settings.CacheMaxBytes, runner)
	a.Jobs = jobs.New(st, runner, paths.Staging, settings.Retries, settings.FileConcurrency, settings.TaskDelay, settings.MinFreeBytes, events)
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

func loadSettings(st *store.Store, p Paths) domain.Settings {
	downloads, _ := os.UserHomeDir()
	downloads = filepath.Join(downloads, "Downloads", "Telegram Media")
	s := domain.Settings{ChatOrder: "recent", MediaOrder: "newest", DataDir: p.Root, DownloadRoot: downloads, ReconnectTimeout: "5m", TaskDelay: "0s", FileConcurrency: 2, Retries: 3, MinFreeBytes: 1024 * 1024 * 1024, CacheMaxBytes: 500 * 1024 * 1024, LogLevel: "info"}
	assign := func(key string, to *string) {
		if v, e := st.GetSetting(context.Background(), key); e == nil && v != "" {
			*to = v
		}
	}
	assign("ui.chat.order", &s.ChatOrder)
	assign("ui.media.order", &s.MediaOrder)
	assign("download.root", &s.DownloadRoot)
	if proxy, err := st.GetSetting(context.Background(), "proxy"); err == nil {
		s.Proxy = proxy // An explicitly empty setting means direct connection.
	} else {
		s.Proxy = systemProxy()
	}
	assign("ntp", &s.NTP)
	assign("reconnect.timeout", &s.ReconnectTimeout)
	assign("task.delay", &s.TaskDelay)
	assign("log.level", &s.LogLevel)
	assign("engine.path", &s.EnginePath)
	assign("engine.version", &s.EngineVersion)
	integer := func(key string, to *int) {
		if v, e := st.GetSetting(context.Background(), key); e == nil {
			if n, e := strconv.Atoi(v); e == nil {
				*to = n
			}
		}
	}
	int64v := func(key string, to *int64) {
		if v, e := st.GetSetting(context.Background(), key); e == nil {
			if n, e := strconv.ParseInt(v, 10, 64); e == nil {
				*to = n
			}
		}
	}
	integer("file.concurrency", &s.FileConcurrency)
	integer("retries", &s.Retries)
	int64v("min.free.bytes", &s.MinFreeBytes)
	int64v("cache.max.bytes", &s.CacheMaxBytes)
	return s
}

func (a *Application) SetLoginProxy(ctx context.Context, proxy string) error {
	if err := a.Store.SetSetting(ctx, "proxy", proxy); err != nil {
		return err
	}
	a.Settings.Proxy = proxy
	a.Runner.SetProxy(proxy)
	a.Preview = preview.New(a.Store, a.Paths.TDLStorage, a.Paths.Cache, proxy, a.Settings.NTP, a.Settings.CacheMaxBytes, a.Runner)
	return nil
}

func (a *Application) SetConfig(ctx context.Context, key, value string) error {
	allowed := map[string]bool{"download.root": true, "proxy": true, "ntp": true, "reconnect.timeout": true, "task.delay": true, "file.concurrency": true, "retries": true, "min.free.bytes": true, "cache.max.bytes": true, "log.level": true}
	if key == "ui.chat.order" {
		if value != "recent" && value != "name" {
			return errors.New("无效聊天顺序")
		}
		a.Settings.ChatOrder = value
		return a.Store.SetSetting(ctx, key, value)
	}
	if key == "ui.media.order" {
		if value != "oldest" && value != "newest" {
			return errors.New("无效消息顺序")
		}
		a.Settings.MediaOrder = value
		return a.Store.SetSetting(ctx, key, value)
	}
	if strings.HasPrefix(key, "ui.folder.") {
		return a.Store.SetSetting(ctx, key, value)
	}
	if !allowed[key] {
		return errors.New("unknown config key")
	}
	return a.Store.SetSetting(ctx, key, value)
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
