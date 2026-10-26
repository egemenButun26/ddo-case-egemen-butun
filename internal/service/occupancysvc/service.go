// Package occupancysvc kapasite, abone rezervasyonu ve doluluk sayaci
// islemlerini tek bir transaction icinde yurutur.
package occupancysvc

import (
	"context"
	"errors"
	"log/slog"
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

// ReserveOptions, rezervasyon isteginin idempotency bilgisini tasir.
type ReserveOptions struct {
	IdempotencyKey string
	RequestHash    [32]byte
}

// Reserve, plaka icin 30 dakikalik aktif rezervasyon olusturur.
func (s *Service) Reserve(ctx context.Context, lotID int64, plateRaw string, now time.Time) (occupancy.Reservation, error) {
	r, _, err := s.ReserveWithOptions(ctx, lotID, plateRaw, now, ReserveOptions{})
	return r, err
}

// ReserveWithOptions, idempotency anahtari verildiginde ayni istegin tekrarini
// ayni rezervasyonla yanitlar. Ikinci donus degeri istegin tekrar olup
// olmadigini soyler.
func (s *Service) ReserveWithOptions(ctx context.Context, lotID int64, plateRaw string, now time.Time,
	opts ReserveOptions,
) (occupancy.Reservation, bool, error) {
	p := plate.Normalize(plateRaw)
	if !plate.ValidTR(p) {
		return occupancy.Reservation{}, false, ErrInvalidPlate
	}
	now = now.Truncate(time.Microsecond)

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return occupancy.Reservation{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if opts.IdempotencyKey != "" {
		onceki, hash, err := store.GetReservationByIdempotencyKey(ctx, tx, opts.IdempotencyKey)
		switch {
		case err == nil:
			if hash != opts.RequestHash {
				return occupancy.Reservation{}, false, store.ErrIdempotencyConflict
			}
			return onceki, true, nil
		case errors.Is(err, store.ErrNotFound):
			// Ilk istek; asagida olusturulacak.
		default:
			return occupancy.Reservation{}, false, err
		}
	}

	capacity, err := store.LockLot(ctx, tx, lotID)
	if err != nil {
		return occupancy.Reservation{}, false, err
	}

	acik, err := store.CountOpenSessions(ctx, tx, lotID, p)
	if err != nil {
		return occupancy.Reservation{}, false, err
	}
	if acik > 0 {
		return occupancy.Reservation{}, false, ErrOpenSessionExists
	}

	aktif, err := store.ListActiveReservations(ctx, tx, lotID)
	if err != nil {
		return occupancy.Reservation{}, false, err
	}
	for _, r := range aktif {
		if r.Plate == p {
			return occupancy.Reservation{}, false, ErrReservationExists
		}
	}

	acikToplam, err := store.CountOpenSessionsForLot(ctx, tx, lotID)
	if err != nil {
		return occupancy.Reservation{}, false, err
	}
	if acikToplam+len(aktif) >= capacity {
		return occupancy.Reservation{}, false, ErrNoCapacity
	}

	var r occupancy.Reservation
	if opts.IdempotencyKey != "" {
		r, err = store.InsertReservationIdempotent(ctx, tx, lotID, p, now, occupancy.ExpiresAt(now),
			opts.IdempotencyKey, opts.RequestHash)
	} else {
		r, err = store.InsertReservation(ctx, tx, lotID, p, now, occupancy.ExpiresAt(now))
	}
	if err != nil {
		if errors.Is(err, store.ErrIdempotencyConflict) && opts.IdempotencyKey != "" {
			// Ayni anahtar eszamanli baska bir istekte kullanildi. Kilidi
			// birakip kazanan istegin sonucunu oku.
			_ = tx.Rollback(ctx)
			onceki, hash, getErr := store.GetReservationByIdempotencyKey(ctx, s.pool, opts.IdempotencyKey)
			if getErr == nil && hash == opts.RequestHash {
				return onceki, true, nil
			}
			return occupancy.Reservation{}, false, store.ErrIdempotencyConflict
		}
		return occupancy.Reservation{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return occupancy.Reservation{}, false, err
	}
	return r, false, nil
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

// RunReleaseLoop, context iptal edilene kadar suresi dolmus rezervasyonlari
// periyodik olarak serbest birakir.
func (s *Service) RunReleaseLoop(ctx context.Context, interval time.Duration, logger *slog.Logger) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	s.runLoop(ctx, interval, func(ctx context.Context) {
		n, err := s.ReleaseExpired(ctx, time.Now())
		if err != nil {
			logger.Error("rezervasyon serbest birakma hatasi", "hata", err)
			return
		}
		if n > 0 {
			logger.Info("suresi dolmus rezervasyonlar serbest birakildi", "sayi", n)
		}
	})
}

// RunDriftLoop, context iptal edilene kadar tum otoparklarda sayac kaymasini
// denetler ve duzeltir.
func (s *Service) RunDriftLoop(ctx context.Context, interval time.Duration, logger *slog.Logger) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	if logger == nil {
		logger = slog.Default()
	}
	s.runLoop(ctx, interval, func(ctx context.Context) {
		lots, err := store.ListLots(ctx, s.pool)
		if err != nil {
			logger.Error("otopark listesi okunamadi", "hata", err)
			return
		}
		now := time.Now()
		for _, lot := range lots {
			if ctx.Err() != nil {
				return
			}
			d, err := s.DetectAndFixDrift(ctx, lot.ID, now)
			if err != nil {
				logger.Error("sayac kaymasi denetimi basarisiz", "lot_id", lot.ID, "hata", err)
				continue
			}
			if d.Drifted {
				logger.Warn("sayac kaymasi duzeltildi", "lot_id", lot.ID,
					"saklanan", d.Stored, "yeniden", d.Recomputed)
			}
		}
	})
}

func (s *Service) runLoop(ctx context.Context, interval time.Duration, fn func(context.Context)) {
	if ctx.Err() != nil {
		return
	}
	fn(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fn(ctx)
		}
	}
}
