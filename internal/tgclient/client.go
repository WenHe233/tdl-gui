// Package tgclient keeps tdl's session and API identity while correcting the
// application clock over HTTPS, including on networks that block NTP/UDP.
package tgclient

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/gotd/td/clock"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/dcs"
	"github.com/iyear/tdl/core/logctx"
	"github.com/iyear/tdl/core/storage"
	coreclient "github.com/iyear/tdl/core/tclient"
	"github.com/iyear/tdl/core/util/netutil"
	"github.com/iyear/tdl/core/util/tutil"
	upstream "github.com/iyear/tdl/pkg/tclient"
)

type Options = upstream.Options

const AppDesktop = upstream.AppDesktop

type adjustedClock struct {
	clock.Clock
	offset time.Duration
}

func (c adjustedClock) Now() time.Time { return c.Clock.Now().Add(c.offset) }

var clockCache = struct {
	sync.Mutex
	entries map[string]struct {
		offset  time.Duration
		expires time.Time
	}
}{entries: make(map[string]struct {
	offset  time.Duration
	expires time.Time
})}

func dateOffset(date string, start, end time.Time) (time.Duration, error) {
	server, err := http.ParseTime(date)
	if err != nil {
		return 0, fmt.Errorf("invalid HTTPS Date header: %w", err)
	}
	return server.Sub(start.Add(end.Sub(start) / 2)), nil
}

func networkClock(ctx context.Context, proxy string, dial dcs.DialFunc) (clock.Clock, error) {
	clockCache.Lock()
	entry, ok := clockCache.entries[proxy]
	clockCache.Unlock()
	if ok && time.Now().Before(entry.expires) {
		return adjustedClock{clock.System, entry.offset}, nil
	}
	transport := &http.Transport{DialContext: dial, TLSHandshakeTimeout: 8 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, "https://telegram.org/", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cache-Control", "no-cache")
	start := time.Now()
	res, err := client.Do(req)
	end := time.Now()
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	offset, err := dateOffset(res.Header.Get("Date"), start, end)
	if err != nil {
		return nil, err
	}
	clockCache.Lock()
	clockCache.entries[proxy] = struct {
		offset  time.Duration
		expires time.Time
	}{offset, time.Now().Add(5 * time.Minute)}
	clockCache.Unlock()
	return adjustedClock{clock.System, offset}, nil
}

func New(ctx context.Context, o Options, login bool, middlewares ...telegram.Middleware) (*telegram.Client, error) {
	if o.NTP != "" {
		return upstream.New(ctx, o, login, middlewares...)
	}
	app, err := upstream.GetApp(o.KV)
	if err != nil {
		return nil, err
	}
	var dial dcs.DialFunc = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
	if o.Proxy != "" {
		p, err := netutil.NewProxy(o.Proxy)
		if err != nil {
			return nil, err
		}
		dial = p.DialContext
	}
	corrected, err := networkClock(ctx, o.Proxy, dial)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Telegram may be reachable even when its website is blocked. The login
		// startup deadline still bounds MTProto retries when the local clock is wrong.
		logctx.From(ctx).Warn("HTTPS clock sync unavailable; using local clock")
		corrected = clock.System
	}
	return telegram.NewClient(app.AppID, app.AppHash, telegram.Options{
		Resolver: dcs.Plain(dcs.PlainOptions{Dial: dial}),
		ReconnectionBackoff: func() backoff.BackOff {
			b := backoff.NewExponentialBackOff()
			b.Multiplier = 1.1
			b.MaxElapsedTime = o.ReconnectTimeout
			b.MaxInterval = 10 * time.Second
			return b
		},
		DC: coreclient.DC, DCList: coreclient.DCList, PublicKeys: coreclient.PublicKeys,
		UpdateHandler: o.UpdateHandler, Device: tutil.Device, SessionStorage: storage.NewSession(o.KV, login),
		RetryInterval: 5 * time.Second, MaxRetries: 5, DialTimeout: 10 * time.Second,
		Middlewares: append(coreclient.NewDefaultMiddlewares(ctx, o.ReconnectTimeout), middlewares...),
		Clock:       corrected, Logger: logctx.From(ctx).Named("td"),
	}), nil
}
