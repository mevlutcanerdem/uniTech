package main

import (
	"errors"
	"fmt"
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

// Doğrulama kodu amaçları
const (
	PurposeVerify = "verify"
	PurposeReset  = "reset"
)

const MaxPhotosPerTicket = 3

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

var priorityRank = map[string]int{"acil": 0, "orta": 1, "dusuk": 2}

// ticketCode öğrenciye verilen takip numarası: AGU-2026-00042
func ticketCode(year, id int) string { return fmt.Sprintf("AGU-%d-%05d", year, id) }

var (
	ErrNotFound    = errors.New("bulunamadı")
	ErrEmailTaken  = errors.New("bu e-posta ile zaten bir hesap var")
	ErrPhotoLimit  = errors.New("bir arızaya en fazla 3 fotoğraf eklenebilir")
	ErrCodeInvalid = errors.New("kod hatalı veya süresi dolmuş")
)

type User struct {
	ID           int       `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	PassHash     string    `json:"-"`
	Role         string    `json:"role"`
	Verified     bool      `json:"verified"`
	TokenVersion int       `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

func (u User) IsStaff() bool { return u.Role == RoleStaff || u.Role == RoleAdmin }

type HistoryEntry struct {
	At     time.Time `json:"at"`
	ByID   int       `json:"by_id"`
	ByName string    `json:"by_name"`
	Status string    `json:"status,omitempty"`
	Note   string    `json:"note,omitempty"`
	// Action sistem olayları için: "created", "assigned", "unassigned", "photo".
	Action string `json:"action,omitempty"`
}

type PhotoRef struct {
	ID     int `json:"id"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type Photo struct {
	PhotoRef
	TicketID    int
	UploaderID  int
	ContentType string
	Data        []byte
	CreatedAt   time.Time
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
	AssigneeID    int            `json:"assignee_id,omitempty"`
	AssigneeName  string         `json:"assignee_name,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	ResolvedAt    *time.Time     `json:"resolved_at,omitempty"`
	PhotoCount    int            `json:"photo_count"`
	Photos        []PhotoRef     `json:"photos,omitempty"`
	History       []HistoryEntry `json:"history,omitempty"`
}

// AuthCode e-posta doğrulama veya şifre sıfırlama için gönderilen tek kullanımlık kod.
type AuthCode struct {
	UserID    int
	Purpose   string
	CodeHash  string
	ExpiresAt time.Time
	Attempts  int
	SentAt    time.Time
}

type TicketFilter struct {
	ReporterID int    // 0 = hepsi
	Status     string // "", "hepsi", "acik" veya durum kodu
	Category   string
	Query      string
	AssigneeID int  // 0 = hepsi
	Unassigned bool // sadece kimsenin üstlenmedikleri
	Limit      int
}

type Stats struct {
	New      int `json:"yeni"`
	Progress int `json:"islemde"`
	Done     int `json:"cozuldu"`
	Urgent   int `json:"acil_acik"`
}
