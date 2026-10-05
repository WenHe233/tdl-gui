package tgclient

import (
	"github.com/gotd/td/clock"
	"net/http"
	"testing"
	"time"
)

func TestHTTPSClockCorrectsSkewWithoutChangingSystemClock(t *testing.T) {
	server := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	for _, skew := range []time.Duration{-10 * time.Minute, 10 * time.Minute} {
		start := server.Add(skew - time.Second)
		offset, err := dateOffset(server.Format(http.TimeFormat), start, start.Add(2*time.Second))
		if err != nil || offset != -skew {
			t.Fatalf("offset=%v err=%v", offset, err)
		}
		c := adjustedClock{clock.System, offset}
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
