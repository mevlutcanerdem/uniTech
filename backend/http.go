package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type App struct {
	store          Store
	tokens         Tokens
	hub            *Hub
	mailer         Mailer // nil: e-posta doğrulaması kapalı
	limits         Limits
	adminEmails    map[string]bool
	frontendOrigin string
	internalKey    string // Django'nun gerçek istemci IP'sini iletmesi için paylaşılan anahtar
	version        string

	pubMu    sync.Mutex
	pubAt    time.Time
	pubStats PublicStats
}

type ctxKey struct{}

func userFrom(r *http.Request) User { return r.Context().Value(ctxKey{}).(User) }

// ---- yanıt yardımcıları ----

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr hata mesajını (Türkçe; arayüz çevirir) ve makine tarafından okunabilir kodu yazar.
func writeErr(w http.ResponseWriter, status int, msg string, code ...string) {
	body := map[string]string{"error": msg}
	if len(code) > 0 {
		body["code"] = code[0]
	}
	writeJSON(w, status, body)
}

func tooMany(w http.ResponseWriter, wait int, msg string) {
	w.Header().Set("Retry-After", strconv.Itoa(wait))
	writeErr(w, http.StatusTooManyRequests, msg, "rate_limited")
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "istek gövdesi okunamadı")
		return false
	}
	return true
}

// clean boşlukları kırpar ve uzunluğu sınırlar.
func clean(s string, max int) (string, bool) {
	s = strings.TrimSpace(s)
	return s, utf8.RuneCountInString(s) <= max
}

func lang(r *http.Request) string {
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Accept-Language")), "en") {
		return "en"
	}
	return "tr"
}

// clientIP gerçek istemci adresi. Django'dan gelen isteklerde (paylaşılan anahtarla) X-Client-IP,
// diğerlerinde Render'ın eklediği X-Forwarded-For'un ilk değeri kullanılır.
func (a *App) clientIP(r *http.Request) string {
	if a.internalKey != "" && r.Header.Get("X-Internal-Key") == a.internalKey {
		if ip := strings.TrimSpace(r.Header.Get("X-Client-IP")); ip != "" {
			return ip
		}
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---- ara katmanlar ----

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(c int) { s.status = c; s.ResponseWriter.WriteHeader(c) }
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func withMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("Cache-Control", "no-store")
		defer func() {
			if v := recover(); v != nil {
				slog.Error("panic", "path", r.URL.Path, "err", v, "stack", string(debug.Stack()))
				writeErr(rec, http.StatusInternalServerError, "beklenmeyen bir hata oluştu")
			}
			if r.URL.Path != "/api/events" && r.URL.Path != "/api/health" {
				slog.Info("istek", "method", r.Method, "path", r.URL.Path, "status", rec.status, "ms", time.Since(start).Milliseconds())
			}
		}()
		h.ServeHTTP(rec, r)
	})
}

func (a *App) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || tok == "" {
			tok = r.URL.Query().Get("token") // EventSource başlık gönderemez
		}
		uid, ver, err := a.tokens.Verify(tok)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, err.Error(), "unauthorized")
			return
		}
		u, err := a.store.UserByID(r.Context(), uid)
		if err != nil || u.TokenVersion != ver {
			writeErr(w, http.StatusUnauthorized, "oturumun kapandı, tekrar giriş yap", "unauthorized")
			return
		}
		// Doğrulama kapalıysa (e-posta gönderici tanımlı değil) eski doğrulanmamış hesaplar açılır.
		if !u.Verified && a.mailer == nil {
			if a.store.SetVerified(r.Context(), u.ID) == nil {
				u.Verified = true
			}
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	}
}

func (a *App) verified(next http.HandlerFunc) http.HandlerFunc {
	return a.auth(func(w http.ResponseWriter, r *http.Request) {
		if !userFrom(r).Verified {
			writeErr(w, http.StatusForbidden, "önce e-posta adresini doğrulaman gerekiyor", "email_unverified")
			return
		}
		next(w, r)
	})
}

