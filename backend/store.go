package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Roller
const (
	RoleStudent = "student"
	RoleStaff   = "staff"
	RoleAdmin   = "admin"
)

// Arıza durumları
const (
	StatusNew      = "yeni"
	StatusProgress = "islemde"
	StatusDone     = "cozuldu"
)

var (
	Categories = map[string]string{
		"elektrik":   "Elektrik",
		"bilgisayar": "Bilgisayar / Projeksiyon",
		"internet":   "İnternet / Wi-Fi",
		"tesisat":    "Su / Tesisat",
		"iklim":      "Isıtma / Klima",
		"kapi":       "Kapı / Mobilya",
		"temizlik":   "Temizlik",
		"diger":      "Diğer",
	}
	Priorities = map[string]string{"dusuk": "Düşük", "orta": "Orta", "acil": "Acil"}
	Statuses   = map[string]string{StatusNew: "Yeni", StatusProgress: "İşlemde", StatusDone: "Çözüldü"}
	Roles      = map[string]bool{RoleStudent: true, RoleStaff: true, RoleAdmin: true}
)

// ticketCode öğrenciye verilen takip numarası: AGU-2026-00042
func ticketCode(year, id int) string { return fmt.Sprintf("AGU-%d-%05d", year, id) }

var ErrNotFound = errors.New("bulunamadı")
var ErrEmailTaken = errors.New("bu e-posta ile zaten bir hesap var")

type User struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	PassHash  string    `json:"pass_hash,omitempty"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// Public, şifre özeti olmadan.
func (u User) Public() User { u.PassHash = ""; return u }

func (u User) IsStaff() bool { return u.Role == RoleStaff || u.Role == RoleAdmin }

type HistoryEntry struct {
	At     time.Time `json:"at"`
	ByID   int       `json:"by_id"`
	ByName string    `json:"by_name"`
	Status string    `json:"status,omitempty"`
	Note   string    `json:"note,omitempty"`
}

type Ticket struct {
	ID            int            `json:"id"`
	Code          string         `json:"code"`
	Category      string         `json:"category"`
	Building      string         `json:"building"`
	Room          string         `json:"room"`
	Title         string         `json:"title"`
	Description   string         `json:"description"`
	Priority      string         `json:"priority"`
	Status        string         `json:"status"`
	ReporterID    int            `json:"reporter_id"`
	ReporterName  string         `json:"reporter_name"`
	ReporterEmail string         `json:"reporter_email"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	History       []HistoryEntry `json:"history"`
}

type data struct {
	NextUserID   int      `json:"next_user_id"`
	NextTicketID int      `json:"next_ticket_id"`
	Users        []User   `json:"users"`
	Tickets      []Ticket `json:"tickets"`
}

// Store tüm veriyi bellekte tutar ve her değişiklikte persister'a yazar
// (yerelde JSON dosyası, sunucuda PostgreSQL). Bir kampüs için yeterlidir.
type Store struct {
	mu sync.RWMutex
	p  persister
	d  data
}

func OpenStore(p persister) (*Store, error) {
	s := &Store{p: p, d: data{NextUserID: 1, NextTicketID: 1}}
	b, err := p.Load()
	if err != nil {
		return nil, err
	}
	if b == nil {
		return s, s.saveLocked()
	}
	if err := json.Unmarshal(b, &s.d); err != nil {
		return nil, err
	}
	return s, nil
}

// saveLocked yazma kilidi alınmışken çağrılır.
func (s *Store) saveLocked() error {
	b, err := json.Marshal(s.d)
	if err != nil {
		return err
	}
	return s.p.Save(b)
}

// ---- Kullanıcılar ----

func (s *Store) CreateUser(name, email, passHash, role string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.d.Users {
		if u.Email == email {
			return User{}, ErrEmailTaken
		}
	}
	u := User{ID: s.d.NextUserID, Name: name, Email: email, PassHash: passHash, Role: role, CreatedAt: time.Now().UTC()}
	s.d.NextUserID++
	s.d.Users = append(s.d.Users, u)
	return u, s.saveLocked()
}

