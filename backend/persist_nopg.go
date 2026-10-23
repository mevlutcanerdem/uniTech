//go:build !postgres

package main

import "errors"

const postgresEnabled = false

func newPGPersister(string) (persister, error) {
	return nil, errors.New("bu derleme PostgreSQL desteği içermiyor; `-tags postgres` ile derle")
}
