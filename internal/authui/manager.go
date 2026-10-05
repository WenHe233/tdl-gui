package authui

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/session"
	tdtdesktop "github.com/gotd/td/session/tdesktop"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/iyear/tdl/core/storage"
	"github.com/iyear/tdl/pkg/key"
	"github.com/iyear/tdl/pkg/kv"
	"github.com/local/tdl-gui/internal/domain"
	"github.com/local/tdl-gui/internal/idgen"
	tdlrunner "github.com/local/tdl-gui/internal/tdl"
	tclient "github.com/local/tdl-gui/internal/tgclient"
	"github.com/skip2/go-qrcode"
)

type Store interface {
	Account(context.Context, string) (domain.Account, error)
	SaveAccount(context.Context, domain.Account) error
	SetActiveAccount(context.Context, string) error
}
type Event struct {
	Type    string          `json:"type"`
	LoginID string          `json:"loginId"`
	Prompt  string          `json:"prompt,omitempty"`
	QRCode  string          `json:"qrCode,omitempty"`
	Choices []string        `json:"choices,omitempty"`
	Account *domain.Account `json:"account,omitempty"`
	Error   string          `json:"error,omitempty"`
}
type StartOptions struct {
	AccountID       string `json:"accountId"`
	Method          string `json:"method"`
	Phone           string `json:"phone,omitempty"`
	DesktopPath     string `json:"desktopPath,omitempty"`
	DesktopPasscode string `json:"desktopPasscode,omitempty"`
	DesktopUserID   string `json:"desktopUserId,omitempty"`
	Proxy           string `json:"proxy,omitempty"`
	NTP             string `json:"ntp,omitempty"`
}
type sessionState struct {
	committed bool
	accountID string
	done      chan struct{}
	cancel    context.CancelFunc
	inputs    chan input
	ready     func()
}
type input struct{ kind, value string }
type Manager struct {
	wg             sync.WaitGroup
	store          Store
	storagePath    string
	emit           func(Event)
	mu             sync.Mutex
	sessions       map[string]*sessionState
	gate           *tdlrunner.Runner
	startupTimeout time.Duration
}

func New(store Store, storagePath string, emit func(Event), gate *tdlrunner.Runner) *Manager {
	return &Manager{store: store, storagePath: storagePath, emit: emit, sessions: map[string]*sessionState{}, gate: gate, startupTimeout: 45 * time.Second}
}

func (m *Manager) Start(parent context.Context, o StartOptions) (string, error) {
	a, err := m.store.Account(parent, o.AccountID)
	if err != nil {
		return "", err
	}
	if a.Removed {
		return "", errors.New("账户登录信息已删除")
	}
	if o.Method == "" {
		o.Method = "qr"
	}
	id := idgen.New("login")
	ctx, cancelCause := context.WithCancelCause(parent)
	cancel := func() { cancelCause(context.Canceled) }
	timer := time.AfterFunc(m.startupTimeout, func() {
		cancelCause(errors.New("登录准备超时：未能在 45 秒内取得二维码或登录提示。请检查代理是否允许本程序连接 Telegram；TUN 不通时可填写 HTTP/SOCKS5 代理地址后重试，并检查系统时间。"))
	})
	st := &sessionState{accountID: a.ID, done: make(chan struct{}), cancel: cancel, inputs: make(chan input, 2), ready: func() { timer.Stop() }}
	m.mu.Lock()
	m.sessions[id] = st
	m.mu.Unlock()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer close(st.done)
		defer timer.Stop()
		err := m.run(ctx, id, a, o, st)
		if ctx.Err() != nil {
			err = context.Cause(ctx)
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			m.emit(Event{Type: "login.error", LoginID: id, Error: err.Error()})
		}
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
		cancel()
	}()
	return id, nil
}
func (m *Manager) Submit(id, kind, value string) error {
	m.mu.Lock()
	s := m.sessions[id]
	m.mu.Unlock()
	if s == nil {
		return fmt.Errorf("login session %s not found", id)
	}
	select {
	case s.inputs <- input{kind: kind, value: value}:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("login input timed out")
	}
}
func (m *Manager) Cancel(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sessions[id]; s != nil {
		s.cancel()
		return true
	}
	return false
}