func (a *App) staff(next http.HandlerFunc) http.HandlerFunc {
	return a.verified(func(w http.ResponseWriter, r *http.Request) {
		if !userFrom(r).IsStaff() {
			writeErr(w, http.StatusForbidden, "bu işlem sadece teknik ekip içindir", "forbidden")
			return
		}
		next(w, r)
	})
}

func (a *App) admin(next http.HandlerFunc) http.HandlerFunc {
	return a.verified(func(w http.ResponseWriter, r *http.Request) {
		if userFrom(r).Role != RoleAdmin {
			writeErr(w, http.StatusForbidden, "bu işlem sadece yöneticiler içindir", "forbidden")
			return
		}
		next(w, r)
	})
}

func (a *App) cors(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.frontendOrigin != "" {
			w.Header().Set("Access-Control-Allow-Origin", a.frontendOrigin)
			w.Header().Set("Vary", "Origin")
		}
		next(w, r)
	}
}

// ---- yönlendirme ----

func (a *App) Routes() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/health", a.health)
	m.HandleFunc("GET /api/meta", a.meta)
	m.HandleFunc("GET /api/public/stats", a.publicStats)

	m.HandleFunc("POST /api/auth/register", a.register)
	m.HandleFunc("POST /api/auth/login", a.login)
	m.HandleFunc("POST /api/auth/verify", a.auth(a.verify))
	m.HandleFunc("POST /api/auth/verify/resend", a.auth(a.resendVerify))
	m.HandleFunc("POST /api/auth/forgot", a.forgot)
	m.HandleFunc("POST /api/auth/reset", a.reset)
	m.HandleFunc("GET /api/me", a.auth(a.me))

	m.HandleFunc("POST /api/tickets", a.verified(a.createTicket))
	m.HandleFunc("GET /api/tickets", a.auth(a.listTickets))
	m.HandleFunc("GET /api/tickets/{id}", a.auth(a.getTicket))
	m.HandleFunc("PATCH /api/tickets/{id}", a.staff(a.updateTicket))
	m.HandleFunc("POST /api/tickets/{id}/photos", a.verified(a.uploadPhoto))
	m.HandleFunc("GET /api/photos/{id}", a.auth(a.getPhoto))
	m.HandleFunc("GET /api/stats", a.staff(a.stats))
	m.HandleFunc("GET /api/reports", a.staff(a.reports))
	m.HandleFunc("GET /api/events", a.cors(a.staff(a.hub.ServeSSE)))

	m.HandleFunc("GET /api/users", a.admin(a.listUsers))
	m.HandleFunc("PATCH /api/users/{id}", a.admin(a.setRole))
	return withMiddleware(m)
}

// ---- genel uç noktalar ----

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	mail := "off"
	if a.mailer != nil {
		mail = a.mailer.Kind()
	}
	writeJSON(w, 200, map[string]string{"status": "ok", "storage": a.store.Kind(), "mail": mail, "version": a.version})
}

func (a *App) meta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"categories": Categories, "priorities": Priorities, "statuses": Statuses, "domain": AllowedDomain,
		"verification": a.mailer != nil, "max_photos": MaxPhotosPerTicket,
	})
}

func (a *App) publicStats(w http.ResponseWriter, r *http.Request) {
	a.pubMu.Lock()
	defer a.pubMu.Unlock()
	if time.Since(a.pubAt) > time.Minute {
		ts, err := a.store.AllTickets(r.Context())
		if err != nil {
			writeErr(w, 500, "istatistikler alınamadı")
			return
		}
		a.pubStats, a.pubAt = BuildPublicStats(ts, time.Now()), time.Now()
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	writeJSON(w, 200, a.pubStats)
}

func idParam(r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	return id, err == nil && id > 0
}

func isNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
