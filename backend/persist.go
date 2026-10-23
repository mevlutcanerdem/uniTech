package main

import (
	"errors"
	"os"
	"path/filepath"
)

// persister, Store'un bellekteki verisini kalıcı bir yere yazar.
// Yerelde JSON dosyası, sunucuda (DATABASE_URL tanımlıysa) PostgreSQL kullanılır.
type persister interface {
	Load() ([]byte, error) // veri yoksa nil, nil
	Save([]byte) error
}

type filePersister struct{ path string }

func newFilePersister(path string) (persister, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return filePersister{path}, nil
}

func (f filePersister) Load() ([]byte, error) {
	b, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

// Save atomik yazar: önce geçici dosya, sonra yeniden adlandırma.
func (f filePersister) Save(b []byte) error {
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}
