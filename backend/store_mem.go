package main

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// MemStore tüm veriyi bellekte tutar. path verilirse her değişiklikte dosyaya yazar.
// Yerel geliştirme ve testler içindir; sunucuda PGStore kullanılır.
type MemStore struct {
	mu   sync.RWMutex
	path string
	d    memState
}

type memState struct {
	NextUserID, NextTicketID, NextPhotoID int
	Users                                 []User
	Tickets                               []Ticket
	Photos                                []Photo
	Codes                                 []AuthCode
}

func NewMemStore(path string) (*MemStore, error) {
	s := &MemStore{path: path, d: memState{NextUserID: 1, NextTicketID: 1, NextPhotoID: 1}}
	if path == "" {
		return s, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := gob.NewDecoder(bytes.NewReader(b)).Decode(&s.d); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *MemStore) Kind() string {
	if s.path != "" {
		return "file"
	}
	return "memory"
}

func (s *MemStore) Close() {}

func (s *MemStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(s.d); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// ---- kullanıcılar ----

func (s *MemStore) userIdx(id int) int {
	return slices.IndexFunc(s.d.Users, func(u User) bool { return u.ID == id })
}

func (s *MemStore) CreateUser(_ context.Context, u User) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.ContainsFunc(s.d.Users, func(x User) bool { return x.Email == u.Email }) {
		return User{}, ErrEmailTaken
	}
	u.ID = s.d.NextUserID
	s.d.NextUserID++
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now().UTC()
	}
	s.d.Users = append(s.d.Users, u)
	return u, s.saveLocked()
}

func (s *MemStore) UserByEmail(_ context.Context, email string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.d.Users {
		if u.Email == email {
			return u, nil
		}
	}
	return User{}, ErrNotFound
}

func (s *MemStore) UserByID(_ context.Context, id int) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if i := s.userIdx(id); i >= 0 {
		return s.d.Users[i], nil
	}
	return User{}, ErrNotFound
}

func (s *MemStore) ListUsers(_ context.Context) ([]User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.d.Users), nil
}

func (s *MemStore) SetRole(_ context.Context, id int, role string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.userIdx(id)
	if i < 0 {
		return User{}, ErrNotFound
	}
	s.d.Users[i].Role = role
	return s.d.Users[i], s.saveLocked()
}

func (s *MemStore) SetVerified(_ context.Context, id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.userIdx(id)
	if i < 0 {
		return ErrNotFound
	}
	s.d.Users[i].Verified = true
	return s.saveLocked()
}

func (s *MemStore) SetPassword(_ context.Context, id int, hash string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.userIdx(id)
	if i < 0 {
		return User{}, ErrNotFound
	}
	s.d.Users[i].PassHash = hash
	s.d.Users[i].TokenVersion++
	s.d.Users[i].Verified = true // kodu e-postadan aldığı için adres de doğrulanmış olur
	return s.d.Users[i], s.saveLocked()
}

// ---- kodlar ----

func (s *MemStore) codeIdx(userID int, purpose string) int {
	return slices.IndexFunc(s.d.Codes, func(c AuthCode) bool { return c.UserID == userID && c.Purpose == purpose })
}

func (s *MemStore) SaveCode(_ context.Context, c AuthCode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.codeIdx(c.UserID, c.Purpose); i >= 0 {
		s.d.Codes[i] = c
	} else {
		s.d.Codes = append(s.d.Codes, c)
	}
	return s.saveLocked()
}

func (s *MemStore) GetCode(_ context.Context, userID int, purpose string) (AuthCode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if i := s.codeIdx(userID, purpose); i >= 0 {
		return s.d.Codes[i], nil
	}
	return AuthCode{}, ErrNotFound
}

func (s *MemStore) IncCodeAttempts(_ context.Context, userID int, purpose string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.codeIdx(userID, purpose); i >= 0 {
		s.d.Codes[i].Attempts++
	}
	return s.saveLocked()
}

func (s *MemStore) DeleteCode(_ context.Context, userID int, purpose string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.codeIdx(userID, purpose); i >= 0 {
		s.d.Codes = slices.Delete(s.d.Codes, i, i+1)
	}
	return s.saveLocked()
}

