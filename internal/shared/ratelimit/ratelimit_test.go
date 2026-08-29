package ratelimit

import (
	"sync"
	"testing"
	"time"
)

// withClock returns a limiter whose time the test controls, so none of these
// have to sleep.
func withClock(limit int, window time.Duration) (*Limiter, func(time.Duration)) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	l := New(limit, window)
	l.now = func() time.Time { return now }
	return l, func(d time.Duration) { now = now.Add(d) }
}

func TestAttemptsAreAllowedUpToTheLimitAndThenRefused(t *testing.T) {
	l, _ := withClock(3, time.Minute)

	for i := 1; i <= 3; i++ {
		if !l.Allow("a") {
			t.Fatalf("attempt %d was refused; the limit is 3", i)
		}
	}
	if l.Allow("a") {
		t.Error("the fourth attempt was allowed")
	}
}

func TestKeysAreCountedIndependently(t *testing.T) {
	l, _ := withClock(1, time.Minute)

	if !l.Allow("a") {
		t.Fatal("first attempt for a was refused")
	}
	if !l.Allow("b") {
		t.Error("b was refused because a had used its own allowance")
	}
}

func TestTheWindowExpires(t *testing.T) {
	l, advance := withClock(1, time.Minute)

	l.Allow("a")
	if l.Allow("a") {
		t.Fatal("second attempt inside the window was allowed")
	}

	advance(time.Minute + time.Second)

	if !l.Allow("a") {
		t.Error("attempt after the window expired was still refused")
	}
}

// Someone who mistypes their password and then gets it right must not stay
// locked out by their own earlier attempts.
func TestResetClearsAKey(t *testing.T) {
	l, _ := withClock(2, time.Minute)

	l.Allow("a")
	l.Allow("a")
	if l.Allow("a") {
		t.Fatal("expected to be over the limit")
	}

	l.Reset("a")

	if !l.Allow("a") {
		t.Error("Reset did not clear the key")
	}
}

// A caller already over the limit must not be able to hold the window open for
// free — every attempt counts, including refused ones.
func TestRefusedAttemptsStillExtendNothingButAreCounted(t *testing.T) {
	l, advance := withClock(1, time.Minute)

	l.Allow("a")
	for i := 0; i < 5; i++ {
		if l.Allow("a") {
			t.Fatal("an attempt over the limit was allowed")
		}
	}

	// The window is fixed, so hammering it does not push the reset further out.
	advance(time.Minute + time.Second)
	if !l.Allow("a") {
		t.Error("the window did not expire on schedule after refused attempts")
	}
}

func TestRetryAfterReportsTimeRemainingOnlyWhenLimited(t *testing.T) {
	l, advance := withClock(1, time.Minute)

	l.Allow("a")
	if d := l.RetryAfter("a"); d != 0 {
		t.Errorf("RetryAfter = %v for a caller that is still within the limit, want 0", d)
	}

	l.Allow("a") // now over
	if d := l.RetryAfter("a"); d <= 0 || d > time.Minute {
		t.Errorf("RetryAfter = %v, want something inside the window", d)
	}

	advance(2 * time.Minute)
	if d := l.RetryAfter("a"); d != 0 {
		t.Errorf("RetryAfter = %v after the window passed, want 0", d)
	}
}

func TestExpiredEntriesAreSweptRatherThanAccumulating(t *testing.T) {
	l, advance := withClock(1, time.Minute)

	for i := 0; i < 100; i++ {
		l.Allow(string(rune('a'+i%26)) + string(rune('0'+i/26)))
	}
	if len(l.attempts) == 0 {
		t.Fatal("nothing was recorded")
	}

	// Past the window, then one more call to trigger the sweep.
	advance(2 * time.Minute)
	l.Allow("trigger")

	if len(l.attempts) != 1 {
		t.Errorf("expected only the triggering key to remain, got %d entries — the map leaks", len(l.attempts))
	}
}

func TestConcurrentUseDoesNotRace(t *testing.T) {
	l := New(1000, time.Minute)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				l.Allow("shared")
				l.RetryAfter("shared")
			}
		}()
	}
	wg.Wait()

	// 50 goroutines × 20 attempts, and every one must have been counted.
	if got := l.attempts["shared"].count; got != 1000 {
		t.Errorf("count = %d, want 1000 — attempts were lost to a race", got)
	}
}
