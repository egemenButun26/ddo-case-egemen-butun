package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"parkakil/internal/domain/occupancy"
)

var (
	// ErrReservationExists: ayni otoparkta plakanin zaten aktif rezervasyonu var.
	ErrReservationExists = errors.New("plaka icin aktif rezervasyon var")
	// ErrReservationNotActive: rezervasyon yok ya da aktif durumda degil.
	ErrReservationNotActive = errors.New("rezervasyon aktif degil")
)

func mapReservationError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case "reservations_one_active_per_plate":
			return ErrReservationExists
		case "reservations_idempotency_key_uniq":
			return ErrIdempotencyConflict
		}
	}
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanReservation(s rowScanner) (occupancy.Reservation, error) {
	var (
		r      occupancy.Reservation
		status string
		arri   *time.Time
		rele   *time.Time
		sess   *int64
	)
	if err := s.Scan(&r.ID, &r.LotID, &r.Plate, &status, &r.ReservedAt, &r.ExpiresAt,
		&arri, &rele, &sess); err != nil {
		return occupancy.Reservation{}, err
	}
	r.Status = occupancy.Status(status)
	r.ReservedAt = r.ReservedAt.UTC()
	r.ExpiresAt = r.ExpiresAt.UTC()
	if arri != nil {
		u := arri.UTC()
		r.ArrivedAt = &u
	}
	if rele != nil {
		u := rele.UTC()
		r.ReleasedAt = &u
	}
	if sess != nil {
		r.SessionID = *sess
	}
	return r, nil
}

const reservationColumns = `id, lot_id, plate, status, reserved_at, expires_at,
	arrived_at, released_at, session_id`

const reservationIdemColumns = reservationColumns + `, idempotency_key, request_hash`

func scanReservationIdem(s rowScanner) (occupancy.Reservation, string, [32]byte, error) {
	var (
		r      occupancy.Reservation
		status string
		arri   *time.Time
		rele   *time.Time
		sess   *int64
		key    *string
		hash   []byte
	)
	if err := s.Scan(&r.ID, &r.LotID, &r.Plate, &status, &r.ReservedAt, &r.ExpiresAt,
		&arri, &rele, &sess, &key, &hash); err != nil {
		return occupancy.Reservation{}, "", [32]byte{}, err
	}
	r.Status = occupancy.Status(status)
	r.ReservedAt = r.ReservedAt.UTC()
	r.ExpiresAt = r.ExpiresAt.UTC()
	if arri != nil {
		u := arri.UTC()
		r.ArrivedAt = &u
	}
	if rele != nil {
		u := rele.UTC()
		r.ReleasedAt = &u
	}
	if sess != nil {
		r.SessionID = *sess
	}
	var out [32]byte
	if len(hash) == 32 {
		copy(out[:], hash)
	}
	var idem string
	if key != nil {
		idem = *key
	}
	return r, idem, out, nil
}

// InsertReservation aktif rezervasyon yazar. Ayni plakanin aktif rezervasyonu
// varsa ErrReservationExists doner.
func InsertReservation(ctx context.Context, q Querier, lotID int64, plate string, reservedAt, expiresAt time.Time) (occupancy.Reservation, error) {
	return insertReservation(ctx, q, lotID, plate, reservedAt, expiresAt, "", nil)
}

// InsertReservationIdempotent, idempotency anahtari ve istek ozetiyle
// rezervasyon yazar. Ayni anahtar baska bir istekte kullanilmissa
// ErrIdempotencyConflict doner.
func InsertReservationIdempotent(ctx context.Context, q Querier, lotID int64, plate string,
	reservedAt, expiresAt time.Time, key string, hash [32]byte,
) (occupancy.Reservation, error) {
	if key == "" || len(key) > 255 {
		return occupancy.Reservation{}, ErrIdempotencyConflict
	}
	h := make([]byte, 32)
	copy(h, hash[:])
	return insertReservation(ctx, q, lotID, plate, reservedAt, expiresAt, key, h)
}

func insertReservation(ctx context.Context, q Querier, lotID int64, plate string,
	reservedAt, expiresAt time.Time, key string, hash []byte,
) (occupancy.Reservation, error) {
	var keyArg any
	var hashArg any
	if key != "" {
		keyArg = key
		hashArg = hash
	}
	r, err := scanReservation(q.QueryRow(ctx,
		`INSERT INTO "ya-reservations"
		   (lot_id, plate, status, reserved_at, expires_at, idempotency_key, request_hash)
		 VALUES ($1, $2, 'active', $3, $4, $5, $6)
		 RETURNING `+reservationColumns,
		lotID, plate, reservedAt, expiresAt, keyArg, hashArg,
	))
	if err != nil {
		return occupancy.Reservation{}, mapReservationError(err)
	}
	return r, nil
}