// ---- arızalar ----

func (s *MemStore) ticketIdx(id int) int {
	return slices.IndexFunc(s.d.Tickets, func(t Ticket) bool { return t.ID == id })
}

func (s *MemStore) CreateTicket(_ context.Context, t Ticket) (Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	t.ID = s.d.NextTicketID
	s.d.NextTicketID++
	t.Code = ticketCode(now.Year(), t.ID)
	t.Status = StatusNew
	t.CreatedAt, t.UpdatedAt = now, now
	t.History = []HistoryEntry{{At: now, ByID: t.ReporterID, ByName: t.ReporterName, Status: StatusNew, Action: "created"}}
	s.d.Tickets = append(s.d.Tickets, t)
	return s.detailLocked(t), s.saveLocked()
}

// detailLocked arızanın kopyasını fotoğraf listesiyle döner.
func (s *MemStore) detailLocked(t Ticket) Ticket {
	t.History = slices.Clone(t.History)
	t.Photos = []PhotoRef{}
	for _, p := range s.d.Photos {
		if p.TicketID == t.ID {
			t.Photos = append(t.Photos, p.PhotoRef)
		}
	}
	t.PhotoCount = len(t.Photos)
	return t
}

func (s *MemStore) summaryLocked(t Ticket) Ticket {
	t.History = nil
	t.Photos = nil
	t.PhotoCount = 0
	for _, p := range s.d.Photos {
		if p.TicketID == t.ID {
			t.PhotoCount++
		}
	}
	return t
}

func (s *MemStore) TicketByID(_ context.Context, id int) (Ticket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if i := s.ticketIdx(id); i >= 0 {
		return s.detailLocked(s.d.Tickets[i]), nil
	}
	return Ticket{}, ErrNotFound
}

func (s *MemStore) ListTickets(_ context.Context, f TicketFilter) ([]Ticket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Ticket{}
	for _, t := range s.d.Tickets {
		if matchesFilter(t, f) {
			out = append(out, s.summaryLocked(t))
		}
	}
	sortTickets(out, f.ReporterID == 0)
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (s *MemStore) AllTickets(_ context.Context) ([]Ticket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Ticket, 0, len(s.d.Tickets))
	for _, t := range s.d.Tickets {
		out = append(out, s.summaryLocked(t))
	}
	return out, nil
}

func (s *MemStore) UpdateTicket(_ context.Context, id int, by User, status, note, assign string) (Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.ticketIdx(id)
	if i < 0 {
		return Ticket{}, ErrNotFound
	}
	t := &s.d.Tickets[i]
	add := applyUpdate(t, by, status, note, assign, time.Now().UTC())
	if len(add) == 0 {
		return s.detailLocked(*t), nil
	}
	t.History = append(t.History, add...)
	return s.detailLocked(*t), s.saveLocked()
}

func (s *MemStore) Stats(_ context.Context) (Stats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return statsOf(s.d.Tickets), nil
}

// ---- fotoğraflar ----

func (s *MemStore) AddPhoto(_ context.Context, p Photo, by User) (PhotoRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.ticketIdx(p.TicketID)
	if i < 0 {
		return PhotoRef{}, ErrNotFound
	}
	n := 0
	for _, x := range s.d.Photos {
		if x.TicketID == p.TicketID {
			n++
		}
	}
	if n >= MaxPhotosPerTicket {
		return PhotoRef{}, ErrPhotoLimit
	}
	now := time.Now().UTC()
	p.ID = s.d.NextPhotoID
	s.d.NextPhotoID++
	p.UploaderID = by.ID
	p.CreatedAt = now
	s.d.Photos = append(s.d.Photos, p)
	t := &s.d.Tickets[i]
	t.History = append(t.History, HistoryEntry{At: now, ByID: by.ID, ByName: by.Name, Action: "photo"})
	t.UpdatedAt = now
	return p.PhotoRef, s.saveLocked()
}

func (s *MemStore) Photo(_ context.Context, id int) (Photo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.d.Photos {
		if p.ID == id {
			return p, nil
		}
	}
	return Photo{}, ErrNotFound
}
