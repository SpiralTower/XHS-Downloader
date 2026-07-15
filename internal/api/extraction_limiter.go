package api

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	publicExtractionRateWindow        = time.Minute
	maxPublicExtractionLimiterEntries = 4096
)

type publicExtractionAttempt struct {
	Requests    int
	WindowStart time.Time
}

// publicExtractionGate bounds anonymous extraction work while leaving
// authenticated administrators on an independent path.
type publicExtractionGate struct {
	mu sync.Mutex

	perHost       map[string]publicExtractionAttempt
	perHostLimit  int
	globalLimit   int
	globalCount   int
	globalStarted time.Time

	slots chan struct{}
}

func newPublicExtractionGate(perHostLimit, globalLimit, concurrency int) *publicExtractionGate {
	if perHostLimit < 1 {
		perHostLimit = 1
	}
	if globalLimit < 1 {
		globalLimit = 1
	}
	if concurrency < 1 {
		concurrency = 1
	}
	return &publicExtractionGate{
		perHost:      make(map[string]publicExtractionAttempt),
		perHostLimit: perHostLimit,
		globalLimit:  globalLimit,
		slots:        make(chan struct{}, concurrency),
	}
}

func (g *publicExtractionGate) acquire() (func(), time.Duration, bool) {
	select {
	case g.slots <- struct{}{}:
	default:
		return nil, time.Second, false
	}
	var once sync.Once
	return func() {
		once.Do(func() { <-g.slots })
	}, 0, true
}

func (g *publicExtractionGate) reserve(key string, now time.Time) (time.Duration, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.globalStarted.IsZero() || !now.Before(g.globalStarted.Add(publicExtractionRateWindow)) {
		g.globalStarted = now
		g.globalCount = 0
	}
	g.pruneLocked(now)

	attempt, exists := g.perHost[key]
	retryAfter := time.Duration(0)
	if g.globalCount >= g.globalLimit {
		retryAfter = remainingRateWindow(g.globalStarted, now)
	}
	if exists && attempt.Requests >= g.perHostLimit {
		hostRetry := remainingRateWindow(attempt.WindowStart, now)
		if hostRetry > retryAfter {
			retryAfter = hostRetry
		}
	}
	if retryAfter > 0 {
		return retryAfter, false
	}
	if !exists && len(g.perHost) >= maxPublicExtractionLimiterEntries {
		return publicExtractionRateWindow, false
	}
	if !exists {
		attempt = publicExtractionAttempt{WindowStart: now}
	}
	attempt.Requests++
	g.perHost[key] = attempt
	g.globalCount++
	return 0, true
}

func (g *publicExtractionGate) pruneLocked(now time.Time) {
	for key, attempt := range g.perHost {
		if !now.Before(attempt.WindowStart.Add(publicExtractionRateWindow)) {
			delete(g.perHost, key)
		}
	}
}

func remainingRateWindow(start, now time.Time) time.Duration {
	remaining := start.Add(publicExtractionRateWindow).Sub(now)
	if remaining <= 0 || remaining > publicExtractionRateWindow {
		return publicExtractionRateWindow
	}
	return remaining
}

func (a *App) allowAnonymousExtraction(
	writer http.ResponseWriter,
	request *http.Request,
	authenticated bool,
) bool {
	if authenticated {
		return true
	}
	retryAfter, allowed := a.publicExtractions.reserve(
		rateLimitSourceKey(request), time.Now().UTC(),
	)
	if allowed {
		return true
	}
	writeAnonymousExtractionLimit(writer, retryAfter)
	return false
}

func (a *App) beginAnonymousExtractionWork(
	writer http.ResponseWriter,
	authenticated bool,
) (func(), bool) {
	if authenticated {
		return func() {}, true
	}
	release, retryAfter, allowed := a.publicExtractions.acquire()
	if allowed {
		return release, true
	}
	writeAnonymousExtractionLimit(writer, retryAfter)
	return nil, false
}

func writeAnonymousExtractionLimit(writer http.ResponseWriter, retryAfter time.Duration) {
	seconds := int64((retryAfter + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	writer.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
	writeAPIError(
		writer, http.StatusTooManyRequests, "EXTRACTION_RATE_LIMITED",
		"匿名解析请求过于频繁，请稍后重试",
	)
}
