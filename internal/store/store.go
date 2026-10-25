// Package store, PostgreSQL uzerinde otopark verisini okur ve yazar.
// Fonksiyonlar islem (transaction) baslatmaz; transaction'i cagiran yonetir.
package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	// ErrLotNotFound: otopark kaydi yok.
	ErrLotNotFound = errors.New("otopark bulunamadi")
	// ErrNoTariff: verilen an icin gecerli tarife surumu yok.
	ErrNoTariff = errors.New("gecerli tarife surumu yok")
	// ErrOpenSessionExists: ayni otoparkta plaka icin zaten acik oturum var.
	ErrOpenSessionExists = errors.New("plaka icin acik oturum var")
	// ErrSessionNotOpen: oturum yok ya da zaten kapali.
	ErrSessionNotOpen = errors.New("oturum acik degil")
	// ErrNotFound: istenen kayit bulunamadi.
	ErrNotFound = errors.New("kayit bulunamadi")
	// ErrTariffOverlap: tarife gecerlilik araliklari cakisiyor.
	ErrTariffOverlap = errors.New("tarife gecerlilik araligi cakisiyor")
)

// Querier, pgx.Tx ve *pgxpool.Pool tarafindan karsilanan en kucuk arayuz.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// mapPgError, bilinen kisit hatalarini sentinel hatalara cevirir.
func mapPgError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			if pgErr.ConstraintName == "sessions_one_open_per_plate" {
				return ErrOpenSessionExists
			}
		case "23P01":
			if pgErr.ConstraintName == "tariff_versions_no_overlap" {
				return ErrTariffOverlap
			}
		}
	}
	return err
}

// nullableID: 0 degeri SQL NULL olarak yazilir.
func nullableID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
