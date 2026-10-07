package app

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/local/tdl-gui/internal/domain"
	"github.com/local/tdl-gui/internal/store"
)

var configKeys = []string{"download.root", "proxy", "ntp", "reconnect.timeout", "task.delay", "file.threads", "file.concurrency", "pool.size", "retries", "min.free.bytes", "cache.max.bytes", "log.level", "ui.chat.order", "ui.media.order"}

func loadSettings(st *store.Store, p Paths) domain.Settings {
	home, _ := os.UserHomeDir()
	s := domain.Settings{ChatOrder: "recent", MediaOrder: "newest", DataDir: p.Root, DownloadRoot: filepath.Join(home, "Downloads", "Telegram Media"), Proxy: systemProxy(), ReconnectTimeout: "5m", TaskDelay: "0s", FileThreads: 8, FileConcurrency: 4, PoolSize: 8, Retries: 3, MinFreeBytes: 1024 * 1024 * 1024, CacheMaxBytes: 500 * 1024 * 1024, LogLevel: "info"}
	for _, key := range configKeys {
		if v, err := st.GetSetting(context.Background(), key); err == nil {
			_ = applySetting(&s, key, v)
		}
	}
	s.EnginePath, _ = st.GetSetting(context.Background(), "engine.path")
	s.EngineVersion, _ = st.GetSetting(context.Background(), "engine.version")
	return s
}

func applySetting(s *domain.Settings, key, value string) error {
	invalid := func(message string) error { return fmt.Errorf("%s: %s", key, message) }
	ints := map[string]*int{"file.threads": &s.FileThreads, "file.concurrency": &s.FileConcurrency, "pool.size": &s.PoolSize, "retries": &s.Retries}
	if target, ok := ints[key]; ok {
		n, err := strconv.Atoi(value)
		minimum := 0
		if key == "file.threads" || key == "file.concurrency" {
			minimum = 1
		}
		if err != nil || n < minimum || int64(n) > 2147483647 {
			return invalid(fmt.Sprintf("请输入 %d 到 2147483647 的整数", minimum))
		}
		*target = n
		return nil
	}
	capacities := map[string]*int64{"min.free.bytes": &s.MinFreeBytes, "cache.max.bytes": &s.CacheMaxBytes}
	if target, ok := capacities[key]; ok {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 0 || n > 9007199254740991 {
			return invalid("请输入非负整数容量")
		}
		*target = n
		return nil
	}
	switch key {
	case "download.root":
		if strings.TrimSpace(value) == "" || strings.ContainsRune(value, 0) || !filepath.IsAbs(value) {
			return invalid("请选择绝对路径")
		}
		s.DownloadRoot = filepath.Clean(value)
	case "proxy":
		if value != "" {
			u, err := url.Parse(value)
			if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5") {
				return invalid("请输入 HTTP、HTTPS 或 SOCKS5 代理地址，留空为直连")
			}
		}
		s.Proxy = value
	case "ntp":
		if strings.ContainsAny(value, " \t\r\n") || strings.Contains(value, "://") {
			return invalid("请输入 NTP 主机名，或留空")
		}
		s.NTP = value
	case "task.delay", "reconnect.timeout":
		d, err := time.ParseDuration(value)
		if err != nil || d < 0 {
			return invalid("请输入非负时长，例如 0s、30s、5m")
		}
		if key == "task.delay" {
			s.TaskDelay = value
		} else {
			s.ReconnectTimeout = value
		}
	case "log.level":
		if value != "debug" && value != "info" && value != "warn" && value != "error" {
			return invalid("无效日志级别")
		}
		s.LogLevel = value
	case "ui.chat.order":
		if value != "recent" && value != "name" {
			return invalid("无效聊天顺序")
		}
		s.ChatOrder = value
	case "ui.media.order":
		if value != "oldest" && value != "newest" {
			return invalid("无效消息顺序")
		}
		s.MediaOrder = value
	default:
		if !strings.HasPrefix(key, "ui.folder.") {
			return invalid("未知设置项")
		}
	}
	return nil
}

func (a *Application) UpdateConfig(ctx context.Context, values map[string]string) (domain.Settings, error) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	next := a.Settings
	for key, value := range values {
		if err := applySetting(&next, key, value); err != nil {
			return domain.Settings{}, err
		}
	}
	if err := a.Store.SetSettings(ctx, values); err != nil {
		return domain.Settings{}, err
	}
	a.Settings = next
	if a.Runner != nil {
		a.Runner.SetNetwork(next.Proxy, next.NTP, reconnectDuration(next))
	}
	return next, nil
}
func (a *Application) SetConfig(ctx context.Context, key, value string) error {
	_, err := a.UpdateConfig(ctx, map[string]string{key: value})
	return err
}
