// Package occupancysvc kapasite, abone rezervasyonu ve doluluk sayaci
// islemlerini tek bir transaction icinde yurutur.
package occupancysvc

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"parkakil/internal/domain/occupancy"
	"parkakil/internal/domain/plate"
	"parkakil/internal/domain/session"
	"parkakil/internal/store"
)

var (
	// ErrInvalidPlate, plaka normalize edildikten sonra TR bicimine uymuyorsa doner.
	ErrInvalidPlate = errors.New("gecersiz plaka")
	// ErrNoCapacity, otoparkta yeni rezervasyon icin yer yoksa doner.
	ErrNoCapacity = errors.New("otoparkta yer yok")
	// ErrReservationExists, plakanin aktif rezervasyonu varsa doner.
	ErrReservationExists = store.ErrReservationExists
	// ErrOpenSessionExists, plakanin acik oturumu varsa doner.
	ErrOpenSessionExists = store.ErrOpenSessionExists
)

// Service rezervasyon ve doluluk islemlerini yurutur.
type Service struct {
	pool *pgxpool.Pool
}

// Snapshot bir otoparkin doluluk gorunumudur.
type Snapshot struct {
	Capacity           int
	Occupied           int
	ActiveReservations int
	Available          int
}

// Drift, sayac kaymasi tespitinin sonucudur.
type Drift struct {
	Stored     int
	Recomputed int
	Delta      int
	Drifted    bool
}

// New servisi olusturur.
func New(pool *pgxpool.Pool) (*Service, error) {
	if pool == nil {
		return nil, errors.New("occupancysvc: veritabani havuzu nil")
	}
	return &Service{pool: pool}, nil
}

// Reserve, plaka icin 30 dakikalik aktif rezervasyon olusturur.
func (s *Service) Reserve(ctx context.Context, lotID int64, plateRaw string, now time.Time) (occupancy.Reservation, error) {
	p := plate.Normalize(plateRaw)
	if !plate.ValidTR(p) {
		return occupancy.Reservation{}, ErrInvalidPlate
	}
	now = now.Truncate(time.Microsecond)

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return occupancy.Reservation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	capacity, err := store.LockLot(ctx, tx, lotID)
	if err != nil {
		return occupancy.Reservation{}, err
	}

	acik, err := store.CountOpenSessions(ctx, tx, lotID, p)
	if err != nil {
		return occupancy.Reservation{}, err
	}
	if acik > 0 {
		return occupancy.Reservation{}, ErrOpenSessionExists
	}

	aktif, err := store.ListActiveReservations(ctx, tx, lotID)
	if err != nil {
		return occupancy.Reservation{}, err
	}
	for _, r := range aktif {
		if r.Plate == p {
			return occupancy.Reservation{}, ErrReservationExists
		}
	}

	acikToplam, err := store.CountOpenSessionsForLot(ctx, tx, lotID)
	if err != nil {
		return occupancy.Reservation{}, err
	}
	if acikToplam+len(aktif) >= capacity {
		return occupancy.Reservation{}, ErrNoCapacity
	}

	r, err := store.InsertReservation(ctx, tx, lotID, p, now, occupancy.ExpiresAt(now))
	if err != nil {
		return occupancy.Reservation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return occupancy.Reservation{}, err
	}
	return r, nil
}

// Cancel, aktif rezervasyonu iptal eder.
func (s *Service) Cancel(ctx context.Context, id int64, now time.Time) error {
	return store.CancelReservation(ctx, s.pool, id, now.Truncate(time.Microsecond))
}

// ReleaseExpired, suresi dolmus aktif rezervasyonlari serbest birakir.
func (s *Service) ReleaseExpired(ctx context.Context, now time.Time) (int, error) {
	return store.ReleaseExpiredReservations(ctx, s.pool, now.Truncate(time.Microsecond))
}

func recompute(ctx context.Context, q store.Querier, lotID int64) (stored, fromEvents, fromSessions int, err error) {
	stored, err = store.GetCounter(ctx, q, lotID)
	if err != nil {
		return 0, 0, 0, err
	}
	opened, err := store.CountEventsByOutcome(ctx, q, lotID, session.OutcomeOpened)
	if err != nil {
		return 0, 0, 0, err
	}
	closed, err := store.CountEventsByOutcome(ctx, q, lotID, session.OutcomeClosed)
	if err != nil {
		return 0, 0, 0, err
	}
	fromSessions, err = store.CountOpenSessionsForLot(ctx, q, lotID)
	if err != nil {
		return 0, 0, 0, err
	}
	return stored, opened - closed, fromSessions, nil
}

// Recompute, saklanan sayaci olaylardan ve acik oturumlardan yeniden hesaplar.
func (s *Service) Recompute(ctx context.Context, lotID int64) (stored, fromEvents, fromSessions int, err error) {
	return recompute(ctx, s.pool, lotID)
}

// DetectAndFixDrift, sayaci olaylardan hesaplanan degerle karsilastirir; farkliysa
// counter_drifts kaydi yazar ve sayaci duzeltir.
func (s *Service) DetectAndFixDrift(ctx context.Context, lotID int64, now time.Time) (Drift, error) {
	now = now.Truncate(time.Microsecond)

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Drift{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := store.LockLot(ctx, tx, lotID); err != nil {
		return Drift{}, err
	}

	stored, fromEvents, _, err := recompute(ctx, tx, lotID)
	if err != nil {
		return Drift{}, err
	}
	delta, drifted := occupancy.Drift(stored, fromEvents)
	if drifted {
		if _, err := store.InsertCounterDrift(ctx, tx, lotID, stored, fromEvents, now, true); err != nil {
			return Drift{}, err
		}
		if err := store.SetCounter(ctx, tx, lotID, fromEvents); err != nil {
			return Drift{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Drift{}, err
	}
	return Drift{Stored: stored, Recomputed: fromEvents, Delta: delta, Drifted: drifted}, nil
}

// Occupancy, otoparkin anlik doluluk gorunumunu doner.
func (s *Service) Occupancy(ctx context.Context, lotID int64) (Snapshot, error) {
	capacity, err := store.LockLot(ctx, s.pool, lotID)
	if err != nil {
		return Snapshot{}, err
	}
	occupied, err := store.GetCounter(ctx, s.pool, lotID)
	if err != nil {
		return Snapshot{}, err
	}
	aktif, err := store.CountActiveReservations(ctx, s.pool, lotID)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		Capacity:           capacity,
		Occupied:           occupied,
		ActiveReservations: aktif,
		Available:          occupancy.Available(capacity, occupied, aktif),
	}, nil
}
