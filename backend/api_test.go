package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func newTestApp(t *testing.T) *httptest.Server {
	t.Helper()
	fp, _ := newFilePersister(filepath.Join(t.TempDir(), "db.json"))
	store, err := OpenStore(fp)
	if err != nil {
		t.Fatal(err)
	}
	app := &App{store: store, tokens: Tokens{[]byte("test")}, hub: NewHub(), limiter: newLoginLimiter(),
		adminEmails: map[string]bool{"admin@agu.edu.tr": true}}
	srv := httptest.NewServer(app.Routes())
	t.Cleanup(srv.Close)
	return srv
}

func call(t *testing.T, srv *httptest.Server, method, path, token string, body any, out any) int {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, srv.URL+path, &buf)
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

func register(t *testing.T, srv *httptest.Server, email string) authResp {
	var r authResp
	if c := call(t, srv, "POST", "/api/auth/register", "", map[string]string{"name": "Test Kişi", "email": email, "password": "gizlisifre"}, &r); c != 201 {
		t.Fatalf("kayıt %s: %d", email, c)
	}
	return r
}

func TestEmailDomain(t *testing.T) {
	srv := newTestApp(t)
	for _, e := range []string{"x@gmail.com", "x@agu.edu.tr.evil.com", "x@sub.agu.edu.tr", "bozuk"} {
		if c := call(t, srv, "POST", "/api/auth/register", "", map[string]string{"name": "A", "email": e, "password": "gizlisifre"}, nil); c != 400 {
			t.Errorf("%s kabul edilmemeliydi, kod %d", e, c)
		}
	}
	register(t, srv, "Ogrenci@AGU.edu.tr")
	if c := call(t, srv, "POST", "/api/auth/register", "", map[string]string{"name": "A", "email": "ogrenci@agu.edu.tr", "password": "gizlisifre"}, nil); c != 409 {
		t.Errorf("aynı e-posta: %d", c)
	}
	var lr authResp
	if c := call(t, srv, "POST", "/api/auth/login", "", map[string]string{"email": "ogrenci@agu.edu.tr", "password": "gizlisifre"}, &lr); c != 200 || lr.Token == "" {
		t.Errorf("giriş: %d", c)
	}
	if c := call(t, srv, "POST", "/api/auth/login", "", map[string]string{"email": "ogrenci@agu.edu.tr", "password": "yanlis"}, nil); c != 401 {
		t.Errorf("yanlış şifre: %d", c)
	}
}

func TestTicketFlow(t *testing.T) {
	srv := newTestApp(t)
	stu := register(t, srv, "ogr1@agu.edu.tr")
	other := register(t, srv, "ogr2@agu.edu.tr")
	adm := register(t, srv, "admin@agu.edu.tr")
	if adm.User.Role != RoleAdmin {
		t.Fatalf("admin rolü atanmadı: %s", adm.User.Role)
	}

	var tk Ticket
	c := call(t, srv, "POST", "/api/tickets", stu.Token, map[string]string{
		"category": "elektrik", "building": "Kütüphane", "room": "2. kat", "title": "Priz", "description": "Kıvılcım", "priority": "acil"}, &tk)
	if c != 201 || tk.Code == "" || tk.Status != StatusNew {
		t.Fatalf("arıza oluşturma: %d %+v", c, tk)
	}
	if c := call(t, srv, "POST", "/api/tickets", stu.Token, map[string]string{"category": "yok", "building": "", "room": "", "title": "", "description": "", "priority": "orta"}, nil); c != 400 {
		t.Errorf("geçersiz arıza: %d", c)
	}
	path := "/api/tickets/" + itoa(tk.ID)
	if c := call(t, srv, "GET", path, other.Token, nil, nil); c != 404 {
		t.Errorf("başka öğrenci görebildi: %d", c)
	}
	var list []Ticket
	call(t, srv, "GET", "/api/tickets", other.Token, nil, &list)
	if len(list) != 0 {
		t.Errorf("başka öğrenci listede gördü")
	}
	if c := call(t, srv, "PATCH", path, stu.Token, map[string]string{"status": "cozuldu"}, nil); c != 403 {
		t.Errorf("öğrenci durumu değiştirebildi: %d", c)
	}
	// Öğrenciyi ekibe al
	if c := call(t, srv, "PATCH", "/api/users/"+itoa(other.User.ID), adm.Token, map[string]string{"role": "staff"}, nil); c != 200 {
		t.Fatalf("rol değiştirme: %d", c)
	}
	var up Ticket
	if c := call(t, srv, "PATCH", path, other.Token, map[string]string{"status": "islemde", "note": "Yoldayız"}, &up); c != 200 || up.Status != StatusProgress || len(up.History) != 2 {
		t.Fatalf("ekip güncellemesi: %d %+v", c, up)
	}
	var mine Ticket
	call(t, srv, "GET", path, stu.Token, nil, &mine)
	if mine.Status != StatusProgress || mine.History[1].Note != "Yoldayız" {
		t.Errorf("öğrenci güncel durumu görmedi: %+v", mine)
	}
	var st Stats
	call(t, srv, "GET", "/api/stats", other.Token, nil, &st)
	if st.Progress != 1 || st.Urgent != 1 {
		t.Errorf("istatistik: %+v", st)
	}
	if c := call(t, srv, "GET", "/api/users", other.Token, nil, nil); c != 403 {
		t.Errorf("ekip kullanıcı listesini gördü: %d", c)
	}
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }
