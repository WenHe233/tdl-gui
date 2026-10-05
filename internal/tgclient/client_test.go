package tgclient

import (
	"github.com/gotd/td/clock"
	"net/http"
	"testing"
	"time"
)

type skewedClock struct {
	clock.Clock
	now time.Time
}

func (c skewedClock) Now() time.Time { return c.now }

func TestCorrectedClockRemainsAlignedWithSimulatedWrongSystemTime(t *testing.T) {
	server := time.Date(2026, 10, 5, 11, 0, 0, 0, time.UTC)
	for _, skew := range []time.Duration{-24 * time.Hour, -10 * time.Minute, 10 * time.Minute, 24 * time.Hour} {
		base := skewedClock{Clock: clock.System, now: server.Add(skew)}
		offset, err := dateOffset(server.Format(http.TimeFormat), base.Now(), base.Now())
		if err != nil {
			t.Fatal(err)
		}
		elapsed := time.Duration(0)
		corrected := adjustedClock{Clock: base, reference: base.Now().Add(offset), elapsed: func() time.Duration { return elapsed }}
		if !corrected.Now().Equal(server) {
			t.Fatalf("skew=%v corrected=%v", skew, corrected.Now())
		}
		if !base.Now().Equal(server.Add(skew)) {
			t.Fatal("host clock changed")
		}
		base.now = server.Add(-skew)
		corrected.Clock = base
		elapsed = 3 * time.Second
		if !corrected.Now().Equal(server.Add(elapsed)) {
			t.Fatal("later system clock adjustment broke synchronized time")
		}
	}
}

func TestHTTPSClockCorrectsSkewWithoutChangingSystemClock(t *testing.T) {
	server := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	for _, skew := range []time.Duration{-10 * time.Minute, 10 * time.Minute} {
		start := server.Add(skew - time.Second)
		offset, err := dateOffset(server.Format(http.TimeFormat), start, start.Add(2*time.Second))
		if err != nil || offset != -skew {
			t.Fatalf("offset=%v err=%v", offset, err)
		}
		anchor := time.Now()
		c := adjustedClock{Clock: clock.System, reference: anchor.Add(offset), elapsed: func() time.Duration { return time.Since(anchor) }}
		if delta := c.Now().Sub(time.Now()) - offset; delta > time.Second || delta < -time.Second {
			t.Fatalf("unexpected offset: %v", delta)
		}
	}
}
func TestHTTPSClockRejectsMissingDate(t *testing.T) {
	if _, err := dateOffset("", time.Now(), time.Now()); err == nil {
		t.Fatal("missing Date was accepted")
	}
}
