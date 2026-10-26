package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"parkakil/internal/auth"
)

type User struct {
	ID           int64
	Email        string
	Name         string
	Role         auth.Role
	PasswordHash string
	Plate        *string
	Active       bool
	CreatedAt    time.Time
}

type CreateUserParams struct {
	Email        string
	Name         string
	Role         auth.Role
	PasswordHash string
	Plate        *string
	Active       *bool
}

func CreateUser(ctx context.Context, q Querier, p CreateUserParams) (int64, error) {
	active := true
	if p.Active != nil {
		active = *p.Active
	}
	var id int64
	err := q.QueryRow(ctx,
		`INSERT INTO "ya-qo-users" (email, name, role, password_hash, plate, active)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		p.Email, p.Name, string(p.Role), p.PasswordHash, p.Plate, active,
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func CreateUserIfMissing(ctx context.Context, q Querier, p CreateUserParams) error {
	active := true
	if p.Active != nil {
		active = *p.Active
	}
	_, err := q.Exec(ctx,
		`INSERT INTO "ya-qo-users" (email, name, role, password_hash, plate, active)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (email) DO NOTHING`,
		p.Email, p.Name, string(p.Role), p.PasswordHash, p.Plate, active,
	)
	return err
}

func GetUserByEmail(ctx context.Context, q Querier, email string) (User, error) {
	row := q.QueryRow(ctx,
		`SELECT id, email, name, role, password_hash, plate, active, created_at
		 FROM "ya-qo-users" WHERE email = $1`,
		email,
	)
	return scanUser(row)
}

func GetUser(ctx context.Context, q Querier, id int64) (User, error) {
	row := q.QueryRow(ctx,
		`SELECT id, email, name, role, password_hash, plate, active, created_at
		 FROM "ya-qo-users" WHERE id = $1`,
		id,
	)
	return scanUser(row)
}

func ListUsers(ctx context.Context, q Querier, afterID int64, limit int) ([]User, error) {
	rows, err := q.Query(ctx,
		`SELECT id, email, name, role, password_hash, plate, active, created_at
		 FROM "ya-qo-users" WHERE id > $1 ORDER BY id LIMIT $2`,
		afterID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

type userScanner interface {
	Scan(dest ...any) error
}

func scanUser(row userScanner) (User, error) {
	var (
		u     User
		role  string
		plate *string
	)
	err := row.Scan(&u.ID, &u.Email, &u.Name, &role, &u.PasswordHash, &plate, &u.Active, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	u.Role = auth.Role(role)
	u.Plate = plate
	return u, nil
}
