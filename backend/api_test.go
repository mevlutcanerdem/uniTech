package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- test altyapısı ----

func TestMain(m *testing.M) {
	if os.Getenv("TEST_VERBOSE") == "" {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	}
	os.Exit(m.Run())
}

// captureMailer gönderilen e-postaları saklar; testler kodu buradan okur.
type captureMailer struct {
	mu   sync.Mutex
	sent []Mail
}

func (c *captureMailer) Kind() string { return "test" }
func (c *captureMailer) Send(_ context.Context, m Mail) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, m)
	return nil
}

func (c *captureMailer) lastCode(t *testing.T, to string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second) // şifremi unuttum e-postası arka planda gider
	for time.Now().Before(deadline) {
		c.mu.Lock()
		for i := len(c.sent) - 1; i >= 0; i-- {
			if c.sent[i].To == to {
				for _, f := range strings.Fields(c.sent[i].Text) {
					if len(f) == 6 && strings.Trim(f, "0123456789") == "" {
						c.mu.Unlock()
						return f
					}
				}
			}
		}
		c.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s için kod e-postası yok", to)
	return ""
}

// stores her test için temiz depolar: bellek ve (TEST_DATABASE_URL varsa) PostgreSQL.
func stores(t *testing.T) map[string]func(t *testing.T) Store {
	out := map[string]func(t *testing.T) Store{
		"memory": func(t *testing.T) Store { s, _ := NewMemStore(""); return s },
	}
	if url := os.Getenv("TEST_DATABASE_URL"); url != "" {
		out["postgres"] = func(t *testing.T) Store {
			ctx := context.Background()
			pg, err := NewPGStore(ctx, url)
			if err != nil {
				t.Fatal(err)
			}
			_, err = pg.db.Exec(ctx, `TRUNCATE users, tickets, ticket_events, photos, auth_codes RESTART IDENTITY CASCADE`)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(pg.Close)
			return pg
		}
	}
	return out
}

type env struct {
	srv  *httptest.Server
	mail *captureMailer
	app  *App
}

func newEnv(t *testing.T, store Store, withMail bool) *env {
	t.Helper()
	e := &env{mail: &captureMailer{}}
	app := &App{store: store, tokens: Tokens{[]byte("test")}, hub: NewHub(), limits: DefaultLimits(),
		adminEmails: map[string]bool{"admin@agu.edu.tr": true}}
	if withMail {
		app.mailer = e.mail
	}
	e.app = app
	e.srv = httptest.NewServer(app.Routes())
	t.Cleanup(e.srv.Close)
	return e
}

func forEachStore(t *testing.T, withMail bool, fn func(t *testing.T, e *env)) {
	for name, mk := range stores(t) {
		t.Run(name, func(t *testing.T) { fn(t, newEnv(t, mk(t), withMail)) })
	}
}

func (e *env) call(t *testing.T, method, path, token string, body any, out any) int {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		_ = json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode
}

func (e *env) register(t *testing.T, email string) authResp {
	t.Helper()
	var r authResp
	if c := e.call(t, "POST", "/api/auth/register", "", map[string]string{"name": "Test Kişi", "email": email, "password": "gizlisifre"}, &r); c != 201 {
		t.Fatalf("kayıt %s: %d", email, c)
	}
	return r
}

func (e *env) ticket(t *testing.T, tok string, fields map[string]string) Ticket {
	t.Helper()
	body := map[string]string{"category": "elektrik", "building": "Kütüphane", "room": "2. kat", "title": "Priz", "description": "Kıvılcım", "priority": "orta"}
	for k, v := range fields {
		body[k] = v
	}
	var tk Ticket
	if c := e.call(t, "POST", "/api/tickets", tok, body, &tk); c != 201 {
		t.Fatalf("arıza oluşturma: %d", c)
	}
	return tk
}

func itoa(i int) string { return fmt.Sprint(i) }

// ---- testler ----

