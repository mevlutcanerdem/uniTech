package main

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"strconv"
	"strings"
	"time"
)

const (
	// Ücretsiz sunucunun düşük işlemci payında girişin birkaç saniye sürmemesi için 210 bin.
	// Özet içinde tur sayısı saklandığından ileride artırılabilir; eski özetler çalışmaya devam eder.
	pbkdf2Iter = 210_000
	tokenTTL   = 7 * 24 * time.Hour
	codeTTL    = 15 * time.Minute
	maxCodeTry = 5
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
	if err != nil || iter < 1 {
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

// dummyHash, var olmayan e-postalarla girişte de aynı sürede yanıt vermek için kullanılır.
var dummyHash, _ = HashPassword("zamanlama-saldirisina-karsi")

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
	Ver int   `json:"ver"` // şifre değişince artar; eski oturumları geçersiz kılar
	Exp int64 `json:"exp"`
}

type Tokens struct{ secret []byte }

var ErrBadToken = errors.New("geçersiz oturum")

func (t Tokens) Issue(u User) (string, error) {
	p, err := json.Marshal(claims{UID: u.ID, Ver: u.TokenVersion, Exp: time.Now().Add(tokenTTL).Unix()})
	if err != nil {
		return "", err
	}
	body := b64.EncodeToString(p)
	return body + "." + b64.EncodeToString(t.mac("token", body)), nil
}

// Verify (kullanıcı id, token sürümü) döner.
func (t Tokens) Verify(tok string) (int, int, error) {
	body, sig, ok := strings.Cut(tok, ".")
	if !ok {
		return 0, 0, ErrBadToken
	}
	got, err := b64.DecodeString(sig)
	if err != nil || !hmac.Equal(got, t.mac("token", body)) {
		return 0, 0, ErrBadToken
	}
	raw, err := b64.DecodeString(body)
	if err != nil {
		return 0, 0, ErrBadToken
	}
	var c claims
	if json.Unmarshal(raw, &c) != nil || c.UID == 0 {
		return 0, 0, ErrBadToken
	}
	if time.Now().Unix() > c.Exp {
		return 0, 0, errors.New("oturumun süresi doldu, tekrar giriş yap")
	}
	return c.UID, c.Ver, nil
}

func (t Tokens) mac(scope, body string) []byte {
	m := hmac.New(sha256.New, t.secret)
	m.Write([]byte(scope + "\x00" + body))
	return m.Sum(nil)
}

// ---- Tek kullanımlık kodlar ----

// NewCode 6 haneli rastgele bir kod üretir.
func NewCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// HashCode kodu düz metin saklamamak için kullanıcı ve amaca bağlı olarak özetler.
func (t Tokens) HashCode(userID int, purpose, code string) string {
	return hex.EncodeToString(t.mac("code", fmt.Sprintf("%d:%s:%s", userID, purpose, code)))
}

func (t Tokens) CheckCode(c AuthCode, code string, now time.Time) bool {
	if now.After(c.ExpiresAt) || c.Attempts >= maxCodeTry {
		return false
	}
	want := t.HashCode(c.UserID, c.Purpose, strings.TrimSpace(code))
	return subtle.ConstantTimeCompare([]byte(want), []byte(c.CodeHash)) == 1
}
