package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"parkakil/internal/domain/session"
	"parkakil/internal/domain/tariff"
)

// StoredSession veritabanindaki oturumu temsil eder.
type StoredSession struct {
	ID              int64
	LotID           int64
	Plate           string
	Status          string
	EntryEventID    int64
	EnteredAt       time.Time
	ExitEventID     int64
	ExitedAt        *time.Time
	TariffVersionID string
	AmountKurus     *tariff.Kurus
}

// InsertSession, acik oturum yazar. Ayni lot+plaka ya da karisiklik anahtari acik oturum varsa
// ErrOpenSessionExists.
func InsertSession(ctx context.Context, q Querier, lotID int64, plate string, entryEventID int64, enteredAt time.Time, tariffVersionID string) (int64, error) {
	var id int64
	err := q.QueryRow(ctx,
		`INSERT INTO sessions (lot_id, plate, status, entry_event_id, entered_at, tariff_version_id)
		 VALUES ($1, $2, 'open', $3, $4, $5)
		 RETURNING id`,
		lotID, plate, entryEventID, enteredAt, tariffVersionID,
	).Scan(&id)
	if err != nil {
		return 0, mapPgError(err)
	}
	return id, nil
}

// GetSession, kimligiyle oturumu doner.
func GetSession(ctx context.Context, q Querier, id int64) (StoredSession, error) {
	var (
		s      StoredSession
		exitID *int64
		exited *time.Time
		amount *tariff.Kurus
	)
	err := q.QueryRow(ctx,
		`SELECT id, lot_id, plate, status, entry_event_id, entered_at,
		        exit_event_id, exited_at, tariff_version_id, amount_kurus
		 FROM sessions WHERE id = $1`,
		id,
	).Scan(&s.ID, &s.LotID, &s.Plate, &s.Status, &s.EntryEventID, &s.EnteredAt,
		&exitID, &exited, &s.TariffVersionID, &amount)
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredSession{}, ErrNotFound
	}
	if err != nil {
		return StoredSession{}, err
	}
	if exitID != nil {
		s.ExitEventID = *exitID
	}
	if exited != nil {
		u := exited.UTC()
		s.ExitedAt = &u
	}
	s.AmountKurus = amount
	s.EnteredAt = s.EnteredAt.UTC()
	return s, nil
}

// ListOpenSessions, otoparkin acik oturumlarini kimlik sirasiyla doner.
func ListOpenSessions(ctx context.Context, q Querier, lotID int64) ([]session.OpenSession, error) {
	rows, err := q.Query(ctx,
		`SELECT id, plate, entered_at FROM sessions
		 WHERE lot_id = $1 AND status = 'open' ORDER BY id`,
		lotID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []session.OpenSession
	for rows.Next() {
		var s session.OpenSession
		if err := rows.Scan(&s.ID, &s.Plate, &s.EnteredAt); err != nil {
			return nil, err
		}
		s.EnteredAt = s.EnteredAt.UTC()
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListRecentClosed, around cevresindeki pencerede kapanan oturumlari doner.
func ListRecentClosed(ctx context.Context, q Querier, lotID int64, around time.Time, window time.Duration) ([]session.ClosedSession, error) {
	rows, err := q.Query(ctx,
		`SELECT id, plate, exited_at FROM sessions
		 WHERE lot_id = $1 AND status = 'closed'
		   AND exited_at BETWEEN $2 AND $3
		 ORDER BY id`,
		lotID, around.Add(-window), around.Add(window),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []session.ClosedSession
	for rows.Next() {
		var s session.ClosedSession
		if err := rows.Scan(&s.ID, &s.Plate, &s.ExitedAt); err != nil {
			return nil, err
		}
		s.ExitedAt = s.ExitedAt.UTC()
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListClosedEndingAfter, at aninda ya da sonrasinda kapanan kapali oturumlari doner.
func ListClosedEndingAfter(ctx context.Context, q Querier, lotID int64, at time.Time) ([]session.ClosedSpan, error) {
	rows, err := q.Query(ctx,
		`SELECT id, plate, entered_at, exited_at FROM sessions
		 WHERE lot_id = $1 AND status = 'closed'
		   AND exited_at >= $2
		 ORDER BY id`,
		lotID, at,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []session.ClosedSpan
	for rows.Next() {
		var s session.ClosedSpan
		if err := rows.Scan(&s.ID, &s.Plate, &s.EnteredAt, &s.ExitedAt); err != nil {
			return nil, err
		}
		s.EnteredAt = s.EnteredAt.UTC()
		s.ExitedAt = s.ExitedAt.UTC()
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// CloseSession, acik oturumu kapatir. Acik degilse ErrSessionNotOpen.
func CloseSession(ctx context.Context, q Querier, id, exitEventID int64, exitedAt time.Time, amount tariff.Kurus, quote []byte) error {
	var quoteArg any
	if quote != nil {
		quoteArg = quote
	}
	tag, err := q.Exec(ctx,
		`UPDATE sessions
		 SET status = 'closed', exit_event_id = $2, exited_at = $3, amount_kurus = $4,
		     quote = $5, updated_at = now()
		 WHERE id = $1 AND status = 'open'`,
		id, exitEventID, exitedAt, amount, quoteArg,
	)
	if err != nil {
		return mapPgError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSessionNotOpen
	}
	return nil
}

// CountOpenSessions, lot+plaka icin acik oturum sayisini doner (testler icin).
func CountOpenSessions(ctx context.Context, q Querier, lotID int64, plate string) (int, error) {
	var n int
	err := q.QueryRow(ctx,
		`SELECT count(*) FROM sessions WHERE lot_id = $1 AND plate = $2 AND status = 'open'`,
		lotID, plate,
	).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}
