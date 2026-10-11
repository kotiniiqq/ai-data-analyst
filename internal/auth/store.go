package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kotiniiqq/ai-data-analyst/internal/rbac"
)

var ErrNoUser = errors.New("no such user")

// Store looks users up for login.
type Store interface {
	ByEmail(ctx context.Context, email string) (u rbac.User, passwordHash string, err error)
}

type PGStore struct{ pool *pgxpool.Pool }

func NewPGStore(pool *pgxpool.Pool) *PGStore { return &PGStore{pool: pool} }

func (s *PGStore) ByEmail(ctx context.Context, email string) (rbac.User, string, error) {
	var id int64
	var mail, hash, role string
	err := s.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, role FROM users WHERE email = $1`,
		strings.ToLower(strings.TrimSpace(email))).Scan(&id, &mail, &hash, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return rbac.User{}, "", ErrNoUser
	}
	if err != nil {
		return rbac.User{}, "", err
	}
	return rbac.NewUser(id, mail, role), hash, nil
}

// Upsert creates the user or resets its password and role (used by the seed command).
func (s *PGStore) Upsert(ctx context.Context, email, password, role string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO users (email, password_hash, role) VALUES ($1, $2, $3)
		ON CONFLICT (email) DO UPDATE SET password_hash = EXCLUDED.password_hash, role = EXCLUDED.role`,
		strings.ToLower(strings.TrimSpace(email)), hash, role)
	return err
}
