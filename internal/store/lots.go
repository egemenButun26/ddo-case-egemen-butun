package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// CreateLot yeni otopark kaydi olusturur ve kimligini doner.
func CreateLot(ctx context.Context, q Querier, code, name string, capacity int) (int64, error) {
	var id int64
	err := q.QueryRow(ctx,
		`INSERT INTO lots (code, name, capacity) VALUES ($1, $2, $3) RETURNING id`,
		code, name, capacity,
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// LockLot, otopark satirini islem boyunca kilitler. Yoksa ErrLotNotFound doner.
func LockLot(ctx context.Context, q Querier, lotID int64) error {
	var id int64
	err := q.QueryRow(ctx, `SELECT id FROM lots WHERE id = $1 FOR NO KEY UPDATE`, lotID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrLotNotFound
	}
	if err != nil {
		return err
	}
	return nil
}
