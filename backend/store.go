package main

import (
	"context"
	"sort"
	"strings"
	"time"
)

// Store, uygulamanın tüm kalıcı verisine erişim arayüzü.
// Sunucuda PGStore (PostgreSQL), yerel geliştirme ve testlerde MemStore kullanılır.
type Store interface {
	Kind() string
	Close()

	CreateUser(ctx context.Context, u User) (User, error)
	UserByEmail(ctx context.Context, email string) (User, error)
	UserByID(ctx context.Context, id int) (User, error)
	ListUsers(ctx context.Context) ([]User, error)
	SetRole(ctx context.Context, id int, role string) (User, error)
	SetVerified(ctx context.Context, id int) error
	// SetPassword şifreyi değiştirir ve eski oturumları geçersiz kılar.
	SetPassword(ctx context.Context, id int, hash string) (User, error)

	SaveCode(ctx context.Context, c AuthCode) error
	GetCode(ctx context.Context, userID int, purpose string) (AuthCode, error)
	IncCodeAttempts(ctx context.Context, userID int, purpose string) error
	DeleteCode(ctx context.Context, userID int, purpose string) error

	CreateTicket(ctx context.Context, t Ticket) (Ticket, error)
	// TicketByID geçmiş ve fotoğraf listesiyle birlikte döner.
	TicketByID(ctx context.Context, id int) (Ticket, error)
	// ListTickets geçmiş olmadan döner.
	ListTickets(ctx context.Context, f TicketFilter) ([]Ticket, error)
	// UpdateTicket durumu, notu ve atamayı günceller. assign: "" (değiştirme), "me" veya "none".
	UpdateTicket(ctx context.Context, id int, by User, status, note, assign string) (Ticket, error)
	Stats(ctx context.Context) (Stats, error)
	// AllTickets raporlar için geçmişsiz tüm arızalar.
	AllTickets(ctx context.Context) ([]Ticket, error)

	AddPhoto(ctx context.Context, p Photo, by User) (PhotoRef, error)
	Photo(ctx context.Context, id int) (Photo, error)
}

// applyUpdate, iki depo uygulamasının paylaştığı güncelleme kuralı.
// Ticket'ı yerinde değiştirir ve eklenecek geçmiş kayıtlarını döner (boşsa değişiklik yok).
func applyUpdate(t *Ticket, by User, status, note, assign string, now time.Time) []HistoryEntry {
	var add []HistoryEntry
	entry := func(e HistoryEntry) { e.At, e.ByID, e.ByName = now, by.ID, by.Name; add = append(add, e) }

	// İşleme alınan ve kimsede olmayan arıza otomatik olarak işlemi yapana atanır.
	if status == StatusProgress && t.AssigneeID == 0 && assign == "" {
		assign = "me"
	}
	switch assign {
	case "me":
		if t.AssigneeID != by.ID {
			t.AssigneeID, t.AssigneeName = by.ID, by.Name
			entry(HistoryEntry{Action: "assigned"})
		}
	case "none":
		if t.AssigneeID != 0 {
			t.AssigneeID, t.AssigneeName = 0, ""
			entry(HistoryEntry{Action: "unassigned"})
		}
	}
	if status != "" && status != t.Status {
		t.Status = status
		if status == StatusDone {
			at := now
			t.ResolvedAt = &at
		} else {
			t.ResolvedAt = nil
		}
		entry(HistoryEntry{Status: status, Note: note})
		note = ""
	}
	if note != "" {
		entry(HistoryEntry{Note: note})
	}
	if len(add) > 0 {
		t.UpdatedAt = now
	}
	return add
}

// ticketLess liste sıralaması: açıklar önce, (ekip görünümünde) aciliyet, sonra en yeni.
func ticketLess(a, b Ticket, byPriority bool) bool {
	if (a.Status == StatusDone) != (b.Status == StatusDone) {
		return b.Status == StatusDone
	}
	if byPriority && priorityRank[a.Priority] != priorityRank[b.Priority] {
		return priorityRank[a.Priority] < priorityRank[b.Priority]
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return a.ID > b.ID
}

func sortTickets(ts []Ticket, byPriority bool) {
	sort.SliceStable(ts, func(i, j int) bool { return ticketLess(ts[i], ts[j], byPriority) })
}

func matchesFilter(t Ticket, f TicketFilter) bool {
	if f.ReporterID != 0 && t.ReporterID != f.ReporterID {
		return false
	}
	switch f.Status {
	case "", "hepsi":
	case "acik":
		if t.Status == StatusDone {
			return false
		}
	default:
		if t.Status != f.Status {
			return false
		}
	}
	if f.Category != "" && t.Category != f.Category {
		return false
	}
	if f.AssigneeID != 0 && t.AssigneeID != f.AssigneeID {
		return false
	}
	if f.Unassigned && t.AssigneeID != 0 {
		return false
	}
	if q := strings.ToLower(strings.TrimSpace(f.Query)); q != "" {
		hay := strings.ToLower(t.Title + " " + t.Description + " " + t.Building + " " + t.Room + " " + t.Code)
		if !strings.Contains(hay, q) {
			return false
		}
	}
	return true
}

func statsOf(ts []Ticket) Stats {
	var st Stats
	for _, t := range ts {
		switch t.Status {
		case StatusNew:
			st.New++
		case StatusProgress:
			st.Progress++
		case StatusDone:
			st.Done++
		}
		if t.Status != StatusDone && t.Priority == "acil" {
			st.Urgent++
		}
	}
	return st
}
