package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreccls/go-customers-api/internal/auth"
)

// Users is the PostgreSQL auth.Store.
type Users struct{ pool *pgxpool.Pool }

// NewUsers returns a store on pool.
func NewUsers(pool *pgxpool.Pool) *Users { return &Users{pool: pool} }

func (s *Users) CreateUser(ctx context.Context, u auth.User) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, role, created_at) VALUES ($1,$2,$3,$4,$5)`,
		u.ID, u.Email, u.PasswordHash, string(u.Role), u.CreatedAt)
	if c, ok := uniqueViolation(err); ok && c == "users_email_key" {
		return auth.ErrEmailTaken
	}
	return err
}

func (s *Users) UserByEmail(ctx context.Context, email string) (auth.User, error) {
	return s.one(ctx, `WHERE email = $1`, email)
}

func (s *Users) UserByID(ctx context.Context, id string) (auth.User, error) {
	return s.one(ctx, `WHERE id = $1`, id)
}

func (s *Users) one(ctx context.Context, where string, arg any) (auth.User, error) {
	var u auth.User
	var role string
	err := s.pool.QueryRow(ctx, `SELECT id, email, password_hash, role, created_at FROM users `+where, arg).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &role, &u.CreatedAt)
	if isNoRows(err) {
		return auth.User{}, auth.ErrNotFound
	}
	u.Role, u.CreatedAt = auth.Role(role), u.CreatedAt.UTC()
	return u, err
}

func (s *Users) SaveRefreshToken(ctx context.Context, t auth.RefreshToken) error {
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO refresh_tokens (token_hash, user_id, expires_at) VALUES ($1,$2,$3)`,
		t.Hash, t.UserID, t.ExpiresAt); err != nil {
		return err
	}
	// Housekeeping: drop this user's expired tokens so the table does not grow forever.
	_, err := s.pool.Exec(ctx, `DELETE FROM refresh_tokens WHERE user_id = $1 AND expires_at < now()`, t.UserID)
	return err
}

func (s *Users) ConsumeRefreshToken(ctx context.Context, hash string, now time.Time) (string, error) {
	var userID string
	err := s.pool.QueryRow(ctx,
		`UPDATE refresh_tokens SET used_at = $2
		 WHERE token_hash = $1 AND used_at IS NULL AND expires_at > $2 RETURNING user_id`,
		hash, now).Scan(&userID)
	if err == nil {
		return userID, nil
	}
	if !isNoRows(err) {
		return "", err
	}
	// Not usable. If it was already used, someone is replaying it: revoke the user's tokens.
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM refresh_tokens WHERE user_id = (SELECT user_id FROM refresh_tokens WHERE token_hash = $1 AND used_at IS NOT NULL)`,
		hash); err != nil {
		return "", err
	}
	return "", auth.ErrInvalidToken
}

func (s *Users) RevokeRefreshToken(ctx context.Context, hash string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM refresh_tokens WHERE token_hash = $1`, hash)
	return err
}
