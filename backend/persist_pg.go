//go:build postgres

package main

import (
	"database/sql"
	"errors"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const postgresEnabled = true

// pgPersister tüm veriyi tek satırlık bir jsonb belgesi olarak saklar.
// API tek örnek çalıştığı için bellekteki kopya her zaman günceldir.
type pgPersister struct{ db *sql.DB }

func newPGPersister(url string) (persister, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS unitech_state (
		id         int PRIMARY KEY,
		body       jsonb NOT NULL,
		updated_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return nil, err
	}
	return &pgPersister{db}, nil
}

func (p *pgPersister) Load() ([]byte, error) {
	var s string
	err := p.db.QueryRow(`SELECT body::text FROM unitech_state WHERE id = 1`).Scan(&s)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return []byte(s), nil
}

func (p *pgPersister) Save(b []byte) error {
	_, err := p.db.Exec(`INSERT INTO unitech_state (id, body) VALUES (1, $1::jsonb)
		ON CONFLICT (id) DO UPDATE SET body = EXCLUDED.body, updated_at = now()`, string(b))
	return err
}
