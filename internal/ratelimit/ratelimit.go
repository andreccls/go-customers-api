// Package ratelimit is a small in-memory token-bucket limiter keyed by string.
//
// ponytail: state lives in the process, so with N replicas the effective limit is
// N times higher (and resets on restart). Move to Redis or the gateway if that matters.
package ratelimit

import (
	"sync"
	"time"
)

type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter allows `burst` requests at once and refills at `perMinute` tokens/minute.
type Limiter struct {
	mu        sync.Mutex
	rate      float64 // tokens per second
	burst     float64
	now       func() time.Time
	buckets   map[string]*bucket
	lastSweep time.Time
}

// New returns a Limiter. now is injectable for tests (nil = time.Now).
func New(perMinute int, now func() time.Time) *Limiter {
	if now == nil {
		now = time.Now
	}
	return &Limiter{
		rate:    float64(perMinute) / 60,
		burst:   float64(perMinute),
		now:     now,
		buckets: map[string]*bucket{},
	}
}

// Allow consumes one token for key. When it refuses, retryAfter says how long
// until a token is available.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)

	b, found := l.buckets[key]
	if !found {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
}

// sweep drops buckets that have been idle long enough to be full again, so the
// map cannot grow without bound. It runs at most once per minute.
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < time.Minute {
		return
	}
	l.lastSweep = now
	full := time.Duration(l.burst / l.rate * float64(time.Second))
	for k, b := range l.buckets {
		if now.Sub(b.last) >= full {
			delete(l.buckets, k)
		}
	}
}
