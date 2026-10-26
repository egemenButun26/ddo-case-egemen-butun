// Package migrate, goose'u "ya-" onekli surum tablosuyla calistirir.
// Uygulama, test yardimcisi ve testler ayni tabloyu kullanir.
package migrate

import (
	"context"
	"database/sql"
	"io/fs"

	"github.com/pressly/goose/v3"
)

// NewProvider, gomulu migrasyonlari ya-goose_db_version tablosuyla calistirir.
func NewProvider(db *sql.DB, fsys fs.FS) (*goose.Provider, error) {
	return goose.NewProvider("", db, fsys, goose.WithStore(Store{}))
}

// Up, bekleyen migrasyonlari uygular ve son surumu doner.
func Up(ctx context.Context, db *sql.DB, fsys fs.FS) (int64, error) {
	provider, err := NewProvider(db, fsys)
	if err != nil {
		return -1, err
	}
	if _, err := provider.Up(ctx); err != nil {
		return -1, err
	}
	return provider.GetDBVersion(ctx)
}

// DownTo, belirtilen surume kadar geri alir.
func DownTo(ctx context.Context, db *sql.DB, fsys fs.FS, version int64) error {
	provider, err := NewProvider(db, fsys)
	if err != nil {
		return err
	}
	_, err = provider.DownTo(ctx, version)
	return err
}

// GetDBVersion, surum tablosundaki son uygulanan surumu doner.
func GetDBVersion(ctx context.Context, db *sql.DB, fsys fs.FS) (int64, error) {
	provider, err := NewProvider(db, fsys)
	if err != nil {
		return -1, err
	}
	return provider.GetDBVersion(ctx)
}
