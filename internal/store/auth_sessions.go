package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type AuthSession struct {
	ID        int64
	UserID    int64
	TokenHash []byte
	CreatedAt time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

func CreateAuthSession(ctx context.Context, q Querier, userID int64, tokenHash []byte, expiresAt time.Time) (int64, error) {
	var id int64
	err := q.QueryRow(ctx,
		`INSERT INTO "ya-auth_sessions" (user_id, token_hash, expires_at)
		 VALUES ($1, $2, $3) RETURNING id`,
		userID, tokenHash, expiresAt,
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func GetAuthSessionByTokenHash(ctx context.Context, q Querier, tokenHash []byte) (AuthSession, error) {
	var (
		s         AuthSession
		revokedAt *time.Time
	)
	err := q.QueryRow(ctx,
		`SELECT id, user_id, token_hash, created_at, expires_at, revoked_at
		 FROM "ya-auth_sessions"
		 WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now()`,
		tokenHash,
	).Scan(&s.ID, &s.UserID, &s.TokenHash, &s.CreatedAt, &s.ExpiresAt, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthSession{}, ErrNotFound
	}
	if err != nil {
		return AuthSession{}, err
	}
	s.RevokedAt = revokedAt
	return s, nil
}

func RevokeAuthSession(ctx context.Context, q Querier, id int64) error {
	_, err := q.Exec(ctx,
		`UPDATE "ya-auth_sessions" SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`,
		id,
	)
	return err
}
