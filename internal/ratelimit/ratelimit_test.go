package ratelimit

import (
	"testing"
	"time"
)

func TestAllowAndRefill(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := New(2, func() time.Time { return now }) // burst 2, one token per 30s

	for i := 0; i < 2; i++ {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("request %d should pass", i)
		}
	}
	ok, retry := l.Allow("a")
	if ok || retry != 30*time.Second {
		t.Fatalf("third request: ok=%v retry=%v", ok, retry)
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Error("another key has its own bucket")
	}
	now = now.Add(30 * time.Second)
	if ok, _ := l.Allow("a"); !ok {
		t.Error("a token should have refilled")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Error("only one token should have refilled")
	}
}

func TestSweepDropsIdleBuckets(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := New(60, func() time.Time { return now })
	l.Allow("idle")
	now = now.Add(5 * time.Minute)
	l.Allow("fresh") // triggers the sweep
	if _, ok := l.buckets["idle"]; ok {
		t.Error("idle bucket should have been swept")
	}
	if _, ok := l.buckets["fresh"]; !ok {
		t.Error("fresh bucket must stay")
	}
}

func TestDefaultClock(t *testing.T) {
	if ok, _ := New(1, nil).Allow("k"); !ok {
		t.Error("first request must pass with the real clock")
	}
}
