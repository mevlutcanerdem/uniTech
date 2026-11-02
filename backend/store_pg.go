package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// PGStore veriyi PostgreSQL tablolarında tutar.
type PGStore struct{ db *pgxpool.Pool }

func NewPGStore(ctx context.Context, url string) (*PGStore, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("DATABASE_URL okunamadı: %w", err)
	}
	cfg.MaxConns = 8
	cfg.MaxConnIdleTime = 5 * time.Minute
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := db.Ping(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("veritabanına bağlanılamadı: %w", err)
	}
	s := &PGStore{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.importLegacy(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("eski veri taşınamadı: %w", err)
	}
	return s, nil
}

func (s *PGStore) Kind() string { return "postgres" }
func (s *PGStore) Close()       { s.db.Close() }

// ---- migration ----

func (s *PGStore) migrate(ctx context.Context) error {
	if _, err := s.db.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		base := strings.TrimPrefix(name, "migrations/")
		v, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration adı geçersiz: %s", base)
		}
		var done bool
		if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, v).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		sqlText, err := migrationFiles.ReadFile(name)
		if err != nil {
			return err
		}
		err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sqlText)); err != nil {
				return fmt.Errorf("%s: %w", base, err)
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, v)
			return err
		})
		if err != nil {
			return err
		}
		slog.Info("migration uygulandı", "dosya", base)
	}
	return nil
}

// importLegacy, önceki sürümün tek satırlık JSON deposunu (unitech_state) tablolara taşır.
func (s *PGStore) importLegacy(ctx context.Context) error {
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT to_regclass('public.unitech_state') IS NOT NULL`).Scan(&exists); err != nil || !exists {
		return err
	}
	var body string
	err := s.db.QueryRow(ctx, `SELECT body::text FROM unitech_state WHERE id = 1`).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = s.db.Exec(ctx, `ALTER TABLE unitech_state RENAME TO unitech_state_imported`)
		return err
	}
	if err != nil {
		return err
	}
	var old struct {
		Users []struct {
			ID        int       `json:"id"`
			Name      string    `json:"name"`
			Email     string    `json:"email"`
			PassHash  string    `json:"pass_hash"`
			Role      string    `json:"role"`
			CreatedAt time.Time `json:"created_at"`
		} `json:"users"`
		Tickets []Ticket `json:"tickets"`
	}
	if err := json.Unmarshal([]byte(body), &old); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			for _, u := range old.Users {
				if _, err := tx.Exec(ctx, `INSERT INTO users (id, name, email, pass_hash, role, verified, created_at)
					VALUES ($1, $2, $3, $4, $5, true, $6)`, u.ID, u.Name, u.Email, u.PassHash, u.Role, u.CreatedAt); err != nil {
					return err
				}
			}
			for _, t := range old.Tickets {
				var assignee *int
				if t.AssigneeID != 0 {
					assignee = &t.AssigneeID
				}
				if _, err := tx.Exec(ctx, `INSERT INTO tickets (id, code, category, building, room, title, description,
					priority, status, reporter_id, assignee_id, created_at, updated_at, resolved_at)
					VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
					t.ID, t.Code, t.Category, t.Building, t.Room, t.Title, t.Description, t.Priority, t.Status,
					t.ReporterID, assignee, t.CreatedAt, t.UpdatedAt, t.ResolvedAt); err != nil {
					return err
				}
				for _, h := range t.History {
					action := h.Action
					if action == "" && h.Note == "Arıza bildirildi" {
						action, h.Note = "created", ""
					}
					if _, err := tx.Exec(ctx, `INSERT INTO ticket_events (ticket_id, at, by_id, status, note, action)
						VALUES ($1,$2,$3,$4,$5,$6)`, t.ID, h.At, h.ByID, h.Status, h.Note, action); err != nil {
						return err
					}
				}
			}
			for _, seq := range []string{"users", "tickets"} {
				if _, err := tx.Exec(ctx, fmt.Sprintf(
					`SELECT setval(pg_get_serial_sequence('%s', 'id'), COALESCE((SELECT max(id) FROM %s), 0) + 1, false)`, seq, seq)); err != nil {
					return err
				}
			}
			slog.Info("eski veri taşındı", "kullanici", len(old.Users), "ariza", len(old.Tickets))
		}
		_, err := tx.Exec(ctx, `ALTER TABLE unitech_state RENAME TO unitech_state_imported`)
		return err
	})
}

