package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"parkakil/internal/domain/payment"
	"parkakil/internal/domain/tariff"
)

var (
	// ErrIdempotencyConflict: ayni anahtar farkli istek ozetiyle kullanilmis.
	ErrIdempotencyConflict = errors.New("idempotency anahtari farkli istekle kullanilmis")
	// ErrPendingPaymentExists: oturumda zaten bekleyen odeme var.
	ErrPendingPaymentExists = errors.New("oturumda bekleyen odeme var")
	// ErrProviderRefTaken: saglayici referansi baska bir kayda ait.
	ErrProviderRefTaken = errors.New("saglayici referansi zaten kullanilmis")
	// ErrPaymentStateConflict: odeme durumu kosullu guncellemeyle degistirilemedi.
	ErrPaymentStateConflict = errors.New("odeme durumu guncellenemedi")
	// ErrDebtExists: oturum icin zaten borc kaydi var.
	ErrDebtExists = errors.New("oturum icin borc kaydi var")
)

// StoredPayment, veritabanindaki odeme satiridir.
type StoredPayment struct {
	ID             int64
	SessionID      int64
	Payment        payment.Payment
	LateApproval   bool
	IdempotencyKey string
	RequestHash    [32]byte
}

// mapPaymentPgError, odeme/iade/borc kisit ihlallerini sentinel hatalara cevirir.
func mapPaymentPgError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case "payments_one_pending_per_session":
			return ErrPendingPaymentExists
		case "payments_idempotency_key_uniq", "refunds_idempotency_key_uniq":
			return ErrIdempotencyConflict
		case "payments_provider_ref_uniq", "refunds_provider_ref_uniq":
			return ErrProviderRefTaken
		case "debts_session_uniq":
			return ErrDebtExists
		}
	}
	return err
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func requestHashBytes(h [32]byte) []byte {
	out := make([]byte, 32)
	copy(out, h[:])
	return out
}

func paymentColumns() string {
	return `id, session_id, amount_kurus, status, provider_ref, idempotency_key, request_hash,
	        late_approval, requested_at, paid_at, confirmed_at`
}

func scanPayment(row interface {
	Scan(dest ...any) error
}) (StoredPayment, error) {
	var (
		sp        StoredPayment
		amount    tariff.Kurus
		status    string
		hash      []byte
		requested time.Time
		paidAt    *time.Time
		confirmed *time.Time
	)
	if err := row.Scan(
		&sp.ID, &sp.SessionID, &amount, &status, &sp.Payment.ProviderRef, &sp.IdempotencyKey,
		&hash, &sp.LateApproval, &requested, &paidAt, &confirmed,
	); err != nil {
		return StoredPayment{}, err
	}
	sp.Payment.Amount = amount
	sp.Payment.Status = payment.Status(status)
	sp.Payment.RequestedAt = requested.UTC()
	if paidAt != nil {
		sp.Payment.PaidAt = paidAt.UTC()
	}
	if confirmed != nil {
		sp.Payment.ConfirmedAt = confirmed.UTC()
	}
	if len(hash) == 32 {
		copy(sp.RequestHash[:], hash)
	}
	return sp, nil
}

func scanPayments(rows pgx.Rows) ([]StoredPayment, error) {
	defer rows.Close()

	var out []StoredPayment
	for rows.Next() {
		sp, err := scanPayment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// InsertPayment odemeyi yazar ve kimligini doner.
func InsertPayment(ctx context.Context, q Querier, sessionID int64, idempotencyKey string, requestHash [32]byte, p payment.Payment) (int64, error) {
	if sessionID <= 0 {
		return 0, fmt.Errorf("%w: oturum kimligi gecersiz (%d)", payment.ErrInvalidPayment, sessionID)
	}
	if idempotencyKey == "" || len(idempotencyKey) > 255 {
		return 0, fmt.Errorf("%w: idempotency anahtari gecersiz", payment.ErrInvalidPayment)
	}
	if err := p.Validate(); err != nil {
		return 0, err
	}

	var id int64
	err := q.QueryRow(ctx,
		`INSERT INTO payments
		   (session_id, amount_kurus, status, provider_ref, idempotency_key, request_hash,
		    late_approval, requested_at, paid_at, confirmed_at)
		 VALUES ($1, $2, $3, $4, $5, $6, false, $7, $8, $9)
		 RETURNING id`,
		sessionID, p.Amount, string(p.Status), p.ProviderRef, idempotencyKey,
		requestHashBytes(requestHash), p.RequestedAt, nullableTime(p.PaidAt), nullableTime(p.ConfirmedAt),
	).Scan(&id)
	if err != nil {
		return 0, mapPaymentPgError(err)
	}
	return id, nil
}

// GetPaymentByIdempotencyKey anahtarla odemeyi okur.
func GetPaymentByIdempotencyKey(ctx context.Context, q Querier, key string) (StoredPayment, error) {
	row := q.QueryRow(ctx,
		`SELECT `+paymentColumns()+` FROM payments WHERE idempotency_key = $1`, key)
	sp, err := scanPayment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredPayment{}, ErrNotFound
	}
	if err != nil {
		return StoredPayment{}, err
	}
	return sp, nil
}

// GetPaymentByProviderRef saglayici referansiyla odemeyi okur.
func GetPaymentByProviderRef(ctx context.Context, q Querier, ref string) (StoredPayment, error) {
	row := q.QueryRow(ctx,
		`SELECT `+paymentColumns()+` FROM payments WHERE provider_ref = $1`, ref)
	sp, err := scanPayment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredPayment{}, ErrNotFound
	}
	if err != nil {
		return StoredPayment{}, err
	}
	return sp, nil
}

