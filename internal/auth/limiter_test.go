package auth

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoginIPLimit(t *testing.T) {
	l := newLoginLimiter()
	now := time.Unix(1000, 0)
	for range loginIPLimit {
		if ok, retry := l.allow("192.0.2.1", now); !ok || retry != 0 {
			t.Fatal("denied attempt within IP limit")
		}
	}
	if ok, retry := l.allow("192.0.2.1", now.Add(time.Second)); ok || retry != 59*time.Second {
		t.Fatalf("expected IP denial for 59 seconds, got %v, %v", ok, retry)
	}
	if ok, _ := l.allow("192.0.2.2", now.Add(time.Second)); !ok {
		t.Fatal("one peer exhausted another peer's limit")
	}
	if ok, _ := l.allow("192.0.2.1", now.Add(loginWindow)); !ok {
		t.Fatal("IP limit did not reset at expiry")
	}
}

func TestLoginSlidingWindow(t *testing.T) {
	l := newLoginLimiter()
	now := time.Unix(1000, 0)
	for i := range loginIPLimit {
		if ok, _ := l.allow("peer", now.Add(time.Duration(i)*time.Second)); !ok {
			t.Fatal("denied attempt within IP limit")
		}
	}
	if ok, _ := l.allow("peer", now.Add(loginWindow-time.Nanosecond)); ok {
		t.Fatal("reset the limit before the oldest attempt expired")
	}
	if ok, _ := l.allow("peer", now.Add(loginWindow)); !ok {
		t.Fatal("did not expire the oldest attempt")
	}
	if ok, retry := l.allow("peer", now.Add(loginWindow)); ok || retry != time.Second {
		t.Fatalf("expected sliding-window denial for one second, got %v, %v", ok, retry)
	}
}

func TestLoginGlobalLimit(t *testing.T) {
	l := newLoginLimiter()
	now := time.Unix(1000, 0)
	for i := range loginGlobalLimit {
		if ok, _ := l.allow(fmt.Sprint(i), now); !ok {
			t.Fatal("denied attempt within global limit")
		}
	}
	if ok, retry := l.allow("another-peer", now.Add(time.Second)); ok || retry != 59*time.Second {
		t.Fatalf("expected global denial for 59 seconds, got %v, %v", ok, retry)
	}
	if _, exists := l.ips["another-peer"]; exists {
		t.Fatal("globally denied requests allocate peer state")
	}
	if ok, _ := l.allow("another-peer", now.Add(loginWindow)); !ok {
		t.Fatal("global limit did not expire")
	}
}

func TestLoginLimiterMemoryBoundAndCleanup(t *testing.T) {
	l := newLoginLimiter()
	now := time.Unix(1000, 0)
	for i := range loginMaxTrackedIP * 3 {
		if ok, _ := l.allow(fmt.Sprint(i), now.Add(time.Duration(i)*loginWindow)); !ok {
			t.Fatal("stale entries prevented a new peer from logging in")
		}
		if len(l.ips) > loginMaxTrackedIP {
			t.Fatal("peer map exceeded its memory bound")
		}
	}
}

func TestLoginLimiterDoesNotEvictActivePeers(t *testing.T) {
	l := newLoginLimiter()
	now := time.Unix(1000, 0)
	for i := range loginMaxTrackedIP {
		l.ips[fmt.Sprint(i)] = &attemptWindow{times: []time.Time{now}}
	}
	if ok, retry := l.allow("new-peer", now); ok || retry != loginWindow {
		t.Fatalf("full active peer map should fail closed, got %v, %v", ok, retry)
	}
	if len(l.ips) != loginMaxTrackedIP {
		t.Fatal("evicted an active peer")
	}
	if ok, _ := l.allow("new-peer", now.Add(loginWindow)); !ok {
		t.Fatal("did not reclaim expired peers")
	}
	if len(l.ips) != 1 {
		t.Fatalf("stale peers were not reclaimed: %d", len(l.ips))
	}
}

func TestExpiredPeerDuringGlobalDenial(t *testing.T) {
	l := newLoginLimiter()
	now := time.Unix(1000, 0)
	l.ips["expired"] = &attemptWindow{times: []time.Time{now.Add(-loginWindow)}}
	for range loginGlobalLimit {
		l.global.times = append(l.global.times, now)
	}
	if ok, _ := l.allow("expired", now); ok {
		t.Fatal("ignored global limit")
	}
	if _, exists := l.ips["expired"]; exists {
		t.Fatal("retained an empty peer window")
	}
}

func TestLoginLimiterConcurrent(t *testing.T) {
	for _, sharedIP := range []bool{false, true} {
		t.Run(fmt.Sprint(sharedIP), func(t *testing.T) {
			l := newLoginLimiter()
			now := time.Unix(1000, 0)
			var accepted atomic.Int64
			var wg sync.WaitGroup
			for i := range 500 {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					ip := fmt.Sprint(i)
					if sharedIP {
						ip = "shared"
					}
					if ok, _ := l.allow(ip, now); ok {
						accepted.Add(1)
					}
				}(i)
			}
			wg.Wait()
			want := int64(loginGlobalLimit)
			if sharedIP {
				want = loginIPLimit
			}
			if got := accepted.Load(); got != want {
				t.Fatalf("accepted %d concurrent requests, want %d", got, want)
			}
		})
	}
}

func TestRemoteIP(t *testing.T) {
	for _, test := range []struct{ remote, want string }{
		{"192.0.2.1:1234", "192.0.2.1"},
		{"192.0.2.1:5678", "192.0.2.1"},
		{"192.0.2.1", "192.0.2.1"},
		{"[2001:db8::1]:1234", "2001:db8::1"},
		{"[2001:0db8:0:0:0:0:0:1]:5678", "2001:db8::1"},
		{"[::ffff:192.0.2.1]:1234", "192.0.2.1"},
		{"[fe80::1%eth0]:1234", "fe80::1"},
		{"malformed-peer", "unknown"},
		{"", "unknown"},
	} {
		if got := remoteIP(test.remote); got != test.want {
			t.Fatalf("remoteIP(%q) = %q, want %q", test.remote, got, test.want)
		}
	}
}