func TestEmailDomainAndLogin(t *testing.T) {
	forEachStore(t, false, func(t *testing.T, e *env) {
		for _, m := range []string{"x@gmail.com", "x@agu.edu.tr.evil.com", "x@sub.agu.edu.tr", "bozuk"} {
			if c := e.call(t, "POST", "/api/auth/register", "", map[string]string{"name": "A", "email": m, "password": "gizlisifre"}, nil); c != 400 {
				t.Errorf("%s kabul edilmemeliydi, kod %d", m, c)
			}
		}
		r := e.register(t, "Ogrenci@AGU.edu.tr")
		if !r.User.Verified || r.VerificationRequired {
			t.Errorf("e-posta kapalıyken hesap doğrulanmış olmalı: %+v", r)
		}
		if c := e.call(t, "POST", "/api/auth/register", "", map[string]string{"name": "A", "email": "ogrenci@agu.edu.tr", "password": "gizlisifre"}, nil); c != 409 {
			t.Errorf("aynı e-posta: %d", c)
		}
		var lr authResp
		if c := e.call(t, "POST", "/api/auth/login", "", map[string]string{"email": "ogrenci@agu.edu.tr", "password": "gizlisifre"}, &lr); c != 200 || lr.Token == "" {
			t.Errorf("giriş: %d", c)
		}
		if c := e.call(t, "POST", "/api/auth/login", "", map[string]string{"email": "ogrenci@agu.edu.tr", "password": "yanlis"}, nil); c != 401 {
			t.Errorf("yanlış şifre: %d", c)
		}
		if c := e.call(t, "POST", "/api/auth/login", "", map[string]string{"email": "yok@agu.edu.tr", "password": "gizlisifre"}, nil); c != 401 {
			t.Errorf("olmayan hesap: %d", c)
		}
	})
}

func TestTicketFlow(t *testing.T) {
	forEachStore(t, false, func(t *testing.T, e *env) {
		stu := e.register(t, "ogr1@agu.edu.tr")
		other := e.register(t, "ogr2@agu.edu.tr")
		adm := e.register(t, "admin@agu.edu.tr")
		if adm.User.Role != RoleAdmin {
			t.Fatalf("admin rolü atanmadı: %s", adm.User.Role)
		}
		tk := e.ticket(t, stu.Token, map[string]string{"priority": "acil"})
		if !strings.HasPrefix(tk.Code, "AGU-") || tk.Status != StatusNew || len(tk.History) != 1 || tk.History[0].Action != "created" {
			t.Fatalf("arıza: %+v", tk)
		}
		if c := e.call(t, "POST", "/api/tickets", stu.Token, map[string]string{"category": "yok", "building": "", "room": "", "title": "", "description": "", "priority": "orta"}, nil); c != 400 {
			t.Errorf("geçersiz arıza: %d", c)
		}
		path := "/api/tickets/" + itoa(tk.ID)
		if c := e.call(t, "GET", path, other.Token, nil, nil); c != 404 {
			t.Errorf("başka öğrenci görebildi: %d", c)
		}
		var list []Ticket
		e.call(t, "GET", "/api/tickets", other.Token, nil, &list)
		if len(list) != 0 {
			t.Errorf("başka öğrenci listede gördü")
		}
		if c := e.call(t, "PATCH", path, stu.Token, map[string]string{"status": "cozuldu"}, nil); c != 403 {
			t.Errorf("öğrenci durumu değiştirebildi: %d", c)
		}
		if c := e.call(t, "PATCH", "/api/users/"+itoa(other.User.ID), adm.Token, map[string]string{"role": "staff"}, nil); c != 200 {
			t.Fatalf("rol değiştirme: %d", c)
		}
		var up Ticket
		if c := e.call(t, "PATCH", path, other.Token, map[string]string{"status": "islemde", "note": "Yoldayız"}, &up); c != 200 ||
			up.Status != StatusProgress || len(up.History) != 3 || up.AssigneeID != other.User.ID || up.AssigneeName != "Test Kişi" {
			t.Fatalf("ekip güncellemesi: %d %+v", c, up)
		}
		var mine Ticket
		e.call(t, "GET", path, stu.Token, nil, &mine)
		if mine.Status != StatusProgress || mine.History[2].Note != "Yoldayız" {
			t.Errorf("öğrenci güncel durumu görmedi: %+v", mine)
		}
		var st Stats
		e.call(t, "GET", "/api/stats", other.Token, nil, &st)
		if st.Progress != 1 || st.Urgent != 1 {
			t.Errorf("istatistik: %+v", st)
		}
		if c := e.call(t, "GET", "/api/users", other.Token, nil, nil); c != 403 {
			t.Errorf("ekip kullanıcı listesini gördü: %d", c)
		}
		// arama ve sıralama
		e.ticket(t, stu.Token, map[string]string{"title": "Wifi %_ kopuyor", "category": "internet", "priority": "dusuk"})
		var found []Ticket
		e.call(t, "GET", "/api/tickets?q="+"%25_", adm.Token, nil, &found)
		if len(found) != 1 || found[0].Category != "internet" {
			t.Errorf("özel karakterli arama: %+v", found)
		}
		var all []Ticket
		e.call(t, "GET", "/api/tickets?status=hepsi", adm.Token, nil, &all)
		if len(all) != 2 || all[0].Priority != "acil" {
			t.Errorf("sıralama: acil önce olmalı: %+v", all)
		}
	})
}