// ---- kullanıcılar ----

const userCols = `id, name, email, pass_hash, role, verified, token_version, created_at`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.PassHash, &u.Role, &u.Verified, &u.TokenVersion, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *PGStore) CreateUser(ctx context.Context, u User) (User, error) {
	row := s.db.QueryRow(ctx, `INSERT INTO users (name, email, pass_hash, role, verified)
		VALUES ($1, $2, $3, $4, $5) RETURNING `+userCols, u.Name, u.Email, u.PassHash, u.Role, u.Verified)
	out, err := scanUser(row)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return User{}, ErrEmailTaken
	}
	return out, err
}

func (s *PGStore) UserByEmail(ctx context.Context, email string) (User, error) {
	return scanUser(s.db.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE email = $1`, email))
}

func (s *PGStore) UserByID(ctx context.Context, id int) (User, error) {
	return scanUser(s.db.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1`, id))
}

func (s *PGStore) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.Query(ctx, `SELECT `+userCols+` FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (User, error) { return scanUser(r) })
}

func (s *PGStore) SetRole(ctx context.Context, id int, role string) (User, error) {
	return scanUser(s.db.QueryRow(ctx, `UPDATE users SET role = $2 WHERE id = $1 RETURNING `+userCols, id, role))
}

func (s *PGStore) SetVerified(ctx context.Context, id int) error {
	tag, err := s.db.Exec(ctx, `UPDATE users SET verified = true WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *PGStore) SetPassword(ctx context.Context, id int, hash string) (User, error) {
	return scanUser(s.db.QueryRow(ctx, `UPDATE users SET pass_hash = $2, token_version = token_version + 1, verified = true
		WHERE id = $1 RETURNING `+userCols, id, hash))
}

// ---- kodlar ----

func (s *PGStore) SaveCode(ctx context.Context, c AuthCode) error {
	_, err := s.db.Exec(ctx, `INSERT INTO auth_codes (user_id, purpose, code_hash, expires_at, attempts, sent_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id, purpose) DO UPDATE SET code_hash = EXCLUDED.code_hash,
			expires_at = EXCLUDED.expires_at, attempts = EXCLUDED.attempts, sent_at = EXCLUDED.sent_at`,
		c.UserID, c.Purpose, c.CodeHash, c.ExpiresAt, c.Attempts, c.SentAt)
	return err
}

func (s *PGStore) GetCode(ctx context.Context, userID int, purpose string) (AuthCode, error) {
	c := AuthCode{UserID: userID, Purpose: purpose}
	err := s.db.QueryRow(ctx, `SELECT code_hash, expires_at, attempts, sent_at FROM auth_codes
		WHERE user_id = $1 AND purpose = $2`, userID, purpose).Scan(&c.CodeHash, &c.ExpiresAt, &c.Attempts, &c.SentAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthCode{}, ErrNotFound
	}
	return c, err
}

func (s *PGStore) IncCodeAttempts(ctx context.Context, userID int, purpose string) error {
	_, err := s.db.Exec(ctx, `UPDATE auth_codes SET attempts = attempts + 1 WHERE user_id = $1 AND purpose = $2`, userID, purpose)
	return err
}