func (s *Store) UserByEmail(email string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.d.Users {
		if u.Email == email {
			return u, nil
		}
	}
	return User{}, ErrNotFound
}

func (s *Store) UserByID(id int) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.d.Users {
		if u.ID == id {
			return u, nil
		}
	}
	return User{}, ErrNotFound
}

func (s *Store) ListUsers() []User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]User, 0, len(s.d.Users))
	for _, u := range s.d.Users {
		out = append(out, u.Public())
	}
	return out
}

func (s *Store) SetRole(id int, role string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Users {
		if s.d.Users[i].ID == id {
			s.d.Users[i].Role = role
			return s.d.Users[i].Public(), s.saveLocked()
		}
	}
	return User{}, ErrNotFound
}

// ---- Arızalar ----

func (s *Store) CreateTicket(t Ticket) (Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	t.ID = s.d.NextTicketID
	s.d.NextTicketID++
	t.Code = ticketCode(now.Year(), t.ID)
	t.Status = StatusNew
	t.CreatedAt, t.UpdatedAt = now, now
	t.History = []HistoryEntry{{At: now, ByID: t.ReporterID, ByName: t.ReporterName, Status: StatusNew, Note: "Arıza bildirildi"}}
	s.d.Tickets = append(s.d.Tickets, t)
	return t, s.saveLocked()
}

func (s *Store) TicketByID(id int) (Ticket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.d.Tickets {
		if t.ID == id {
			return t, nil
		}
	}
	return Ticket{}, ErrNotFound
}

type TicketFilter struct {
	ReporterID int    // 0 = hepsi
	Status     string // "", "acik" veya durum kodu
	Category   string
	Query      string
}

var priorityRank = map[string]int{"acil": 0, "orta": 1, "dusuk": 2}

func (s *Store) ListTickets(f TicketFilter) []Ticket {
	s.mu.RLock()
	defer s.mu.RUnlock()
	q := strings.ToLower(strings.TrimSpace(f.Query))
	out := []Ticket{}
	for _, t := range s.d.Tickets {
		if f.ReporterID != 0 && t.ReporterID != f.ReporterID {
			continue
		}
		switch f.Status {
		case "", "hepsi":
		case "acik":
			if t.Status == StatusDone {
				continue
			}
		default:
			if t.Status != f.Status {
				continue
			}
		}
		if f.Category != "" && t.Category != f.Category {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(t.Title+" "+t.Description+" "+t.Building+" "+t.Room+" "+t.Code), q) {
			continue
		}
		out = append(out, t)
	}
	// Açıklar önce, sonra aciliyet, sonra en yeni.
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Status == StatusDone) != (b.Status == StatusDone) {
			return b.Status == StatusDone
		}
		if f.ReporterID == 0 && priorityRank[a.Priority] != priorityRank[b.Priority] {
			return priorityRank[a.Priority] < priorityRank[b.Priority]
		}
		return a.CreatedAt.After(b.CreatedAt)
	})
	return out
}

func (s *Store) UpdateTicket(id int, by User, status, note string) (Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Tickets {
		t := &s.d.Tickets[i]
		if t.ID != id {
			continue
		}
		now := time.Now().UTC()
		h := HistoryEntry{At: now, ByID: by.ID, ByName: by.Name, Note: note}
		if status != "" && status != t.Status {
			t.Status = status
			h.Status = status
		}
		if h.Status == "" && h.Note == "" {
			return *t, nil
		}
		t.UpdatedAt = now
		t.History = append(t.History, h)
		return *t, s.saveLocked()
	}
	return Ticket{}, ErrNotFound
}

type Stats struct {
	New      int `json:"yeni"`
	Progress int `json:"islemde"`
	Done     int `json:"cozuldu"`
	Urgent   int `json:"acil_acik"`
}

func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var st Stats
	for _, t := range s.d.Tickets {
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
