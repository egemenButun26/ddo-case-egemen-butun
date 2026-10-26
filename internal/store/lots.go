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

// LotSummary, otoparkin temel bilgileridir.
type LotSummary struct {
	ID       int64
	Code     string
	Name     string
	Capacity int
}

// ListLots, otoparklari kimlik sirasiyla doner.
func ListLots(ctx context.Context, q Querier) ([]LotSummary, error) {
	rows, err := q.Query(ctx,
		`SELECT id, code, name, capacity FROM lots ORDER BY id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LotSummary
	for rows.Next() {
		var l LotSummary
		if err := rows.Scan(&l.ID, &l.Code, &l.Name, &l.Capacity); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// LockLot, otopark satirini islem boyunca kilitler ve kapasitesini doner.
// Yoksa ErrLotNotFound doner.
func LockLot(ctx context.Context, q Querier, lotID int64) (int, error) {
	var capacity int
	err := q.QueryRow(ctx, `SELECT capacity FROM lots WHERE id = $1 FOR NO KEY UPDATE`, lotID).Scan(&capacity)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrLotNotFound
	}
	if err != nil {
		return 0, err
	}
	return capacity, nil
}