// GetReservationByIdempotencyKey, anahtarla rezervasyonu ve istek ozetini doner.
func GetReservationByIdempotencyKey(ctx context.Context, q Querier, key string) (occupancy.Reservation, [32]byte, error) {
	r, _, hash, err := scanReservationIdem(q.QueryRow(ctx,
		`SELECT `+reservationIdemColumns+` FROM "ya-reservations" WHERE idempotency_key = $1`, key,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return occupancy.Reservation{}, [32]byte{}, ErrNotFound
	}
	if err != nil {
		return occupancy.Reservation{}, [32]byte{}, err
	}
	return r, hash, nil
}

// GetReservation, kimligiyle rezervasyonu doner.
func GetReservation(ctx context.Context, q Querier, id int64) (occupancy.Reservation, error) {
	r, err := scanReservation(q.QueryRow(ctx,
		`SELECT `+reservationColumns+` FROM "ya-reservations" WHERE id = $1`, id,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return occupancy.Reservation{}, ErrNotFound
	}
	if err != nil {
		return occupancy.Reservation{}, err
	}
	return r, nil
}

// ListActiveReservations, otoparkin aktif rezervasyonlarini kimlik sirasiyla doner.
func ListActiveReservations(ctx context.Context, q Querier, lotID int64) ([]occupancy.Reservation, error) {
	rows, err := q.Query(ctx,
		`SELECT `+reservationColumns+` FROM "ya-reservations"
		 WHERE lot_id = $1 AND status = 'active' ORDER BY id`,
		lotID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []occupancy.Reservation
	for rows.Next() {
		r, err := scanReservation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// CountActiveReservations, otoparkin aktif rezervasyon sayisini doner.
func CountActiveReservations(ctx context.Context, q Querier, lotID int64) (int, error) {
	var n int
	err := q.QueryRow(ctx,
		`SELECT count(*) FROM "ya-reservations" WHERE lot_id = $1 AND status = 'active'`,
		lotID,
	).Scan(&n)
	return n, err
}

// MarkReservationArrivedByPlate, plakanin aktif rezervasyonunu gelen oturuma baglar.
// Aktif rezervasyon yoksa false doner (hata degildir).
func MarkReservationArrivedByPlate(ctx context.Context, q Querier, lotID int64, plate string, sessionID int64, arrivedAt time.Time) (bool, error) {
	var id int64
	err := q.QueryRow(ctx,
		`UPDATE "ya-reservations"
		 SET status = 'arrived', arrived_at = $3, session_id = $4
		 WHERE lot_id = $1 AND plate = $2 AND status = 'active'
		 RETURNING id`,
		lotID, plate, arrivedAt, sessionID,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, mapReservationError(err)
	}
	return true, nil
}

// CancelReservation, aktif rezervasyonu iptal eder. Yoksa ErrNotFound,
// aktif degilse ErrReservationNotActive doner.
func CancelReservation(ctx context.Context, q Querier, id int64, now time.Time) error {
	tag, err := q.Exec(ctx,
		`UPDATE "ya-reservations" SET status = 'cancelled', released_at = $2
		 WHERE id = $1 AND status = 'active'`,
		id, now,
	)
	if err != nil {
		return mapReservationError(err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	var status string
	err = q.QueryRow(ctx, `SELECT status FROM "ya-reservations" WHERE id = $1`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return ErrReservationNotActive
}

// ReleaseExpiredReservations, suresi dolmus aktif rezervasyonlari serbest birakir
// ve serbest birakilan satir sayisini doner. Satirlar FOR UPDATE SKIP LOCKED ile
// secilir; eszamanli cagrilar ayni satiri iki kez islemez.
func ReleaseExpiredReservations(ctx context.Context, q Querier, now time.Time) (int, error) {
	tag, err := q.Exec(ctx,
		`UPDATE "ya-reservations" SET status = 'released', released_at = $1
		 WHERE id IN (
		   SELECT id FROM "ya-reservations"
		   WHERE status = 'active' AND expires_at <= $1
		   FOR UPDATE SKIP LOCKED
		 )`,
		now,
	)
	if err != nil {
		return 0, mapReservationError(err)
	}
	return int(tag.RowsAffected()), nil
}