func (s *PGStore) DeleteCode(ctx context.Context, userID int, purpose string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM auth_codes WHERE user_id = $1 AND purpose = $2`, userID, purpose)
	return err
}

// ---- arızalar ----

const ticketSelect = `SELECT t.id, t.code, t.category, t.building, t.room, t.title, t.description, t.priority, t.status,
	t.reporter_id, r.name, r.email, COALESCE(t.assignee_id, 0), COALESCE(a.name, ''),
	t.created_at, t.updated_at, t.resolved_at,
	(SELECT count(*) FROM photos p WHERE p.ticket_id = t.id)
	FROM tickets t
	JOIN users r ON r.id = t.reporter_id
	LEFT JOIN users a ON a.id = t.assignee_id`

func scanTicket(row pgx.Row) (Ticket, error) {
	var t Ticket
	err := row.Scan(&t.ID, &t.Code, &t.Category, &t.Building, &t.Room, &t.Title, &t.Description, &t.Priority, &t.Status,
		&t.ReporterID, &t.ReporterName, &t.ReporterEmail, &t.AssigneeID, &t.AssigneeName,
		&t.CreatedAt, &t.UpdatedAt, &t.ResolvedAt, &t.PhotoCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return Ticket{}, ErrNotFound
	}
	return t, err
}

func (s *PGStore) CreateTicket(ctx context.Context, t Ticket) (Ticket, error) {
	var id int
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('tickets', 'id'))`).Scan(&id); err != nil {
			return err
		}
		now := time.Now().UTC()
		if _, err := tx.Exec(ctx, `INSERT INTO tickets (id, code, category, building, room, title, description, priority,
			status, reporter_id, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11)`,
			id, ticketCode(now.Year(), id), t.Category, t.Building, t.Room, t.Title, t.Description, t.Priority,
			StatusNew, t.ReporterID, now); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO ticket_events (ticket_id, at, by_id, status, action) VALUES ($1, $2, $3, $4, 'created')`,
			id, now, t.ReporterID, StatusNew)
		return err
	})
	if err != nil {
		return Ticket{}, err
	}
	return s.TicketByID(ctx, id)
}

func (s *PGStore) TicketByID(ctx context.Context, id int) (Ticket, error) {
	t, err := scanTicket(s.db.QueryRow(ctx, ticketSelect+` WHERE t.id = $1`, id))
	if err != nil {
		return Ticket{}, err
	}
	rows, err := s.db.Query(ctx, `SELECT e.at, e.by_id, u.name, e.status, e.note, e.action
		FROM ticket_events e JOIN users u ON u.id = e.by_id WHERE e.ticket_id = $1 ORDER BY e.at, e.id`, id)
	if err != nil {
		return Ticket{}, err
	}
	t.History, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (HistoryEntry, error) {
		var h HistoryEntry
		return h, r.Scan(&h.At, &h.ByID, &h.ByName, &h.Status, &h.Note, &h.Action)
	})
	if err != nil {
		return Ticket{}, err
	}
	rows, err = s.db.Query(ctx, `SELECT id, width, height FROM photos WHERE ticket_id = $1 ORDER BY id`, id)
	if err != nil {
		return Ticket{}, err
	}
	t.Photos, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (PhotoRef, error) {
		var p PhotoRef
		return p, r.Scan(&p.ID, &p.Width, &p.Height)
	})
	if t.Photos == nil {
		t.Photos = []PhotoRef{}
	}
	return t, err
}

// likeEscape kullanıcı aramasındaki % ve _ karakterlerini düz metin yapar.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (s *PGStore) ListTickets(ctx context.Context, f TicketFilter) ([]Ticket, error) {
	var where []string
	var args []any
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	if f.ReporterID != 0 {
		where = append(where, "t.reporter_id = "+arg(f.ReporterID))
	}
	switch f.Status {
	case "", "hepsi":
	case "acik":
		where = append(where, "t.status <> 'cozuldu'")
	default:
		where = append(where, "t.status = "+arg(f.Status))
	}
	if f.Category != "" {
		where = append(where, "t.category = "+arg(f.Category))
	}
	if f.AssigneeID != 0 {
		where = append(where, "t.assignee_id = "+arg(f.AssigneeID))
	}
	if f.Unassigned {
		where = append(where, "t.assignee_id IS NULL")
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		p := arg("%" + likeEscape(q) + "%")
		where = append(where, fmt.Sprintf("(t.title ILIKE %[1]s OR t.description ILIKE %[1]s OR t.building ILIKE %[1]s OR t.room ILIKE %[1]s OR t.code ILIKE %[1]s)", p))
	}
	sql := ticketSelect
	if len(where) > 0 {
		sql += " WHERE " + strings.Join(where, " AND ")
	}
	order := " ORDER BY (t.status = 'cozuldu')"
	if f.ReporterID == 0 {
		order += ", CASE t.priority WHEN 'acil' THEN 0 WHEN 'orta' THEN 1 ELSE 2 END"
	}
	sql += order + ", t.created_at DESC, t.id DESC"
	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	sql += " LIMIT " + arg(limit)
	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Ticket, error) { return scanTicket(r) })
	if out == nil {
		out = []Ticket{}
	}
	return out, err
}

func (s *PGStore) AllTickets(ctx context.Context) ([]Ticket, error) {
	rows, err := s.db.Query(ctx, ticketSelect)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Ticket, error) { return scanTicket(r) })
}

func (s *PGStore) UpdateTicket(ctx context.Context, id int, by User, status, note, assign string) (Ticket, error) {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		t, err := scanTicket(tx.QueryRow(ctx, ticketSelect+` WHERE t.id = $1 FOR UPDATE OF t`, id))
		if err != nil {
			return err
		}
		add := applyUpdate(&t, by, status, note, assign, time.Now().UTC())
		if len(add) == 0 {
			return nil
		}
		var assignee *int
		if t.AssigneeID != 0 {
			assignee = &t.AssigneeID
		}
		if _, err := tx.Exec(ctx, `UPDATE tickets SET status = $2, assignee_id = $3, updated_at = $4, resolved_at = $5 WHERE id = $1`,
			id, t.Status, assignee, t.UpdatedAt, t.ResolvedAt); err != nil {
			return err
		}
		for _, h := range add {
			if _, err := tx.Exec(ctx, `INSERT INTO ticket_events (ticket_id, at, by_id, status, note, action) VALUES ($1,$2,$3,$4,$5,$6)`,
				id, h.At, h.ByID, h.Status, h.Note, h.Action); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Ticket{}, err
	}
	return s.TicketByID(ctx, id)
}

func (s *PGStore) Stats(ctx context.Context) (Stats, error) {
	var st Stats
	err := s.db.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE status = 'yeni'),
		count(*) FILTER (WHERE status = 'islemde'),
		count(*) FILTER (WHERE status = 'cozuldu'),
		count(*) FILTER (WHERE status <> 'cozuldu' AND priority = 'acil')
		FROM tickets`).Scan(&st.New, &st.Progress, &st.Done, &st.Urgent)
	return st, err
}

