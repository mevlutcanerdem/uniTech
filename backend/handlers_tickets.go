package main

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func itoaInt(i int) string { return strconv.Itoa(i) }

func (a *App) createTicket(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if !u.IsStaff() {
		if ok, wait := a.limits.Tickets.Allow(itoaInt(u.ID)); !ok {
			tooMany(w, wait, "kısa sürede çok fazla bildirim gönderdin, biraz sonra tekrar dene")
			return
		}
	}
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
		writeErr(w, 400, "eksik veya hatalı alan: "+strings.Join(errs, ", "), "invalid")
		return
	}
	t, err := a.store.CreateTicket(r.Context(), Ticket{
		Category: in.Category, Building: building, Room: room, Title: title, Description: desc,
		Priority: in.Priority, ReporterID: u.ID, ReporterName: u.Name, ReporterEmail: u.Email,
	})
	if err != nil {
		slog.Error("arıza kaydı", "err", err)
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
	switch q.Get("assignee") {
	case "me":
		f.AssigneeID = u.ID
	case "none":
		f.Unassigned = true
	}
	// Öğrenciler sadece kendi bildirimlerini görür; ekip scope=mine ile kendininkini isteyebilir.
	if !u.IsStaff() || !u.Verified || q.Get("scope") == "mine" {
		f.ReporterID = u.ID
	}
	ts, err := a.store.ListTickets(r.Context(), f)
	if err != nil {
		slog.Error("liste", "err", err)
		writeErr(w, 500, "arızalar alınamadı")
		return
	}
	writeJSON(w, 200, anonymizeAll(ts))
}

// visibleTicket arızayı sahibi veya ekip için döner; diğerleri için bulunamadı.
func (a *App) visibleTicket(w http.ResponseWriter, r *http.Request, id int) (Ticket, bool) {
	t, err := a.store.TicketByID(r.Context(), id)
	u := userFrom(r)
	if err != nil || (!(u.IsStaff() && u.Verified) && t.ReporterID != u.ID) {
		if err != nil && !isNotFound(err) {
			slog.Error("arıza", "id", id, "err", err)
		}
		writeErr(w, 404, "arıza bulunamadı", "not_found")
		return Ticket{}, false
	}
	return t, true
}

func (a *App) getTicket(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeErr(w, 404, "arıza bulunamadı", "not_found")
		return
	}
	if t, ok := a.visibleTicket(w, r, id); ok {
		writeJSON(w, 200, anonymize(t))
	}
}

func (a *App) updateTicket(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeErr(w, 404, "arıza bulunamadı", "not_found")
		return
	}
	var in struct{ Status, Note, Assign string }
	if !readJSON(w, r, &in) {
		return
	}
	if _, ok := Statuses[in.Status]; in.Status != "" && !ok {
		writeErr(w, 400, "geçersiz durum")
		return
	}
	if in.Assign != "" && in.Assign != "me" && in.Assign != "none" {
		writeErr(w, 400, "geçersiz atama")
		return
	}
	note, ok := clean(in.Note, 500)
	if !ok {
		writeErr(w, 400, "not en fazla 500 karakter olabilir")
		return
	}
	if in.Status == "" && note == "" && in.Assign == "" {
		writeErr(w, 400, "durum, not veya atama gerekli")
		return
	}
	t, err := a.store.UpdateTicket(r.Context(), id, userFrom(r), in.Status, note, in.Assign)
	if isNotFound(err) {
		writeErr(w, 404, "arıza bulunamadı", "not_found")
		return
	} else if err != nil {
		slog.Error("güncelleme", "id", id, "err", err)
		writeErr(w, 500, "güncellenemedi")
		return
	}
	a.hub.Publish(Event{"updated", t.ID, t.Code, t.Title, t.Status, t.Priority})
	writeJSON(w, 200, anonymize(t))
}

func (a *App) uploadPhoto(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeErr(w, 404, "arıza bulunamadı", "not_found")
		return
	}
	u := userFrom(r)
	t, ok := a.visibleTicket(w, r, id)
	if !ok {
		return
	}
	if len(t.Photos) >= MaxPhotosPerTicket {
		writeErr(w, 400, ErrPhotoLimit.Error(), "photo_limit")
		return
	}
	if ok, wait := a.limits.Photos.Allow(itoaInt(u.ID)); !ok {
		tooMany(w, wait, "çok fazla fotoğraf yükledin, biraz sonra tekrar dene")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPhotoBytes+(1<<20))
	file, _, err := r.FormFile("photo")
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeErr(w, 413, ErrPhotoTooBig.Error(), "photo_too_big")
			return
		}
		writeErr(w, 400, "fotoğraf bulunamadı", "invalid")
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxPhotoBytes+1))
	if err != nil {
		writeErr(w, 400, "fotoğraf okunamadı", "invalid")
		return
	}
	data, pw, ph, err := ProcessPhoto(raw)
	if err != nil {
		code := 400
		if errors.Is(err, ErrPhotoTooBig) {
			code = 413
		}
		writeErr(w, code, err.Error(), "photo_invalid")
		return
	}
	ref, err := a.store.AddPhoto(r.Context(), Photo{PhotoRef: PhotoRef{Width: pw, Height: ph}, TicketID: id,
		ContentType: "image/jpeg", Data: data}, u)
	if errors.Is(err, ErrPhotoLimit) {
		writeErr(w, 400, err.Error(), "photo_limit")
		return
	} else if err != nil {
		slog.Error("fotoğraf kaydı", "err", err)
		writeErr(w, 500, "fotoğraf kaydedilemedi")
		return
	}
	a.hub.Publish(Event{"updated", t.ID, t.Code, t.Title, t.Status, t.Priority})
	writeJSON(w, 201, ref)
}

func (a *App) getPhoto(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeErr(w, 404, "fotoğraf bulunamadı", "not_found")
		return
	}
	p, err := a.store.Photo(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "fotoğraf bulunamadı", "not_found")
		return
	}
	if _, ok := a.visibleTicket(w, r, p.TicketID); !ok {
		return
	}
	w.Header().Set("Content-Type", p.ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(p.Data)))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable") // içerik hiç değişmez
	w.Header().Set("Content-Disposition", "inline")
	_, _ = w.Write(p.Data)
}

func (a *App) stats(w http.ResponseWriter, r *http.Request) {
	st, err := a.store.Stats(r.Context())
	if err != nil {
		writeErr(w, 500, "istatistikler alınamadı")
		return
	}
	writeJSON(w, 200, st)
}

func (a *App) reports(w http.ResponseWriter, r *http.Request) {
	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil || days < 7 || days > 90 {
		days = 14
	}
	ts, err1 := a.store.AllTickets(r.Context())
	us, err2 := a.store.ListUsers(r.Context())
	if err1 != nil || err2 != nil {
		writeErr(w, 500, "rapor hazırlanamadı")
		return
	}
	writeJSON(w, 200, BuildReport(ts, us, days, time.Now()))
}

func (a *App) listUsers(w http.ResponseWriter, r *http.Request) {
	us, err := a.store.ListUsers(r.Context())
	if err != nil {
		writeErr(w, 500, "kullanıcılar alınamadı")
		return
	}
	writeJSON(w, 200, us)
}

func (a *App) setRole(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeErr(w, 404, "kullanıcı bulunamadı", "not_found")
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
	u, err := a.store.SetRole(r.Context(), id, in.Role)
	if err != nil {
		writeErr(w, 404, "kullanıcı bulunamadı", "not_found")
		return
	}
	writeJSON(w, 200, u)
}