func TestTicketsAreAnonymous(t *testing.T) {
	forEachStore(t, false, func(t *testing.T, e *env) {
		var r authResp
		e.call(t, "POST", "/api/auth/register", "", map[string]string{"name": "Mevlüt Can Erdem", "email": "mevlut.erdem@agu.edu.tr", "password": "gizlisifre"}, &r)
		adm := e.register(t, "admin@agu.edu.tr")
		e.ticket(t, r.Token, nil)
		var list []Ticket
		e.call(t, "GET", "/api/tickets", adm.Token, nil, &list)
		if len(list) != 1 || list[0].ReporterName != "M*** C*** E***" || list[0].ReporterEmail != "m***.e***@agu.edu.tr" {
			t.Fatalf("ekip tam adı gördü: %+v", list)
		}
		var one Ticket
		e.call(t, "GET", "/api/tickets/"+itoa(list[0].ID), adm.Token, nil, &one)
		if one.History[0].ByName != "M*** C*** E***" {
			t.Fatalf("geçmişte tam ad: %+v", one.History)
		}
	})
}

func TestMask(t *testing.T) {
	if got := maskName("Mevlüt Can Erdem"); got != "M*** C*** E***" {
		t.Errorf("maskName: %q", got)
	}
	if got := maskName("şule  özgür"); got != "Ş*** Ö***" {
		t.Errorf("maskName türkçe: %q", got)
	}
	if got := maskEmail("mevlut.erdem@agu.edu.tr"); got != "m***.e***@agu.edu.tr" {
		t.Errorf("maskEmail: %q", got)
	}
}

func TestAssignAndReport(t *testing.T) {
	forEachStore(t, false, func(t *testing.T, e *env) {
		stu := e.register(t, "ogr@agu.edu.tr")
		adm := e.register(t, "admin@agu.edu.tr")
		a := e.ticket(t, stu.Token, map[string]string{"building": "Ambar"})
		b := e.ticket(t, stu.Token, nil)
		e.ticket(t, stu.Token, map[string]string{"category": "internet", "building": "Ambar"})

		var up Ticket
		if c := e.call(t, "PATCH", "/api/tickets/"+itoa(a.ID), adm.Token, map[string]string{"assign": "me"}, &up); c != 200 || up.AssigneeID != adm.User.ID {
			t.Fatalf("üzerine alma: %d %+v", c, up)
		}
		var mine, free []Ticket
		e.call(t, "GET", "/api/tickets?assignee=me", adm.Token, nil, &mine)
		e.call(t, "GET", "/api/tickets?assignee=none", adm.Token, nil, &free)
		if len(mine) != 1 || mine[0].ID != a.ID || len(free) != 2 {
			t.Fatalf("atama filtreleri: %d %d", len(mine), len(free))
		}
		e.call(t, "PATCH", "/api/tickets/"+itoa(a.ID), adm.Token, map[string]string{"status": "cozuldu"}, &up)
		if up.ResolvedAt == nil {
			t.Fatal("çözüm zamanı yazılmadı")
		}
		e.call(t, "PATCH", "/api/tickets/"+itoa(b.ID), adm.Token, map[string]string{"assign": "none"}, nil)

		var rep Report
		if c := e.call(t, "GET", "/api/reports", adm.Token, nil, &rep); c != 200 {
			t.Fatalf("rapor: %d", c)
		}
		if rep.Total != 3 || rep.Totals.Done != 1 || rep.Unassigned != 2 || rep.ByCategory[0].Key != "elektrik" || rep.ByBuilding[0].Key != "Ambar" || len(rep.Daily) != 14 {
			t.Fatalf("rapor içeriği: %+v", rep)
		}
		if rep.Daily[13].Created != 3 || rep.Daily[13].Resolved != 1 {
			t.Fatalf("günlük sayım: %+v", rep.Daily[13])
		}
		if len(rep.Staff) != 1 || rep.Staff[0].Resolved != 1 {
			t.Fatalf("ekip istatistiği: %+v", rep.Staff)
		}
		if c := e.call(t, "GET", "/api/reports", stu.Token, nil, nil); c != 403 {
			t.Errorf("öğrenci raporu gördü: %d", c)
		}
		var pub PublicStats
		if c := e.call(t, "GET", "/api/public/stats", "", nil, &pub); c != 200 || pub.Total != 3 || pub.Resolved != 1 || pub.Open != 2 {
			t.Errorf("herkese açık istatistik: %d %+v", c, pub)
		}
	})
}

func TestEmailVerification(t *testing.T) {
	forEachStore(t, true, func(t *testing.T, e *env) {
		r := e.register(t, "yeni@agu.edu.tr")
		if r.User.Verified || !r.VerificationRequired || !r.CodeSent {
			t.Fatalf("doğrulama beklenirdi: %+v", r)
		}
		if c := e.call(t, "POST", "/api/tickets", r.Token, map[string]string{"category": "diger", "building": "Ambar", "room": "", "title": "X", "description": "Y", "priority": "orta"}, nil); c != 403 {
			t.Errorf("doğrulanmamış hesap arıza açabildi: %d", c)
		}
		if c := e.call(t, "POST", "/api/auth/verify", r.Token, map[string]string{"code": "000000"}, nil); c != 400 {
			t.Errorf("yanlış kod: %d", c)
		}
		if c := e.call(t, "POST", "/api/auth/verify/resend", r.Token, nil, nil); c != 429 {
			t.Errorf("hemen tekrar gönderim sınırlanmalıydı: %d", c)
		}
		code := e.mail.lastCode(t, "yeni@agu.edu.tr")
		var u User
		if c := e.call(t, "POST", "/api/auth/verify", r.Token, map[string]string{"code": code}, &u); c != 200 || !u.Verified {
			t.Fatalf("doğrulama: %d %+v", c, u)
		}
		e.ticket(t, r.Token, nil)
		// kod tek kullanımlık
		if c := e.call(t, "GET", "/api/me", r.Token, nil, &u); c != 200 || !u.Verified {
			t.Fatalf("me: %d", c)
		}
	})
}

func TestCodeAttemptsLimited(t *testing.T) {
	forEachStore(t, true, func(t *testing.T, e *env) {
		r := e.register(t, "deneme@agu.edu.tr")
		code := e.mail.lastCode(t, "deneme@agu.edu.tr")
		for i := 0; i < maxCodeTry; i++ {
			e.call(t, "POST", "/api/auth/verify", r.Token, map[string]string{"code": "111111"}, nil)
		}
		if c := e.call(t, "POST", "/api/auth/verify", r.Token, map[string]string{"code": code}, nil); c != 400 {
			t.Errorf("deneme hakkı bitince doğru kod da reddedilmeli: %d", c)
		}
	})
}