// ---- fotoğraflar ----

func (s *PGStore) AddPhoto(ctx context.Context, p Photo, by User) (PhotoRef, error) {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		// Arızayı kilitleyerek aynı anda gelen yüklemelerin sınırı aşmasını engelle.
		var tid int
		if err := tx.QueryRow(ctx, `SELECT id FROM tickets WHERE id = $1 FOR UPDATE`, p.TicketID).Scan(&tid); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM photos WHERE ticket_id = $1`, p.TicketID).Scan(&n); err != nil {
			return err
		}
		if n >= MaxPhotosPerTicket {
			return ErrPhotoLimit
		}
		now := time.Now().UTC()
		if err := tx.QueryRow(ctx, `INSERT INTO photos (ticket_id, uploader_id, content_type, width, height, data, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
			p.TicketID, by.ID, p.ContentType, p.Width, p.Height, p.Data, now).Scan(&p.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO ticket_events (ticket_id, at, by_id, action) VALUES ($1, $2, $3, 'photo')`,
			p.TicketID, now, by.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE tickets SET updated_at = $2 WHERE id = $1`, p.TicketID, now)
		return err
	})
	return p.PhotoRef, err
}

func (s *PGStore) Photo(ctx context.Context, id int) (Photo, error) {
	var p Photo
	err := s.db.QueryRow(ctx, `SELECT id, ticket_id, uploader_id, content_type, width, height, data, created_at
		FROM photos WHERE id = $1`, id).Scan(&p.ID, &p.TicketID, &p.UploaderID, &p.ContentType, &p.Width, &p.Height, &p.Data, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Photo{}, ErrNotFound
	}
	return p, err
}
