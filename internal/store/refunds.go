package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"parkakil/internal/domain/tariff"
)

// StoredRefund, veritabanindaki iade satiridir.
type StoredRefund struct {
	ID             int64
	PaymentID      int64
	Amount         tariff.Kurus
	Status         string
	Reason         string
	IdempotencyKey string
	RequestHash    [32]byte
	ProviderRef    string
	RequestedAt    time.Time
	CompletedAt    *time.Time
}

func scanRefund(row interface {
	Scan(dest ...any) error
}) (StoredRefund, error) {
	var (
		r           StoredRefund
		providerRef *string
		completed   *time.Time
		hash        []byte
	)
	if err := row.Scan(
		&r.ID, &r.PaymentID, &r.Amount, &r.Status, &r.Reason, &r.IdempotencyKey,
		&hash, &providerRef, &r.RequestedAt, &completed,
	); err != nil {
		return StoredRefund{}, err
	}
	if providerRef != nil {
		r.ProviderRef = *providerRef
	}
	if completed != nil {
		t := completed.UTC()
		r.CompletedAt = &t
	}
	r.RequestedAt = r.RequestedAt.UTC()
	if len(hash) == 32 {
		copy(r.RequestHash[:], hash)
	}
	return r, nil
}

func refundColumns() string {
	return `id, payment_id, amount_kurus, status, reason, idempotency_key, request_hash,
	        provider_ref, requested_at, completed_at`
}

// InsertRefund bekleyen iadeyi yazar ve kimligini doner.
func InsertRefund(ctx context.Context, q Querier, paymentID int64, amount tariff.Kurus, reason, idempotencyKey string, requestHash [32]byte, providerRef string, requestedAt time.Time) (int64, error) {
	if paymentID <= 0 {
		return 0, fmt.Errorf("iade: odeme kimligi gecersiz (%d)", paymentID)
	}
	if amount <= 0 {
		return 0, fmt.Errorf("%w: iade tutari pozitif olmali (%d)", tariff.ErrNegativeAmount, amount)
	}
	if reason != "overpaid" && reason != "manual" {
		return 0, fmt.Errorf("iade: gecersiz neden %q", reason)
	}
	if idempotencyKey == "" || len(idempotencyKey) > 255 {
		return 0, errors.New("iade: idempotency anahtari gecersiz")
	}
	if requestedAt.IsZero() {
		return 0, errors.New("iade: istek zamani bos")
	}
	var refArg any
	if providerRef != "" {
		refArg = providerRef
	}

	var id int64
	err := q.QueryRow(ctx,
		`INSERT INTO "ya-refunds"
		   (payment_id, amount_kurus, status, reason, idempotency_key, request_hash,
		    provider_ref, requested_at)
		 VALUES ($1, $2, 'pending', $3, $4, $5, $6, $7)
		 RETURNING id`,
		paymentID, amount, reason, idempotencyKey, requestHashBytes(requestHash), refArg, requestedAt,
	).Scan(&id)
	if err != nil {
		return 0, mapPaymentPgError(err)
	}
	return id, nil
}

// GetRefundByIdempotencyKey anahtarla iadeyi okur.
func GetRefundByIdempotencyKey(ctx context.Context, q Querier, key string) (StoredRefund, error) {
	row := q.QueryRow(ctx,
		`SELECT `+refundColumns()+` FROM "ya-refunds" WHERE idempotency_key = $1`, key)
	r, err := scanRefund(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredRefund{}, ErrNotFound
	}
	if err != nil {
		return StoredRefund{}, err
	}
	return r, nil
}

// SumRefunds odemeye yazilmis iade toplamini doner; basarisiz iadeler sayilmaz.
func SumRefunds(ctx context.Context, q Querier, paymentID int64) (tariff.Kurus, error) {
	var toplam tariff.Kurus
	if err := q.QueryRow(ctx,
		`SELECT COALESCE(sum(amount_kurus), 0) FROM "ya-refunds"
		 WHERE payment_id = $1 AND status <> 'failed'`, paymentID,
	).Scan(&toplam); err != nil {
		return 0, err
	}
	return toplam, nil
}
