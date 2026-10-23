package main

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	pbkdf2Iter = 210_000
	tokenTTL   = 7 * 24 * time.Hour
	// Sadece bu alan adındaki e-postalar kayıt olabilir.
	AllowedDomain = "agu.edu.tr"
)

var b64 = base64.RawURLEncoding

// ---- Şifre ----

func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, pw, salt, pbkdf2Iter, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iter, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

func CheckPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	salt, err1 := b64.DecodeString(parts[2])
	want, err2 := b64.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// ---- E-posta ----

// NormalizeEmail adresi küçük harfe çevirir ve @agu.edu.tr alan adını zorunlu kılar.
func NormalizeEmail(raw string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(raw))
	a, err := mail.ParseAddress(e)
	if err != nil || a.Address != e {
		return "", errors.New("geçerli bir e-posta adresi gir")
	}
	at := strings.LastIndex(e, "@")
	if at < 1 || e[at+1:] != AllowedDomain {
		return "", errors.New("sadece @" + AllowedDomain + " uzantılı okul e-postasıyla kayıt olunabilir")
	}
	return e, nil
}

// ---- Token (HMAC-SHA256 imzalı) ----

type claims struct {
	UID int   `json:"uid"`
	Exp int64 `json:"exp"`
}

type Tokens struct{ secret []byte }

func (t Tokens) Issue(uid int) (string, error) {
	p, err := json.Marshal(claims{UID: uid, Exp: time.Now().Add(tokenTTL).Unix()})
	if err != nil {
		return "", err
	}
	body := b64.EncodeToString(p)
	return body + "." + b64.EncodeToString(t.sign(body)), nil
}

func (t Tokens) Verify(tok string) (int, error) {
	body, sig, ok := strings.Cut(tok, ".")
	if !ok {
		return 0, errors.New("geçersiz oturum")
	}
	got, err := b64.DecodeString(sig)
	if err != nil || !hmac.Equal(got, t.sign(body)) {
		return 0, errors.New("geçersiz oturum")
	}
	raw, err := b64.DecodeString(body)
	if err != nil {
		return 0, errors.New("geçersiz oturum")
	}
	var c claims
	if json.Unmarshal(raw, &c) != nil || c.UID == 0 {
		return 0, errors.New("geçersiz oturum")
	}
	if time.Now().Unix() > c.Exp {
		return 0, errors.New("oturumun süresi doldu, tekrar giriş yap")
	}
	return c.UID, nil
}

func (t Tokens) sign(body string) []byte {
	m := hmac.New(sha256.New, t.secret)
	m.Write([]byte(body))
	return m.Sum(nil)
}

// ---- Giriş denemesi sınırı ----

// loginLimiter aynı e-posta için 15 dakikada en fazla 10 hatalı denemeye izin verir.
type loginLimiter struct {
	mu   sync.Mutex
	fail map[string][]time.Time
}

func newLoginLimiter() *loginLimiter { return &loginLimiter{fail: map[string][]time.Time{}} }

func (l *loginLimiter) recent(key string) []time.Time {
	cut := time.Now().Add(-15 * time.Minute)
	keep := l.fail[key][:0]
	for _, t := range l.fail[key] {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	l.fail[key] = keep
	return keep
}

func (l *loginLimiter) Blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(key)) >= 10
}

func (l *loginLimiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fail[key] = append(l.recent(key), time.Now())
}

func (l *loginLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fail, key)
}
