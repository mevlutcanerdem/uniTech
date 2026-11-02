package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

type authResp struct {
	Token string `json:"token"`
	User  User   `json:"user"`
	// Kayıtta: doğrulama gerekiyorsa ve kod gönderildiyse.
	VerificationRequired bool `json:"verification_required,omitempty"`
	CodeSent             bool `json:"code_sent,omitempty"`
}

func validPassword(pw string) bool {
	n := utf8.RuneCountInString(pw)
	return n >= 8 && n <= 128
}

// sendCode yeni bir kod üretir, özetini saklar ve e-postayla gönderir.
func (a *App) sendCode(ctx context.Context, u User, purpose, lng string) error {
	if a.mailer == nil {
		return errors.New("e-posta gönderici tanımlı değil")
	}
	code, err := NewCode()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := a.store.SaveCode(ctx, AuthCode{UserID: u.ID, Purpose: purpose, CodeHash: a.tokens.HashCode(u.ID, purpose, code),
		ExpiresAt: now.Add(codeTTL), SentAt: now}); err != nil {
		return err
	}
	m := codeMail(lng, purpose, strings.Fields(u.Name + " ")[0], code)
	m.To = u.Email
	sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := a.mailer.Send(sendCtx, m); err != nil {
		slog.Error("e-posta gönderilemedi", "to", u.Email, "purpose", purpose, "err", err)
		return err
	}
	return nil
}

func (a *App) register(w http.ResponseWriter, r *http.Request) {
	if ok, wait := a.limits.RegisterIP.Allow(a.clientIP(r)); !ok {
		tooMany(w, wait, "çok fazla kayıt denemesi, biraz sonra tekrar dene")
		return
	}
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
	if !validPassword(in.Password) {
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
	u, err := a.store.CreateUser(r.Context(), User{Name: name, Email: email, PassHash: hash, Role: role, Verified: a.mailer == nil})
	if errors.Is(err, ErrEmailTaken) {
		writeErr(w, 409, err.Error(), "email_taken")
		return
	} else if err != nil {
		slog.Error("kayıt", "err", err)
		writeErr(w, 500, "hesap oluşturulamadı")
		return
	}
	resp := authResp{User: u}
	resp.Token, _ = a.tokens.Issue(u)
	if !u.Verified {
		a.limits.ResendShort.Hit(itoaInt(u.ID))
		a.limits.ResendLong.Hit(itoaInt(u.ID))
		resp.VerificationRequired = true
		resp.CodeSent = a.sendCode(r.Context(), u, PurposeVerify, lang(r)) == nil
	}
	writeJSON(w, 201, resp)
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if !readJSON(w, r, &in) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if a.limits.LoginFail.Blocked(email) {
		writeErr(w, 429, "çok fazla hatalı deneme, 15 dakika sonra tekrar dene", "rate_limited")
		return
	}
	if ok, wait := a.limits.LoginIP.Allow(a.clientIP(r)); !ok {
		tooMany(w, wait, "çok fazla giriş denemesi, biraz sonra tekrar dene")
		return
	}
	u, err := a.store.UserByEmail(r.Context(), email)
	hash := u.PassHash
	if err != nil {
		hash = dummyHash // var olmayan hesaplar için de aynı süre
	}
	if !CheckPassword(hash, in.Password) || err != nil {
		a.limits.LoginFail.Hit(email)
		writeErr(w, 401, "e-posta veya şifre hatalı", "bad_credentials")
		return
	}
	a.limits.LoginFail.Reset(email)
	tok, _ := a.tokens.Issue(u)
	writeJSON(w, 200, authResp{Token: tok, User: u})
}

func (a *App) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, userFrom(r))
}

