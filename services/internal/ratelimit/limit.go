package ratelimit

import (
	"sync"
	"time"
)

// Limiter is a simple in-memory fixed-window limiter.
type Limiter struct {
	mu       sync.Mutex
	window   time.Duration
	max      int
	hits     map[string][]time.Time
}

func New(window time.Duration, max int) *Limiter {
	if window <= 0 {
		window = time.Minute
	}
	if max <= 0 {
		max = 60
	}
	return &Limiter{window: window, max: max, hits: map[string][]time.Time{}}
}

func (l *Limiter) Allow(key string) bool {
	now := time.Now()
	cutoff := now.Add(-l.window)
	l.mu.Lock()
	defer l.mu.Unlock()
	arr := l.hits[key]
	kept := arr[:0]
	for _, t := range arr {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}
