package auth

import (
	"strings"
	"sync"
	"time"
)

// Limiter slows down password guessing. After FreeAttempts wrong passwords
// for one username from one address, further tries must wait: a minute,
// then twice as long each time, up to MaxWait. A right password resets the
// count.
type Limiter struct {
	FreeAttempts int
	BaseWait     time.Duration
	MaxWait      time.Duration
	// Now is replaceable so tests can move time.
	Now func() time.Time

	mu      sync.Mutex
	entries map[string]*limiterEntry
	ops     int
}

type limiterEntry struct {
	failures    int
	lockedUntil time.Time
	lastSeen    time.Time
}

// NewLimiter returns a limiter with the brief's schedule.
func NewLimiter() *Limiter {
	return &Limiter{FreeAttempts: 5, BaseWait: time.Minute, MaxWait: 15 * time.Minute, Now: time.Now, entries: map[string]*limiterEntry{}}
}

// Key names the bucket for a username and a client address.
func Key(username, addr string) string {
	return strings.ToLower(strings.TrimSpace(username)) + "@" + strings.TrimSpace(addr)
}

// Check reports how long the caller must still wait before trying again;
// zero means the attempt may proceed.
func (l *Limiter) Check(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep()
	e, ok := l.entries[key]
	if !ok {
		return 0
	}
	now := l.Now()
	if e.lockedUntil.After(now) {
		return e.lockedUntil.Sub(now)
	}
	return 0
}

// Fail records a wrong password and returns the wait now imposed, zero
// while free attempts remain.
func (l *Limiter) Fail(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep()
	now := l.Now()
	e, ok := l.entries[key]
	if !ok {
		e = &limiterEntry{}
		l.entries[key] = e
	}
	e.failures++
	e.lastSeen = now
	over := e.failures - l.FreeAttempts
	if over < 0 {
		return 0
	}
	wait := l.BaseWait
	for i := 0; i < over && wait < l.MaxWait; i++ {
		wait *= 2
	}
	if wait > l.MaxWait {
		wait = l.MaxWait
	}
	e.lockedUntil = now.Add(wait)
	return wait
}

// Reset forgets a bucket after a successful login.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

// sweep drops buckets idle for an hour, every few hundred operations, so
// the map cannot grow without bound. Callers hold the lock.
func (l *Limiter) sweep() {
	l.ops++
	if l.ops%256 != 0 {
		return
	}
	cutoff := l.Now().Add(-time.Hour)
	for k, e := range l.entries {
		if e.lastSeen.Before(cutoff) && e.lockedUntil.Before(cutoff) {
			delete(l.entries, k)
		}
	}
}
