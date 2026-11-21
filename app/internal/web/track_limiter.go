package web

import (
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	trackWindow      = time.Minute
	trackLimit       = 10
	maxTrackedClient = 2048
)

type trackAttempt struct {
	started time.Time
	count   int
}

type trackLimiter struct {
	mu       sync.Mutex
	attempts map[string]trackAttempt
}

func newTrackLimiter() *trackLimiter {
	return &trackLimiter{attempts: make(map[string]trackAttempt)}
}

func (l *trackLimiter) allow(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	attempt, ok := l.attempts[host]
	if ok && now.Sub(attempt.started) < trackWindow {
		if attempt.count >= trackLimit {
			return false
		}
		attempt.count++
		l.attempts[host] = attempt
		return true
	}

	if !ok && len(l.attempts) >= maxTrackedClient {
		for client, existing := range l.attempts {
			if now.Sub(existing.started) >= trackWindow {
				delete(l.attempts, client)
			}
		}
		if len(l.attempts) >= maxTrackedClient {
			return false
		}
	}
	l.attempts[host] = trackAttempt{started: now, count: 1}
	return true
}
