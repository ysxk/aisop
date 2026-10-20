package auth

import (
	"net"
	"net/netip"
	"sync"
	"time"
)

const (
	loginWindow       = time.Minute
	loginIPLimit      = 10
	loginGlobalLimit  = 100
	loginMaxTrackedIP = 1024
)

type attemptWindow struct {
	times []time.Time
}

func (w *attemptWindow) prune(now time.Time) {
	cutoff := now.Add(-loginWindow)
	n := 0
	for n < len(w.times) && !w.times[n].After(cutoff) {
		n++
	}
	copy(w.times, w.times[n:])
	w.times = w.times[:len(w.times)-n]
}

func (w *attemptWindow) retryAfter(now time.Time) time.Duration {
	return w.times[0].Add(loginWindow).Sub(now)
}

type loginLimiter struct {
	mu     sync.Mutex
	ips    map[string]*attemptWindow
	global attemptWindow
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{ips: make(map[string]*attemptWindow)}
}

func (l *loginLimiter) allow(ip string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.global.prune(now)
	peer := l.ips[ip]
	if peer != nil {
		peer.prune(now)
		if len(peer.times) >= loginIPLimit {
			return false, peer.retryAfter(now)
		}
		if len(peer.times) == 0 {
			delete(l.ips, ip)
			peer = nil
		}
	}
	if len(l.global.times) >= loginGlobalLimit {
		return false, l.global.retryAfter(now)
	}
	if peer == nil {
		if len(l.ips) >= loginMaxTrackedIP {
			retry := loginWindow
			for key, window := range l.ips {
				lastExpiry := window.times[len(window.times)-1].Add(loginWindow)
				if !lastExpiry.After(now) {
					delete(l.ips, key)
				} else if remaining := lastExpiry.Sub(now); remaining < retry {
					retry = remaining
				}
			}
			// Do not evict active peers: rotating addresses must not reset their limits.
			if len(l.ips) >= loginMaxTrackedIP {
				return false, retry
			}
		}
		peer = &attemptWindow{times: make([]time.Time, 0, loginIPLimit)}
		l.ips[ip] = peer
	}
	peer.times = append(peer.times, now)
	l.global.times = append(l.global.times, now)
	return true, 0
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	return ip.Unmap().WithZone("").String()
}