func (m *Manager) run(ctx context.Context, loginID string, a domain.Account, o StartOptions, st *sessionState) (result error) {
	m.emit(Event{Type: "login.status", LoginID: loginID, Prompt: "正在等待登录连接…"})
	if err := m.gate.AcquireContext(ctx); err != nil {
		return err
	}
	defer m.gate.Release()
	fresh, err := m.store.Account(ctx, a.ID)
	if err != nil {
		return err
	}
	if fresh.Removed || ctx.Err() != nil {
		return context.Canceled
	}
	// Authenticate into an isolated namespace. A wrong identity must not replace
	// the credentials used by the account's existing downloads and index.
	originalNamespace := a.Namespace
	a.Namespace = "auth_" + loginID
	defer func() {
		// Client.Run also returns nil when a connection is cancelled. Only a
		// committed account proves that the new namespace may replace the old one.
		if err := m.cleanupLoginNamespace(originalNamespace, a.Namespace, st.committed); err != nil {
			result = errors.Join(result, err)
		}
	}()
	prompt := "正在直连 Telegram…"
	if o.Proxy != "" {
		prompt = "正在通过代理连接 Telegram…"
	}
	m.emit(Event{Type: "login.status", LoginID: loginID, Prompt: prompt})
	switch o.Method {
	case "desktop":
		return m.importDesktop(ctx, loginID, a, o, st)
	case "qr", "code":
	default:
		return fmt.Errorf("unsupported login method %q", o.Method)
	}
	storageFile, err := kv.New(kv.DriverFile, map[string]any{"path": m.storagePath})
	if err != nil {
		return err
	}
	defer storageFile.Close()
	kvd, err := storageFile.Open(a.Namespace)
	if err != nil {
		return err
	}
	if err = kvd.Set(ctx, key.App(), []byte(tclient.AppDesktop)); err != nil {
		return err
	}
	if o.Method == "qr" {
		return m.qr(ctx, loginID, a, o, kvd, st)
	}
	return m.code(ctx, loginID, a, o, kvd, st)
}
func (m *Manager) qr(ctx context.Context, loginID string, a domain.Account, o StartOptions, kvd storage.Storage, st *sessionState) error {
	d := tg.NewUpdateDispatcher()
	c, err := tclient.New(ctx, tclient.Options{KV: kvd, Proxy: o.Proxy, NTP: o.NTP, ReconnectTimeout: 5 * time.Minute, UpdateHandler: d}, true)
	if err != nil {
		return err
	}
	return c.Run(ctx, func(ctx context.Context) error {
		m.emit(Event{Type: "login.status", LoginID: loginID, Prompt: "已连接 Telegram，正在请求二维码…"})
		// Auth handles migration after a QR code is accepted, but Telegram can also
		// request migration while exporting the first token. Retry the whole flow on
		// the requested DC so that this implementation detail never leaks into the UI.
		for migrations := 0; ; {
			_, err = c.QR().Auth(ctx, qrlogin.OnLoginToken(d), func(_ context.Context, token qrlogin.Token) error {
				png, e := qrcode.Encode(token.URL(), qrcode.Medium, 280)
				if e != nil {
					return e
				}
				st.ready()
				m.emit(Event{Type: "login.qr", LoginID: loginID, Prompt: "请用 Telegram 手机客户端扫码", QRCode: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)})
				return nil
			})
			if err == nil || tgerr.Is(err, "SESSION_PASSWORD_NEEDED") {
				break
			}
			var migration *qrlogin.MigrationNeededError
			if !errors.As(err, &migration) || migration.MigrateTo == nil || migrations >= 3 {
				break
			}
			migrations++
			m.emit(Event{Type: "login.status", LoginID: loginID, Prompt: fmt.Sprintf("正在切换到 Telegram 数据中心 %d…", migration.MigrateTo.DCID)})
			if err = c.MigrateTo(ctx, migration.MigrateTo.DCID); err != nil {
				return fmt.Errorf("切换 Telegram 数据中心失败: %w", err)
			}
		}
		if err != nil {
			// A login-token update and its callback can race. If the session was
			// persisted successfully, finish login instead of showing a stale error.
			if user, selfErr := c.Self(ctx); selfErr == nil {
				return m.complete(ctx, loginID, a, user)
			}
			if !tgerr.Is(err, "SESSION_PASSWORD_NEEDED") {
				return err
			}
			st.ready()
			m.emit(Event{Type: "login.passwordRequired", LoginID: loginID, Prompt: "请输入 Telegram 两步验证密码"})
			pwd, e := waitInput(ctx, st, "password")
			if e != nil {
				return e
			}
			if _, e = c.Auth().Password(ctx, pwd); e != nil {
				return e
			}
		}
		user, e := c.Self(ctx)
		if e != nil {
			return e
		}
		return m.complete(ctx, loginID, a, user)
	})
}
func (m *Manager) code(ctx context.Context, loginID string, a domain.Account, o StartOptions, kvd storage.Storage, st *sessionState) error {
	if strings.TrimSpace(o.Phone) == "" {
		return errors.New("phone is required for code login")
	}
	c, err := tclient.New(ctx, tclient.Options{KV: kvd, Proxy: o.Proxy, NTP: o.NTP, ReconnectTimeout: 5 * time.Minute}, true)
	if err != nil {
		return err
	}
	ui := &channelAuth{phone: strings.TrimSpace(o.Phone), loginID: loginID, emit: m.emit, state: st}
	return c.Run(ctx, func(ctx context.Context) error {
		if err = c.Ping(ctx); err != nil {
			return err
		}
		if err = c.Auth().IfNecessary(ctx, auth.NewFlow(ui, auth.SendCodeOptions{})); err != nil {
			return err
		}
		user, e := c.Self(ctx)
		if e != nil {
			return e
		}
		return m.complete(ctx, loginID, a, user)
	})
}
func (m *Manager) complete(ctx context.Context, loginID string, a domain.Account, user *tg.User) error {
	fresh, err := m.store.Account(ctx, a.ID)
	if err != nil {
		return err
	}
	if fresh.Removed || ctx.Err() != nil {
		return context.Canceled
	}
	if fresh.UserID != "" && fresh.UserID != strconv.FormatInt(user.ID, 10) {
		return errors.New("登录账户与原账户不一致，请添加新账户")
	}
	a.UserID = strconv.FormatInt(user.ID, 10)
	a.Username = user.Username
	if a.DisplayName == "" {
		a.DisplayName = strings.TrimSpace(user.FirstName + " " + user.LastName)
	}
	if restorer, ok := m.store.(interface {
		RestoreAccount(context.Context, domain.Account) (domain.Account, error)
	}); ok {
		var err error
		a, err = restorer.RestoreAccount(ctx, a)
		if err != nil {
			return err
		}
	}
	if err := m.store.SaveAccount(ctx, a); err != nil {
		return err
	}
	m.mu.Lock()
	if state := m.sessions[loginID]; state != nil {
		state.committed = true
	}
	m.mu.Unlock()
	if err := m.store.SetActiveAccount(ctx, a.ID); err != nil {
		return err
	}
	a.Active = true
	m.emit(Event{Type: "login.completed", LoginID: loginID, Account: &a})
	return nil
}

