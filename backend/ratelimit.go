package main

import (
	"sync"
	"time"
)

// Limiter anahtar başına kayan pencere sınırlayıcı (tek sunucu örneği için bellekte).
type Limiter struct {
	mu     sync.Mutex
	window time.Duration
	max    int
	hits   map[string][]time.Time
	calls  int
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{window: window, max: max, hits: map[string][]time.Time{}}
}

func (l *Limiter) recentLocked(key string, now time.Time) []time.Time {
	cut := now.Add(-l.window)
	h := l.hits[key]
	i := 0
	for i < len(h) && !h[i].After(cut) {
		i++
	}
	h = h[i:]
	if len(h) == 0 {
		delete(l.hits, key)
	} else {
		l.hits[key] = h
	}
	return h
}

// Allow isteği sayar; sınır aşıldıysa false ve kaç saniye sonra tekrar denenebileceğini döner.
func (l *Limiter) Allow(key string) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.gcLocked(now)
	h := l.recentLocked(key, now)
	if len(h) >= l.max {
		wait := int(h[0].Add(l.window).Sub(now).Seconds()) + 1
		return false, wait
	}
	l.hits[key] = append(h, now)
	return true, 0
}

// Blocked saymadan kontrol eder (hatalı giriş sayacı gibi kullanım için).
func (l *Limiter) Blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recentLocked(key, time.Now())) >= l.max
}

// Hit sadece sayar.
func (l *Limiter) Hit(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.hits[key] = append(l.recentLocked(key, now), now)
}

func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}

// gcLocked arada bir süresi dolmuş anahtarları temizler.
func (l *Limiter) gcLocked(now time.Time) {
	l.calls++
	if l.calls%1000 != 0 {
		return
	}
	for k := range l.hits {
		l.recentLocked(k, now)
	}
}

// Limits uygulamanın kullandığı tüm sınırlar.
type Limits struct {
	LoginFail   *Limiter // e-posta başına hatalı giriş
	LoginIP     *Limiter // IP başına giriş denemesi
	RegisterIP  *Limiter
	ForgotEmail *Limiter
	ForgotIP    *Limiter
	ResendShort *Limiter // kullanıcı başına kod tekrar gönderme (dakikada 1)
	ResendLong  *Limiter // kullanıcı başına kod tekrar gönderme (saatte 5)
	Tickets     *Limiter // öğrenci başına arıza bildirimi
	Photos      *Limiter
}

func DefaultLimits() Limits {
	return Limits{
		LoginFail:   NewLimiter(10, 15*time.Minute),
		LoginIP:     NewLimiter(30, 15*time.Minute),
		RegisterIP:  NewLimiter(10, time.Hour),
		ForgotEmail: NewLimiter(3, time.Hour),
		ForgotIP:    NewLimiter(10, time.Hour),
		ResendShort: NewLimiter(1, time.Minute),
		ResendLong:  NewLimiter(5, time.Hour),
		Tickets:     NewLimiter(10, time.Hour),
		Photos:      NewLimiter(30, time.Hour),
	}
}
