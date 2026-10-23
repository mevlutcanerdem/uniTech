package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

type App struct {
	store          *Store
	tokens         Tokens
	hub            *Hub
	limiter        *loginLimiter
	adminEmails    map[string]bool
	frontendOrigin string
}

type ctxKey struct{}

func userFrom(r *http.Request) User { return r.Context().Value(ctxKey{}).(User) }

// ---- yardımcılar ----

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
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

// ---- middleware ----

func (a *App) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if tok == "" || tok == r.Header.Get("Authorization") {
			tok = r.URL.Query().Get("token") // EventSource başlık gönderemez
		}
		uid, err := a.tokens.Verify(tok)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, err.Error())
			return
		}
		u, err := a.store.UserByID(uid)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "hesap bulunamadı")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	}
}

func (a *App) staff(next http.HandlerFunc) http.HandlerFunc {
	return a.auth(func(w http.ResponseWriter, r *http.Request) {
		if !userFrom(r).IsStaff() {
			writeErr(w, http.StatusForbidden, "bu işlem sadece teknik ekip içindir")
			return
		}
		next(w, r)
	})
}

func (a *App) admin(next http.HandlerFunc) http.HandlerFunc {
	return a.auth(func(w http.ResponseWriter, r *http.Request) {
		if userFrom(r).Role != RoleAdmin {
			writeErr(w, http.StatusForbidden, "bu işlem sadece yöneticiler içindir")
			return
		}
		next(w, r)
	})
}

// ---- yönlendirme ----

func (a *App) Routes() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	m.HandleFunc("GET /api/meta", a.meta)
	m.HandleFunc("POST /api/auth/register", a.register)
	m.HandleFunc("POST /api/auth/login", a.login)
	m.HandleFunc("GET /api/me", a.auth(a.me))

	m.HandleFunc("POST /api/tickets", a.auth(a.createTicket))
	m.HandleFunc("GET /api/tickets", a.auth(a.listTickets))
	m.HandleFunc("GET /api/tickets/{id}", a.auth(a.getTicket))
	m.HandleFunc("PATCH /api/tickets/{id}", a.staff(a.updateTicket))
	m.HandleFunc("GET /api/stats", a.staff(a.stats))
	m.HandleFunc("GET /api/events", a.cors(a.staff(a.hub.ServeSSE)))

	m.HandleFunc("GET /api/users", a.admin(a.listUsers))
	m.HandleFunc("PATCH /api/users/{id}", a.admin(a.setRole))
	return logRequests(m)
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

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/events" {
			log.Printf("%s %s", r.Method, r.URL.Path)
		}
		h.ServeHTTP(w, r)
	})
}

// ---- uç noktalar ----

func (a *App) meta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"categories": Categories, "priorities": Priorities, "statuses": Statuses, "domain": AllowedDomain,
	})
}

type authResp struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

func (a *App) register(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Email, Password string }
	if !readJSON(w, r, &in) {
		return
	}
	name, ok := clean(in.Name, 80)
	if name == "" || !ok {
		writeErr(w, 400, "ad soyad gerekli (en fazla 80 karakter)")
		return
	}
	email, err := NormalizeEmail(in.Email)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if n := utf8.RuneCountInString(in.Password); n < 8 || n > 128 {
		writeErr(w, 400, "şifre en az 8 karakter olmalı")
		return
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		writeErr(w, 500, "şifre işlenemedi")
		return
	}
	role := RoleStudent
	if a.adminEmails[email] {
		role = RoleAdmin
	}
	u, err := a.store.CreateUser(name, email, hash, role)
	if errors.Is(err, ErrEmailTaken) {
		writeErr(w, 409, err.Error())
		return
	} else if err != nil {
		writeErr(w, 500, "hesap oluşturulamadı")
		return
	}
	tok, _ := a.tokens.Issue(u.ID)
	writeJSON(w, 201, authResp{tok, u.Public()})
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if !readJSON(w, r, &in) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if a.limiter.Blocked(email) {
		writeErr(w, 429, "çok fazla hatalı deneme, 15 dakika sonra tekrar dene")
		return
	}
	u, err := a.store.UserByEmail(email)
	if err != nil || !CheckPassword(u.PassHash, in.Password) {
		a.limiter.Fail(email)
		writeErr(w, 401, "e-posta veya şifre hatalı")
		return
	}
	a.limiter.Reset(email)
	tok, _ := a.tokens.Issue(u.ID)
	writeJSON(w, 200, authResp{tok, u.Public()})
}

func (a *App) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, userFrom(r).Public())
}

