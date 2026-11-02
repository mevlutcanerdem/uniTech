// uniTech API — Abdullah Gül Üniversitesi arıza bildirim sistemi.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	if err := run(); err != nil {
		slog.Error("sunucu durdu", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr := getenv("ADDR", ":8080")
	if port := os.Getenv("PORT"); port != "" { // Render gibi platformlar PORT verir
		addr = ":" + port
	}

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		secret = fmt.Sprintf("%x", b)
		slog.Warn("JWT_SECRET tanımlı değil; rastgele üretildi. Sunucu yeniden başlarsa herkesin oturumu kapanır.")
	}

	admins := map[string]bool{}
	for _, e := range strings.Split(os.Getenv("ADMIN_EMAILS"), ",") {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			admins[e] = true
		}
	}

	var store Store
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		pg, err := NewPGStore(ctx, dbURL)
		if err != nil {
			return err
		}
		store = pg
	} else {
		mem, err := NewMemStore(os.Getenv("DATA_PATH"))
		if err != nil {
			return fmt.Errorf("veri dosyası açılamadı: %w", err)
		}
		store = mem
		slog.Warn("DATABASE_URL tanımlı değil; veriler kalıcı veritabanında değil", "depo", mem.Kind())
	}
	defer store.Close()

	mailer := NewMailerFromEnv()
	mailKind := "kapalı (doğrulama yok)"
	if mailer != nil {
		mailKind = mailer.Kind()
	}

	version := os.Getenv("RENDER_GIT_COMMIT")
	if len(version) > 7 {
		version = version[:7]
	}
	app := &App{
		store:          store,
		tokens:         Tokens{secret: []byte(secret)},
		hub:            NewHub(),
		mailer:         mailer,
		limits:         DefaultLimits(),
		adminEmails:    admins,
		frontendOrigin: getenv("FRONTEND_ORIGIN", "http://localhost:8000"),
		internalKey:    os.Getenv("INTERNAL_API_KEY"),
		version:        getenv("APP_VERSION", version),
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           app.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second, // fotoğraf yüklemeleri için
		IdleTimeout:       120 * time.Second,
		// WriteTimeout bilerek yok: canlı panel akışı (SSE) uzun süre açık kalır.
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("uniTech API çalışıyor", "adres", addr, "depo", store.Kind(), "eposta", mailKind)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		slog.Info("kapatılıyor")
		app.hub.CloseAll()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
	return nil
}