func TestPasswordReset(t *testing.T) {
	forEachStore(t, true, func(t *testing.T, e *env) {
		r := e.register(t, "unutkan@agu.edu.tr")
		if c := e.call(t, "POST", "/api/auth/forgot", "", map[string]string{"email": "yok@agu.edu.tr"}, nil); c != 200 {
			t.Errorf("olmayan e-posta da aynı yanıtı almalı: %d", c)
		}
		if c := e.call(t, "POST", "/api/auth/forgot", "", map[string]string{"email": "unutkan@agu.edu.tr"}, nil); c != 200 {
			t.Fatalf("şifremi unuttum: %d", c)
		}
		time.Sleep(100 * time.Millisecond)
		code := ""
		e.mail.mu.Lock()
		for _, m := range e.mail.sent {
			if m.To == "unutkan@agu.edu.tr" && strings.Contains(m.Subject, "şifre") {
				for _, f := range strings.Fields(m.Text) {
					if len(f) == 6 && strings.Trim(f, "0123456789") == "" {
						code = f
					}
				}
			}
		}
		e.mail.mu.Unlock()
		if code == "" {
			t.Fatal("sıfırlama kodu gelmedi")
		}
		if c := e.call(t, "POST", "/api/auth/reset", "", map[string]string{"email": "unutkan@agu.edu.tr", "code": "999999", "password": "yenisifre123"}, nil); c != 400 {
			t.Errorf("yanlış kod: %d", c)
		}
		var nr authResp
		if c := e.call(t, "POST", "/api/auth/reset", "", map[string]string{"email": "unutkan@agu.edu.tr", "code": code, "password": "yenisifre123"}, &nr); c != 200 || !nr.User.Verified {
			t.Fatalf("sıfırlama: %d %+v", c, nr)
		}
		if c := e.call(t, "GET", "/api/me", r.Token, nil, nil); c != 401 {
			t.Errorf("eski oturum geçersiz olmalıydı: %d", c)
		}
		if c := e.call(t, "POST", "/api/auth/login", "", map[string]string{"email": "unutkan@agu.edu.tr", "password": "yenisifre123"}, nil); c != 200 {
			t.Errorf("yeni şifreyle giriş: %d", c)
		}
	})
}

// testJPEG EXIF yön etiketli (6 = 90° saat yönü) küçük bir JPEG üretir.
func testJPEG(t *testing.T, w, h int, orientation uint16) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 100, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	if orientation == 0 {
		return b
	}
	// APP1 Exif segmenti (büyük endian TIFF, tek IFD girişi: Orientation)
	tiff := []byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1, 0x01, 0x12, 0, 3, 0, 0, 0, 1, byte(orientation >> 8), byte(orientation), 0, 0, 0, 0, 0, 0}
	payload := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xFF, 0xE1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}
	out := append([]byte{0xFF, 0xD8}, seg...)
	out = append(out, payload...)
	return append(out, b[2:]...)
}

func TestProcessPhoto(t *testing.T) {
	out, w, h, err := ProcessPhoto(testJPEG(t, 40, 20, 6))
	if err != nil || w != 20 || h != 40 {
		t.Fatalf("yön düzeltme: %v %dx%d", err, w, h)
	}
	if jpegOrientation(out) != 1 || bytes.Contains(out, []byte("Exif")) {
		t.Errorf("EXIF silinmeliydi")
	}
	big := image.NewRGBA(image.Rect(0, 0, 3200, 1000))
	var pb bytes.Buffer
	_ = png.Encode(&pb, big)
	_, w, h, err = ProcessPhoto(pb.Bytes())
	if err != nil || w != 1600 || h != 500 {
		t.Fatalf("küçültme: %v %dx%d", err, w, h)
	}
	if _, _, _, err := ProcessPhoto([]byte("GIF89a....")); err != ErrPhotoFormat {
		t.Errorf("biçim kontrolü: %v", err)
	}
}