func (a *App) createTicket(w http.ResponseWriter, r *http.Request) {
	var in struct{ Category, Building, Room, Title, Description, Priority string }
	if !readJSON(w, r, &in) {
		return
	}
	var errs []string
	if _, ok := Categories[in.Category]; !ok {
		errs = append(errs, "arıza türü")
	}
	if _, ok := Priorities[in.Priority]; !ok {
		errs = append(errs, "aciliyet")
	}
	building, ok1 := clean(in.Building, 60)
	room, ok2 := clean(in.Room, 60)
	title, ok3 := clean(in.Title, 100)
	desc, ok4 := clean(in.Description, 2000)
	if building == "" || !ok1 {
		errs = append(errs, "bina")
	}
	if !ok2 {
		errs = append(errs, "kat / oda")
	}
	if title == "" || !ok3 {
		errs = append(errs, "kısa başlık")
	}
	if desc == "" || !ok4 {
		errs = append(errs, "açıklama")
	}
	if len(errs) > 0 {
		writeErr(w, 400, "eksik veya hatalı alan: "+strings.Join(errs, ", "))
		return
	}
	u := userFrom(r)
	t, err := a.store.CreateTicket(Ticket{
		Category: in.Category, Building: building, Room: room, Title: title, Description: desc,
		Priority: in.Priority, ReporterID: u.ID, ReporterName: u.Name, ReporterEmail: u.Email,
	})
	if err != nil {
		writeErr(w, 500, "arıza kaydedilemedi")
		return
	}
	a.hub.Publish(Event{"created", t.ID, t.Code, t.Title, t.Status, t.Priority})
	writeJSON(w, 201, anonymize(t))
}

func (a *App) listTickets(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	q := r.URL.Query()
	f := TicketFilter{Status: q.Get("status"), Category: q.Get("category"), Query: q.Get("q")}
	// Öğrenciler sadece kendi bildirimlerini görür; ekip scope=mine ile kendininkini isteyebilir.
	if !u.IsStaff() || q.Get("scope") == "mine" {
		f.ReporterID = u.ID
	}
	writeJSON(w, 200, anonymizeAll(a.store.ListTickets(f)))
}

func (a *App) ticketID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "arıza bulunamadı")
		return 0, false
	}
	return id, true
}

func (a *App) getTicket(w http.ResponseWriter, r *http.Request) {
	id, ok := a.ticketID(w, r)
	if !ok {
		return
	}
	t, err := a.store.TicketByID(id)
	u := userFrom(r)
	if err != nil || (!u.IsStaff() && t.ReporterID != u.ID) {
		writeErr(w, 404, "arıza bulunamadı")
		return
	}
	writeJSON(w, 200, anonymize(t))
}

func (a *App) updateTicket(w http.ResponseWriter, r *http.Request) {
	id, ok := a.ticketID(w, r)
	if !ok {
		return
	}
	var in struct{ Status, Note string }
	if !readJSON(w, r, &in) {
		return
	}
	if _, ok := Statuses[in.Status]; in.Status != "" && !ok {
		writeErr(w, 400, "geçersiz durum")
		return
	}
	note, ok := clean(in.Note, 500)
	if !ok {
		writeErr(w, 400, "not en fazla 500 karakter olabilir")
		return
	}
	if in.Status == "" && note == "" {
		writeErr(w, 400, "durum veya not gerekli")
		return
	}
	t, err := a.store.UpdateTicket(id, userFrom(r), in.Status, note)
	if errors.Is(err, ErrNotFound) {
		writeErr(w, 404, "arıza bulunamadı")
		return
	} else if err != nil {
		writeErr(w, 500, "güncellenemedi")
		return
	}
	a.hub.Publish(Event{"updated", t.ID, t.Code, t.Title, t.Status, t.Priority})
	writeJSON(w, 200, anonymize(t))
}

func (a *App) stats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.store.Stats())
}

func (a *App) listUsers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.store.ListUsers())
}

func (a *App) setRole(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "kullanıcı bulunamadı")
		return
	}
	var in struct{ Role string }
	if !readJSON(w, r, &in) {
		return
	}
	if !Roles[in.Role] {
		writeErr(w, 400, "geçersiz rol")
		return
	}
	if id == userFrom(r).ID && in.Role != RoleAdmin {
		writeErr(w, 400, "kendi yönetici yetkini kaldıramazsın")
		return
	}
	u, err := a.store.SetRole(id, in.Role)
	if err != nil {
		writeErr(w, 404, "kullanıcı bulunamadı")
		return
	}
	writeJSON(w, 200, u)
}
