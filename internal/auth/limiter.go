package auth

import (
	"sync"
	"time"
)

type attemptLimiter struct {
	mu        sync.Mutex
	attempts  map[string][]time.Time
	lastSweep time.Time
}

const (
	perIPMinute  = 5
	perIPHour    = 20
	globalMinute = 30
	globalHour   = 200
)

func newAttemptLimiter() *attemptLimiter {
	return &attemptLimiter{attempts: make(map[string][]time.Time)}
}

func (l *attemptLimiter) Allow(key string, now time.Time) bool {
	if key == "" {
		key = "unknown"
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepLocked(now)
	ipKey := "ip:" + key
	if !l.hasCapacityLocked(ipKey, now, perIPMinute, perIPHour) {
		return false
	}
	if !l.hasCapacityLocked("global", now, globalMinute, globalHour) {
		return false
	}
	l.attempts[ipKey] = append(l.attempts[ipKey], now)
	l.attempts["global"] = append(l.attempts["global"], now)
	return true
}

func (l *attemptLimiter) sweepLocked(now time.Time) {
	if !l.lastSweep.IsZero() && !now.Before(l.lastSweep.Add(time.Hour)) {
		cutoff := now.Add(-time.Hour)
		for key, values := range l.attempts {
			kept := values[:0]
			for _, value := range values {
				if !value.Before(cutoff) {
					kept = append(kept, value)
				}
			}
			if len(kept) == 0 {
				delete(l.attempts, key)
			} else {
				l.attempts[key] = kept
			}
		}
	}
	if l.lastSweep.IsZero() || now.Before(l.lastSweep) || !now.Before(l.lastSweep.Add(time.Hour)) {
		l.lastSweep = now
	}
}

func (l *attemptLimiter) hasCapacityLocked(key string, now time.Time, minuteLimit, hourLimit int) bool {
	cutoff := now.Add(-time.Hour)
	values := l.attempts[key]
	kept := values[:0]
	minuteCount := 0
	for _, value := range values {
		if value.Before(cutoff) {
			continue
		}
		kept = append(kept, value)
		if !value.Before(now.Add(-time.Minute)) {
			minuteCount++
		}
	}
	if len(kept) == 0 {
		delete(l.attempts, key)
	} else {
		l.attempts[key] = kept
	}
	return len(kept) < hourLimit && minuteCount < minuteLimit
}