func (e *env) upload(t *testing.T, tok string, ticketID int, data []byte) int {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("photo", "foto.jpg")
	_, _ = fw.Write(data)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/tickets/"+itoa(ticketID)+"/photos", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func TestPhotos(t *testing.T) {
	forEachStore(t, false, func(t *testing.T, e *env) {
		stu := e.register(t, "foto@agu.edu.tr")
		other := e.register(t, "baska@agu.edu.tr")
		adm := e.register(t, "admin@agu.edu.tr")
		tk := e.ticket(t, stu.Token, nil)
		img := testJPEG(t, 64, 48, 0)
		if c := e.upload(t, other.Token, tk.ID, img); c != 404 {
			t.Errorf("başkasının arızasına yükleyebildi: %d", c)
		}
		for i := 0; i < MaxPhotosPerTicket; i++ {
			if c := e.upload(t, stu.Token, tk.ID, img); c != 201 {
				t.Fatalf("yükleme %d: %d", i, c)
			}
		}
		if c := e.upload(t, stu.Token, tk.ID, img); c != 400 {
			t.Errorf("sınır aşıldı: %d", c)
		}
		if c := e.upload(t, stu.Token, tk.ID, []byte("bu bir resim değil")); c != 400 {
			t.Errorf("geçersiz dosya: %d", c)
		}
		var got Ticket
		e.call(t, "GET", "/api/tickets/"+itoa(tk.ID), adm.Token, nil, &got)
		if len(got.Photos) != 3 || got.PhotoCount != 3 || got.History[len(got.History)-1].Action != "photo" {
			t.Fatalf("fotoğraf listesi: %+v", got)
		}
		get := func(tok string) (int, string) {
			req, _ := http.NewRequest("GET", e.srv.URL+"/api/photos/"+itoa(got.Photos[0].ID), nil)
			req.Header.Set("Authorization", "Bearer "+tok)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			b, _ := io.ReadAll(res.Body)
			return res.StatusCode, http.DetectContentType(b)
		}
		if c, ct := get(stu.Token); c != 200 || ct != "image/jpeg" {
			t.Errorf("sahibi göremedi: %d %s", c, ct)
		}
		if c, _ := get(adm.Token); c != 200 {
			t.Errorf("ekip göremedi: %d", c)
		}
		if c, _ := get(other.Token); c != 404 {
			t.Errorf("başka öğrenci gördü: %d", c)
		}
		var list []Ticket
		e.call(t, "GET", "/api/tickets?scope=mine", stu.Token, nil, &list)
		if len(list) != 1 || list[0].PhotoCount != 3 || list[0].Photos != nil {
			t.Errorf("listede fotoğraf sayısı: %+v", list)
		}
	})
}

func TestRateLimits(t *testing.T) {
	e := newEnv(t, func() Store { s, _ := NewMemStore(""); return s }(), false)
	stu := e.register(t, "hizli@agu.edu.tr")
	for i := 0; i < 10; i++ {
		e.ticket(t, stu.Token, nil)
	}
	if c := e.call(t, "POST", "/api/tickets", stu.Token, map[string]string{"category": "diger", "building": "Ambar", "room": "", "title": "X", "description": "Y", "priority": "orta"}, nil); c != 429 {
		t.Errorf("11. bildirim sınırlanmalıydı: %d", c)
	}
	for i := 0; i < 10; i++ {
		e.call(t, "POST", "/api/auth/login", "", map[string]string{"email": "hizli@agu.edu.tr", "password": "yanlis"}, nil)
	}
	if c := e.call(t, "POST", "/api/auth/login", "", map[string]string{"email": "hizli@agu.edu.tr", "password": "gizlisifre"}, nil); c != 429 {
		t.Errorf("hatalı denemelerden sonra kilitlenmeli: %d", c)
	}
}

func TestHealthAndEvents(t *testing.T) {
	e := newEnv(t, func() Store { s, _ := NewMemStore(""); return s }(), false)
	var h map[string]string
	if c := e.call(t, "GET", "/api/health", "", nil, &h); c != 200 || h["storage"] != "memory" || h["mail"] != "off" {
		t.Fatalf("health: %d %v", c, h)
	}
	adm := e.register(t, "admin@agu.edu.tr")
	stu := e.register(t, "ogr@agu.edu.tr")
	res, err := http.Get(e.srv.URL + "/api/events?token=" + adm.Token)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("akış: %v", err)
	}
	defer res.Body.Close()
	got := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data:") {
				got <- sc.Text()
				return
			}
		}
	}()
	time.Sleep(50 * time.Millisecond)
	e.ticket(t, stu.Token, nil)
	select {
	case line := <-got:
		if !strings.Contains(line, `"type":"created"`) {
			t.Errorf("olay: %s", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canlı olay gelmedi")
	}
}

func TestMemStorePersists(t *testing.T) {
	path := t.TempDir() + "/data.gob"
	s, _ := NewMemStore(path)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, User{Name: "A", Email: "a@agu.edu.tr", PassHash: "x", Role: RoleStudent, Verified: true})
	if _, err := s.CreateTicket(ctx, Ticket{Category: "diger", Building: "Ambar", Title: "T", Description: "D", Priority: "orta", ReporterID: u.ID, ReporterName: u.Name}); err != nil {
		t.Fatal(err)
	}
	s2, err := NewMemStore(path)
	if err != nil {
		t.Fatal(err)
	}
	u2, err := s2.UserByEmail(ctx, "a@agu.edu.tr")
	ts, _ := s2.AllTickets(ctx)
	if err != nil || u2.PassHash != "x" || len(ts) != 1 {
		t.Fatalf("dosyadan okunamadı: %v %+v %d", err, u2, len(ts))
	}
}

