package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"parkakil/internal/domain/tariff"
)

// InsertTariffVersion yeni tarife surumu yazar. Tanim once dogrulanir.
func InsertTariffVersion(ctx context.Context, q Querier, lotID int64, v tariff.Version) error {
	if err := v.Validate(); err != nil {
		return err
	}
	def, err := EncodeTariffDefinition(v)
	if err != nil {
		return err
	}
	var validTo *time.Time
	if !v.ValidTo.IsZero() {
		t := v.ValidTo
		validTo = &t
	}
	_, err = q.Exec(ctx,
		`INSERT INTO "ya-tariff_versions" (id, lot_id, valid_from, valid_to, definition)
		 VALUES ($1, $2, $3, $4, $5)`,
		v.ID, lotID, v.ValidFrom, validTo, def,
	)
	return mapPgError(err)
}

// TariffVersionAt, verilen an icin gecerli tarife surumunu doner.
// [valid_from, valid_to) araligi gecerlidir; yoksa ErrNoTariff.
func TariffVersionAt(ctx context.Context, q Querier, lotID int64, at time.Time) (tariff.Version, error) {
	var (
		id        string
		validFrom time.Time
		validTo   *time.Time
		def       []byte
	)
	err := q.QueryRow(ctx,
		`SELECT id, valid_from, valid_to, definition
		 FROM "ya-tariff_versions"
		 WHERE lot_id = $1 AND valid_from <= $2 AND (valid_to IS NULL OR $2 < valid_to)`,
		lotID, at,
	).Scan(&id, &validFrom, &validTo, &def)
	if errors.Is(err, pgx.ErrNoRows) {
		return tariff.Version{}, ErrNoTariff
	}
	if err != nil {
		return tariff.Version{}, err
	}
	return DecodeTariffDefinition(id, validFrom.UTC(), utcPtr(validTo), def)
}

// GetTariffVersion, kimligiyle tarife surumunu doner.
func GetTariffVersion(ctx context.Context, q Querier, id string) (tariff.Version, error) {
	var (
		validFrom time.Time
		validTo   *time.Time
		def       []byte
	)
	err := q.QueryRow(ctx,
		`SELECT valid_from, valid_to, definition FROM "ya-tariff_versions" WHERE id = $1`,
		id,
	).Scan(&validFrom, &validTo, &def)
	if errors.Is(err, pgx.ErrNoRows) {
		return tariff.Version{}, ErrNotFound
	}
	if err != nil {
		return tariff.Version{}, err
	}
	return DecodeTariffDefinition(id, validFrom.UTC(), utcPtr(validTo), def)
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