type channelAuth struct {
	phone, loginID string
	emit           func(Event)
	state          *sessionState
}

func (a *channelAuth) Phone(context.Context) (string, error) { return a.phone, nil }
func (a *channelAuth) Code(ctx context.Context, _ *tg.AuthSentCode) (string, error) {
	a.state.ready()
	a.emit(Event{Type: "login.codeRequired", LoginID: a.loginID, Prompt: "请输入 Telegram 发送的验证码"})
	return waitInput(ctx, a.state, "code")
}
func (a *channelAuth) Password(ctx context.Context) (string, error) {
	a.state.ready()
	a.emit(Event{Type: "login.passwordRequired", LoginID: a.loginID, Prompt: "请输入 Telegram 两步验证密码"})
	return waitInput(ctx, a.state, "password")
}
func (a *channelAuth) SignUp(context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("不支持通过本程序注册 Telegram 账户")
}
func (a *channelAuth) AcceptTermsOfService(_ context.Context, tos tg.HelpTermsOfService) error {
	return &auth.SignUpRequired{TermsOfService: tos}
}
func waitInput(ctx context.Context, st *sessionState, kind string) (string, error) {
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case in := <-st.inputs:
			if in.kind == kind {
				return strings.TrimSpace(in.value), nil
			}
		}
	}
}