func TestLegacyImport(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL yok")
	}
	ctx := context.Background()
	pg, err := NewPGStore(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = pg.db.Exec(ctx, `TRUNCATE users, tickets, ticket_events, photos, auth_codes RESTART IDENTITY CASCADE`)
	_, _ = pg.db.Exec(ctx, `DROP TABLE IF EXISTS unitech_state, unitech_state_imported`)
	hash, _ := HashPassword("eskisifre1")
	old := fmt.Sprintf(`{"next_user_id":3,"next_ticket_id":8,"users":[
		{"id":1,"name":"Eski Öğrenci","email":"eski@agu.edu.tr","pass_hash":%q,"role":"student","created_at":"2026-10-23T08:00:00Z"},
		{"id":2,"name":"Eski Ekip","email":"ekip@agu.edu.tr","pass_hash":%q,"role":"staff","created_at":"2026-10-23T08:00:00Z"}],
		"tickets":[{"id":7,"code":"AGU-2026-00007","category":"internet","building":"Ambar","room":"","title":"Wifi","description":"Yok",
		"priority":"orta","status":"islemde","reporter_id":1,"assignee_id":2,"created_at":"2026-10-23T09:00:00Z","updated_at":"2026-10-23T10:00:00Z",
		"history":[{"at":"2026-10-23T09:00:00Z","by_id":1,"by_name":"Eski Öğrenci","status":"yeni","note":"Arıza bildirildi"},
		{"at":"2026-10-23T10:00:00Z","by_id":2,"by_name":"Eski Ekip","status":"islemde","note":"Bakıyoruz"}]}]}`, hash, hash)
	if _, err := pg.db.Exec(ctx, `CREATE TABLE unitech_state (id int PRIMARY KEY, body jsonb NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.db.Exec(ctx, `INSERT INTO unitech_state VALUES (1, $1::jsonb)`, old); err != nil {
		t.Fatal(err)
	}
	pg.Close()

	pg2, err := NewPGStore(ctx, url) // açılışta taşır
	if err != nil {
		t.Fatal(err)
	}
	defer pg2.Close()
	e := newEnv(t, pg2, false)
	var lr authResp
	if c := e.call(t, "POST", "/api/auth/login", "", map[string]string{"email": "eski@agu.edu.tr", "password": "eskisifre1"}, &lr); c != 200 || !lr.User.Verified {
		t.Fatalf("eski hesapla giriş: %d %+v", c, lr.User)
	}
	var tk Ticket
	if c := e.call(t, "GET", "/api/tickets/7", lr.Token, nil, &tk); c != 200 || tk.AssigneeName != "Eski Ekip" || len(tk.History) != 2 ||
		tk.History[0].Action != "created" || tk.History[1].Note != "Bakıyoruz" {
		t.Fatalf("eski arıza: %d %+v", c, tk)
	}
	// diziler devam etmeli: yeni arıza 8 numaradan başlar
	nt := e.ticket(t, lr.Token, nil)
	if nt.ID != 8 {
		t.Errorf("yeni arıza id: %d", nt.ID)
	}
	var renamed bool
	_ = pg2.db.QueryRow(ctx, `SELECT to_regclass('public.unitech_state_imported') IS NOT NULL`).Scan(&renamed)
	if !renamed {
		t.Error("eski tablo yeniden adlandırılmadı")
	}
}

func TestListLimit(t *testing.T) {
	for name, mk := range stores(t) {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, mk(t), false)
			stu := e.register(t, "ogr@agu.edu.tr")
			for i := 0; i < 5; i++ {
				e.call(t, "POST", "/api/tickets", stu.Token, map[string]string{
					"category": "diger", "building": "Ambar", "room": "", "title": "T", "description": "D", "priority": "orta"}, nil)
			}
			var list []Ticket
			e.call(t, "GET", "/api/tickets?limit=3", stu.Token, nil, &list)
			if len(list) != 3 {
				t.Fatalf("limit=3 ile %d arıza döndü", len(list))
			}
		})
	}
}