// checkCode kodu doğrular; hatalıysa deneme sayısını artırır.
func (a *App) checkCode(ctx context.Context, u User, purpose, code string) error {
	c, err := a.store.GetCode(ctx, u.ID, purpose)
	if err != nil {
		return ErrCodeInvalid
	}
	if !a.tokens.CheckCode(c, code, time.Now()) {
		_ = a.store.IncCodeAttempts(ctx, u.ID, purpose)
		if c.Attempts+1 >= maxCodeTry {
			return errors.New("çok fazla hatalı deneme, yeni bir kod iste")
		}
		return ErrCodeInvalid
	}
	return a.store.DeleteCode(ctx, u.ID, purpose)
}

func (a *App) verify(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u.Verified {
		writeJSON(w, 200, u)
		return
	}
	var in struct{ Code string }
	if !readJSON(w, r, &in) {
		return
	}
	if err := a.checkCode(r.Context(), u, PurposeVerify, in.Code); err != nil {
		writeErr(w, 400, err.Error(), "bad_code")
		return
	}
	if err := a.store.SetVerified(r.Context(), u.ID); err != nil {
		writeErr(w, 500, "doğrulama kaydedilemedi")
		return
	}
	u.Verified = true
	writeJSON(w, 200, u)
}

func (a *App) resendVerify(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u.Verified || a.mailer == nil {
		writeJSON(w, 200, map[string]bool{"sent": false})
		return
	}
	key := itoaInt(u.ID)
	if ok, wait := a.limits.ResendShort.Allow(key); !ok {
		tooMany(w, wait, "yeni kod istemeden önce biraz bekle")
		return
	}
	if ok, wait := a.limits.ResendLong.Allow(key); !ok {
		tooMany(w, wait, "çok fazla kod istedin, bir saat sonra tekrar dene")
		return
	}
	if err := a.sendCode(r.Context(), u, PurposeVerify, lang(r)); err != nil {
		writeErr(w, 502, "kod gönderilemedi, biraz sonra tekrar dene", "mail_failed")
		return
	}
	writeJSON(w, 200, map[string]bool{"sent": true})
}

func (a *App) forgot(w http.ResponseWriter, r *http.Request) {
	if a.mailer == nil {
		writeErr(w, 503, "şifre sıfırlama şu an kullanılamıyor, teknik destekle iletişime geç", "mail_disabled")
		return
	}
	var in struct{ Email string }
	if !readJSON(w, r, &in) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if ok, wait := a.limits.ForgotIP.Allow(a.clientIP(r)); !ok {
		tooMany(w, wait, "çok fazla istek, biraz sonra tekrar dene")
		return
	}
	if ok, wait := a.limits.ForgotEmail.Allow(email); !ok {
		tooMany(w, wait, "bu adrese çok fazla kod istendi, bir saat sonra tekrar dene")
		return
	}
	// Hesabın var olup olmadığını belli etmemek için yanıt her durumda aynı ve hemen döner.
	if u, err := a.store.UserByEmail(r.Context(), email); err == nil {
		lng := lang(r)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = a.sendCode(ctx, u, PurposeReset, lng)
		}()
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) reset(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Code, Password string }
	if !readJSON(w, r, &in) {
		return
	}
	if !validPassword(in.Password) {
		writeErr(w, 400, "şifre en az 8 karakter olmalı")
		return
	}
	u, err := a.store.UserByEmail(r.Context(), strings.ToLower(strings.TrimSpace(in.Email)))
	if err != nil {
		writeErr(w, 400, ErrCodeInvalid.Error(), "bad_code")
		return
	}
	if err := a.checkCode(r.Context(), u, PurposeReset, in.Code); err != nil {
		writeErr(w, 400, err.Error(), "bad_code")
		return
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		writeErr(w, 500, "şifre işlenemedi")
		return
	}
	u, err = a.store.SetPassword(r.Context(), u.ID, hash)
	if err != nil {
		writeErr(w, 500, "şifre kaydedilemedi")
		return
	}
	a.limits.LoginFail.Reset(u.Email)
	tok, _ := a.tokens.Issue(u)
	writeJSON(w, 200, authResp{Token: tok, User: u})
}
