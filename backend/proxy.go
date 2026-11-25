package main

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// Tek servis modu: FRONTEND_PROXY tanımlıysa API, /api/ dışındaki her isteği aynı
// kapsayıcıda çalışan Django'ya iletir. Böylece site tek adres ve tek servis olur;
// ücretsiz barındırmada yarı kaynak harcar ve "API uykuda" sorunu ortadan kalkar.
func withFrontend(api http.Handler, target string) (http.Handler, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	rp := httputil.NewSingleHostReverseProxy(u)
	orig := rp.Director
	rp.Director = func(r *http.Request) {
		host := r.Host
		orig(r)
		// Django'nun ALLOWED_HOSTS / CSRF kontrolleri tarayıcının gördüğü adı görsün.
		r.Host = host
		if r.Header.Get("X-Forwarded-Proto") == "" {
			if r.TLS != nil {
				r.Header.Set("X-Forwarded-Proto", "https")
			} else {
				r.Header.Set("X-Forwarded-Proto", "http")
			}
		}
	}
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		slog.Warn("arayüze ulaşılamadı", "path", r.URL.Path, "err", err)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="5">` +
			`<title>uniTech</title><p style="font-family:system-ui;padding:2rem">Sistem açılıyor, sayfa birkaç saniye içinde kendiliğinden yenilenecek…</p>`))
	}
	rp.FlushInterval = 100 * time.Millisecond
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}
		rp.ServeHTTP(w, r)
	}), nil
}