func (m *Manager) importDesktop(ctx context.Context, loginID string, a domain.Account, o StartOptions, st *sessionState) error {
	path := o.DesktopPath
	if path == "" {
		path = defaultDesktopPath()
	}
	if filepath.Base(path) != "tdata" {
		path = filepath.Join(path, "tdata")
	}
	accounts, err := tdtdesktop.Read(path, []byte(o.DesktopPasscode))
	if err != nil {
		return fmt.Errorf("read Telegram Desktop session: %w", err)
	}
	if len(accounts) == 0 {
		return errors.New("Telegram Desktop 中没有可导入的账户")
	}
	chosen := o.DesktopUserID
	if chosen == "" && len(accounts) > 1 {
		st.ready()
		choices := make([]string, 0, len(accounts))
		for _, x := range accounts {
			choices = append(choices, strconv.FormatUint(x.Authorization.UserID, 10))
		}
		m.emit(Event{Type: "login.desktopAccountRequired", LoginID: loginID, Prompt: "选择要导入的 Telegram Desktop 账户", Choices: choices})
		chosen, err = waitInput(ctx, st, "desktopAccount")
		if err != nil {
			return err
		}
	}
	var selected *tdtdesktop.Account
	for i := range accounts {
		id := strconv.FormatUint(accounts[i].Authorization.UserID, 10)
		if chosen == "" || chosen == id {
			selected = &accounts[i]
			break
		}
	}
	if selected == nil {
		return fmt.Errorf("Telegram Desktop account %s not found", chosen)
	}
	data, err := session.TDesktopSession(*selected)
	if err != nil {
		return err
	}
	storageFile, err := kv.New(kv.DriverFile, map[string]any{"path": m.storagePath})
	if err != nil {
		return err
	}
	defer storageFile.Close()
	kvd, err := storageFile.Open(a.Namespace)
	if err != nil {
		return err
	}
	if err = (&session.Loader{Storage: storage.NewSession(kvd, true)}).Save(ctx, data); err != nil {
		return err
	}
	if err = kvd.Set(ctx, key.App(), []byte(tclient.AppDesktop)); err != nil {
		return err
	}
	client, err := tclient.New(ctx, tclient.Options{KV: kvd, Proxy: o.Proxy, NTP: o.NTP, ReconnectTimeout: time.Minute}, false)
	if err != nil {
		return err
	}
	return client.Run(ctx, func(ctx context.Context) error {
		user, err := client.Self(ctx)
		if err != nil {
			return err
		}
		return m.complete(ctx, loginID, a, user)
	})
}
func defaultDesktopPath() string {
	if appData := os.Getenv("APPDATA"); appData != "" {
		return filepath.Join(appData, "Telegram Desktop")
	}
	return "Telegram Desktop"
}

func (m *Manager) Shutdown() {
	m.mu.Lock()
	for _, s := range m.sessions {
		s.cancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
}

func (m *Manager) CancelAccount(ctx context.Context, id string) error {
	m.mu.Lock()
	var done []chan struct{}
	for _, s := range m.sessions {
		if s.accountID == id {
			s.cancel()
			done = append(done, s.done)
		}
	}
	m.mu.Unlock()
	for _, ch := range done {
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (m *Manager) clearNamespace(namespace string) error {
	if _, err := os.Stat(m.storagePath); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	file, err := kv.New(kv.DriverFile, map[string]any{"path": m.storagePath})
	if err != nil {
		return err
	}
	defer file.Close()
	meta, err := file.MigrateTo()
	if err != nil {
		return err
	}
	delete(meta, namespace)
	return file.MigrateFrom(meta)
}

func (m *Manager) cleanupLoginNamespace(original, candidate string, committed bool) error {
	if committed {
		return m.clearNamespace(original)
	}
	return m.clearNamespace(candidate)
}
