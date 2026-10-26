package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"parkakil/internal/domain/plate"
)

var (
	// ErrBlacklistExists: plakanin aktif kara liste kaydi zaten var.
	ErrBlacklistExists = errors.New("plaka zaten kara listede")
	// ErrInvalidBlacklist: plaka ya da neden bos.
	ErrInvalidBlacklist = errors.New("gecersiz kara liste kaydi")
)

// BlacklistEntry, kara listedeki bir plakayi temsil eder.
type BlacklistEntry struct {
	ID        int64
	Plate     string
	Reason    string
	Active    bool
	CreatedBy *int64
	CreatedAt time.Time
	RemovedAt *time.Time
}

const blacklistColumns = `id, plate, reason, active, created_by, created_at, removed_at`

func mapBlacklistError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
		pgErr.ConstraintName == "blacklist_active_plate_uniq" {
		return ErrBlacklistExists
	}
	return err
}

func scanBlacklist(s rowScanner) (BlacklistEntry, error) {
	var (
		e       BlacklistEntry
		removed *time.Time
	)
	if err := s.Scan(&e.ID, &e.Plate, &e.Reason, &e.Active, &e.CreatedBy, &e.CreatedAt, &removed); err != nil {
		return BlacklistEntry{}, err
	}
	e.CreatedAt = e.CreatedAt.UTC()
	if removed != nil {
		u := removed.UTC()
		e.RemovedAt = &u
	}
	return e, nil
}

// InsertBlacklist plakayi normalize ederek kara listeye ekler. Plakanin aktif
// kaydi varsa ErrBlacklistExists doner.
func InsertBlacklist(ctx context.Context, q Querier, plateRaw, reason string, createdBy *int64) (BlacklistEntry, error) {
	p := plate.Normalize(plateRaw)
	reason = strings.TrimSpace(reason)
	if p == "" || reason == "" {
		return BlacklistEntry{}, ErrInvalidBlacklist
	}
	e, err := scanBlacklist(q.QueryRow(ctx,
		`INSERT INTO "ya-blacklist" (plate, reason, created_by)
		 VALUES ($1, $2, $3)
		 RETURNING `+blacklistColumns,
		p, reason, createdBy,
	))
	if err != nil {
		return BlacklistEntry{}, mapBlacklistError(err)
	}
	return e, nil
}

// ListBlacklist, kayitlari kimlik sirasiyla doner.
func ListBlacklist(ctx context.Context, q Querier, afterID int64, limit int) ([]BlacklistEntry, error) {
	rows, err := q.Query(ctx,
		`SELECT `+blacklistColumns+` FROM "ya-blacklist"
		 WHERE id > $1 ORDER BY id LIMIT $2`,
		afterID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BlacklistEntry
	for rows.Next() {
		e, err := scanBlacklist(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// DeactivateBlacklist, aktif kara liste kaydini kaldirir. Kayit yoksa ya da
// zaten kaldirilmissa ErrNotFound doner.
func DeactivateBlacklist(ctx context.Context, q Querier, id int64, at time.Time) error {
	tag, err := q.Exec(ctx,
		`UPDATE "ya-blacklist" SET active = false, removed_at = $2
		 WHERE id = $1 AND active`,
		id, at,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// IsPlateBlacklisted, plakanin aktif kara liste kaydi olup olmadigini doner.
func IsPlateBlacklisted(ctx context.Context, q Querier, plateNorm string) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM "ya-blacklist" WHERE plate = $1 AND active)`,
		plateNorm,
	).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return exists, err
}
