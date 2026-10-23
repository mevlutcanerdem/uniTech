// uniTech API — Abdullah Gül Üniversitesi arıza bildirim sistemi.
package main

import (
	"crypto/rand"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	addr := getenv("ADDR", ":8080")
	dataPath := getenv("DATA_PATH", "data/unitech.json")

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		secret = fmt.Sprintf("%x", b)
		log.Println("UYARI: JWT_SECRET tanımlı değil; rastgele üretildi. Sunucu yeniden başlarsa herkesin oturumu kapanır.")
	}

	admins := map[string]bool{}
	for _, e := range strings.Split(os.Getenv("ADMIN_EMAILS"), ",") {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			admins[e] = true
		}
	}

	store, err := OpenStore(dataPath)
	if err != nil {
		log.Fatalf("veri dosyası açılamadı: %v", err)
	}

	app := &App{
		store:          store,
		tokens:         Tokens{secret: []byte(secret)},
		hub:            NewHub(),
		limiter:        newLoginLimiter(),
		adminEmails:    admins,
		frontendOrigin: getenv("FRONTEND_ORIGIN", "http://localhost:8000"),
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           app.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		// WriteTimeout bilerek yok: canlı panel akışı (SSE) uzun süre açık kalır.
	}
	log.Printf("uniTech API %s adresinde çalışıyor (veri: %s)", addr, dataPath)
	log.Fatal(srv.ListenAndServe())
}
