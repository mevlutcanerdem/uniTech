package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"sync"
	"time"
)

// Mailer doğrulama ve şifre sıfırlama e-postalarını gönderir.
type Mailer interface {
	Kind() string
	Send(ctx context.Context, m Mail) error
}

type Mail struct {
	To, Subject, Text, HTML string
}

type mailFrom struct{ Name, Email string }

// NewMailerFromEnv ortam değişkenlerine göre gönderici seçer:
// BREVO_API_KEY → Brevo HTTP API, SMTP_HOST → SMTP, MAIL_DEV_LOG=1 → kodları log'a yazar.
// Hiçbiri yoksa nil döner; bu durumda e-posta doğrulaması kapalıdır.
func NewMailerFromEnv() Mailer {
	from := mailFrom{Name: getenv("MAIL_FROM_NAME", "uniTech · AGÜ Teknik Destek"), Email: os.Getenv("MAIL_FROM")}
	switch {
	case os.Getenv("BREVO_API_KEY") != "":
		return &brevoMailer{key: os.Getenv("BREVO_API_KEY"), from: from, client: &http.Client{Timeout: 15 * time.Second}}
	case os.Getenv("SMTP_HOST") != "":
		return &smtpMailer{host: os.Getenv("SMTP_HOST"), port: getenv("SMTP_PORT", "587"),
			user: os.Getenv("SMTP_USER"), pass: os.Getenv("SMTP_PASS"), from: from}
	case os.Getenv("MAIL_DEV_LOG") == "1":
		return &logMailer{file: os.Getenv("MAIL_DEV_FILE")}
	}
	return nil
}

// ---- Brevo (https://www.brevo.com) — ücretsiz planda günde 300 e-posta ----

type brevoMailer struct {
	key    string
	from   mailFrom
	client *http.Client
}

func (b *brevoMailer) Kind() string { return "brevo" }

func (b *brevoMailer) Send(ctx context.Context, m Mail) error {
	body, _ := json.Marshal(map[string]any{
		"sender":      map[string]string{"name": b.from.Name, "email": b.from.Email},
		"to":          []map[string]string{{"email": m.To}},
		"subject":     m.Subject,
		"textContent": m.Text,
		"htmlContent": m.HTML,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.brevo.com/v3/smtp/email", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("api-key", b.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("brevo %d: %s", res.StatusCode, msg)
	}
	return nil
}

// ---- SMTP (okulun e-posta sunucusu vb.) ----

type smtpMailer struct {
	host, port, user, pass string
	from                   mailFrom
}

func (s *smtpMailer) Kind() string { return "smtp" }

func (s *smtpMailer) Send(ctx context.Context, m Mail) error {
	boundary := fmt.Sprintf("unitech-%d", time.Now().UnixNano())
	var msg bytes.Buffer
	fmt.Fprintf(&msg, "From: %s <%s>\r\n", mime.QEncoding.Encode("utf-8", s.from.Name), s.from.Email)
	fmt.Fprintf(&msg, "To: %s\r\n", m.To)
	fmt.Fprintf(&msg, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", m.Subject))
	fmt.Fprintf(&msg, "MIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=%s\r\n\r\n", boundary)
	fmt.Fprintf(&msg, "--%s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n", boundary, m.Text)
	fmt.Fprintf(&msg, "--%s\r\nContent-Type: text/html; charset=utf-8\r\n\r\n%s\r\n--%s--\r\n", boundary, m.HTML, boundary)

	addr := net.JoinHostPort(s.host, s.port)
	var auth smtp.Auth
	if s.user != "" {
		auth = smtp.PlainAuth("", s.user, s.pass, s.host)
	}
	done := make(chan error, 1)
	go func() {
		if s.port == "465" { // doğrudan TLS
			conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: s.host})
			if err != nil {
				done <- err
				return
			}
			c, err := smtp.NewClient(conn, s.host)
			if err != nil {
				done <- err
				return
			}
			defer c.Close()
			if auth != nil {
				if err := c.Auth(auth); err != nil {
					done <- err
					return
				}
			}
			done <- sendVia(c, s.from.Email, m.To, msg.Bytes())
			return
		}
		done <- smtp.SendMail(addr, auth, s.from.Email, []string{m.To}, msg.Bytes()) // STARTTLS destekliyorsa kullanır
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func sendVia(c *smtp.Client, from, to string, msg []byte) error {
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// ---- Geliştirme: e-postaları log'a (ve isteğe bağlı dosyaya) yazar ----

type logMailer struct {
	mu   sync.Mutex
	file string
}

func (l *logMailer) Kind() string { return "log" }

func (l *logMailer) Send(_ context.Context, m Mail) error {
	slog.Info("e-posta (geliştirme modu)", "to", m.To, "subject", m.Subject, "text", m.Text)
	if l.file == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(l.file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	line, _ := json.Marshal(m)
	_, err = f.Write(append(line, '\n'))
	return err
}

// ---- E-posta içerikleri (Türkçe / İngilizce) ----

func codeMail(lang, purpose, name, code string) Mail {
	en := strings.HasPrefix(lang, "en")
	var subject, intro, note string
	switch {
	case purpose == PurposeVerify && en:
		subject, intro = "Your uniTech verification code", "Enter this code to verify your school email address:"
	case purpose == PurposeVerify:
		subject, intro = "uniTech doğrulama kodun", "Okul e-posta adresini doğrulamak için bu kodu gir:"
	case en:
		subject, intro = "Reset your uniTech password", "Enter this code to choose a new password:"
	default:
		subject, intro = "uniTech şifre sıfırlama kodun", "Yeni şifre belirlemek için bu kodu gir:"
	}
	greet := "Merhaba " + name + ","
	note = "Kod 15 dakika geçerlidir. Bu isteği sen yapmadıysan bu e-postayı yok sayabilirsin."
	sign := "uniTech · Abdullah Gül Üniversitesi Teknik Destek"
	if en {
		greet = "Hi " + name + ","
		note = "The code is valid for 15 minutes. If you didn't request this, you can ignore this email."
		sign = "uniTech · Abdullah Gül University Facilities Support"
	}
	text := fmt.Sprintf("%s\n\n%s\n\n    %s\n\n%s\n\n%s\n", greet, intro, code, note, sign)
	h := fmt.Sprintf(`<!doctype html><html><body style="margin:0;background:#f3f6fb;font-family:Segoe UI,Roboto,Arial,sans-serif;color:#0f172a">
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0"><tr><td align="center" style="padding:32px 16px">
<table role="presentation" width="100%%" style="max-width:480px;background:#ffffff;border-radius:14px;overflow:hidden" cellpadding="0" cellspacing="0">
<tr><td style="background:#0b1f3a;color:#ffffff;padding:20px 28px;font-size:18px;font-weight:700">uniTech</td></tr>
<tr><td style="padding:28px">
<p style="margin:0 0 12px">%s</p><p style="margin:0 0 20px;color:#5b6b82">%s</p>
<div style="font-size:32px;font-weight:800;letter-spacing:8px;background:#e8efff;color:#1d4ed8;border-radius:10px;padding:16px;text-align:center">%s</div>
<p style="margin:20px 0 0;color:#5b6b82;font-size:13px">%s</p>
</td></tr>
<tr><td style="padding:16px 28px;border-top:1px solid #e2e8f0;color:#5b6b82;font-size:12px">%s</td></tr>
</table></td></tr></table></body></html>`,
		html.EscapeString(greet), html.EscapeString(intro), code, html.EscapeString(note), html.EscapeString(sign))
	return Mail{Subject: subject, Text: text, HTML: h}
}