// LockPaymentByProviderRef odemeyi islem boyunca kilitler.
func LockPaymentByProviderRef(ctx context.Context, q Querier, ref string) (StoredPayment, error) {
	row := q.QueryRow(ctx,
		`SELECT `+paymentColumns()+` FROM payments WHERE provider_ref = $1 FOR UPDATE`, ref)
	sp, err := scanPayment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredPayment{}, ErrNotFound
	}
	if err != nil {
		return StoredPayment{}, err
	}
	return sp, nil
}

// LockPaymentByID odemeyi kimligiyle kilitler.
func LockPaymentByID(ctx context.Context, q Querier, id int64) (StoredPayment, error) {
	row := q.QueryRow(ctx,
		`SELECT `+paymentColumns()+` FROM payments WHERE id = $1 FOR UPDATE`, id)
	sp, err := scanPayment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredPayment{}, ErrNotFound
	}
	if err != nil {
		return StoredPayment{}, err
	}
	return sp, nil
}

// ListSessionPayments oturumun odemelerini istek zamani ve kimlik sirasiyla doner.
func ListSessionPayments(ctx context.Context, q Querier, sessionID int64) ([]StoredPayment, error) {
	rows, err := q.Query(ctx,
		`SELECT `+paymentColumns()+`
		 FROM payments WHERE session_id = $1
		 ORDER BY requested_at, id`, sessionID)
	if err != nil {
		return nil, err
	}
	return scanPayments(rows)
}

// UpdatePaymentStatus odemeyi kosullu gunceller. Yalniz domain durum tablosunun
// izin verdigi gecisler uygulanir; kosul tutmazsa ErrPaymentStateConflict.
func UpdatePaymentStatus(ctx context.Context, q Querier, id int64, p payment.Payment, lateApproval bool) error {
	if err := p.Validate(); err != nil {
		return err
	}
	tag, err := q.Exec(ctx,
		`UPDATE payments
		 SET status = $2, paid_at = $3, confirmed_at = $4, late_approval = $5, updated_at = now()
		 WHERE id = $1 AND (
		   ($2 = 'succeeded' AND status IN ('pending', 'timed_out'))
		   OR ($2 = 'failed' AND status IN ('pending', 'timed_out'))
		   OR ($2 = 'timed_out' AND status = 'pending')
		 )`,
		id, string(p.Status), nullableTime(p.PaidAt), nullableTime(p.ConfirmedAt), lateApproval,
	)
	if err != nil {
		return mapPaymentPgError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPaymentStateConflict
	}
	return nil
}

// ListExpiredPending, istek zamani esik ya da oncesindeki bekleyen odemeleri
// kilitler (FOR UPDATE SKIP LOCKED) ve kimlik sirasiyla doner.
func ListExpiredPending(ctx context.Context, q Querier, before time.Time, limit int) ([]StoredPayment, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := q.Query(ctx,
		`SELECT `+paymentColumns()+`
		 FROM payments
		 WHERE status = 'pending' AND requested_at <= $1
		 ORDER BY requested_at, id
		 LIMIT $2
		 FOR UPDATE SKIP LOCKED`, before, limit)
	if err != nil {
		return nil, err
	}
	return scanPayments(rows)
}
