// Package ratelimit provides a small in-memory limiter for the endpoints where
// unlimited attempts are the vulnerability — sign-in above all.
//
// In-memory means per-process: the counters are not shared between replicas and
// they reset on deploy. That is a real weakening, and it is accepted here on
// purpose. The alternative is a Redis dependency for one feature, and even a
// per-process limit turns online password guessing from "as fast as the network
// allows" into something too slow to be worth attempting. If the app ever runs
// enough replicas for that to stop being true, this is the piece to move into
// Postgres — the interface below does not have to change.
package ratelimit

import (
	"sync"
	"time"
)

type attempt struct {
	count int
	// resets is when the window ends. A fixed window rather than a sliding one:
	// the worst case is a caller getting 2×limit across a window boundary,
	// which for sign-in attempts is not worth the extra bookkeeping.
	resets time.Time
}

type Limiter struct {
	mu       sync.Mutex
	attempts map[string]*attempt

	limit  int
	window time.Duration

	// now is injectable so the tests can advance time without sleeping.
	now func() time.Time

	lastSweep time.Time
}

func New(limit int, window time.Duration) *Limiter {
	return &Limiter{
		attempts: make(map[string]*attempt),
		limit:    limit,
		window:   window,
		now:      time.Now,
	}
}

// Allow records an attempt against key and reports whether it may proceed.
//
// It counts every call, including the ones it rejects. Not counting rejected
// attempts would let a caller who is already over the limit keep the window
// alive for free while never being told to stop.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweepLocked(now)

	a, ok := l.attempts[key]
	if !ok || now.After(a.resets) {
		l.attempts[key] = &attempt{count: 1, resets: now.Add(l.window)}
		return true
	}

	a.count++
	return a.count <= l.limit
}

// Reset clears a key. Sign-in calls this on success, so that someone who
// mistyped their password several times and then got it right is not left
// locked out by their own earlier attempts.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// RetryAfter reports how long until key is allowed again, or zero.
func (l *Limiter) RetryAfter(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	a, ok := l.attempts[key]
	if !ok || a.count <= l.limit {
		return 0
	}
	if d := a.resets.Sub(l.now()); d > 0 {
		return d
	}
	return 0
}

// sweepLocked drops expired entries so the map does not grow without bound —
// otherwise every distinct address ever tried stays in memory forever, which is
// a slow leak an attacker controls the size of.
//
// It runs at most once per window rather than on every call, because the whole
// point of this package is that it is cheap on the hot path.
func (l *Limiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < l.window {
		return
	}
	l.lastSweep = now

	for key, a := range l.attempts {
		if now.After(a.resets) {
			delete(l.attempts, key)
		}
	}
}
