package repotest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/andreccls/go-customers-api/internal/auth"
)

func user(email string) auth.User {
	return auth.User{ID: uuid.NewString(), Email: email, PasswordHash: "hash", Role: auth.RoleUser, CreatedAt: base}
}

// AuthStore runs the auth.Store contract; newStore must return an empty store.
func AuthStore(t *testing.T, newStore func(t *testing.T) auth.Store) {
	ctx := context.Background()

	t.Run("users: create, lookup, unique e-mail", func(t *testing.T) {
		s := newStore(t)
		u := user("a@example.com")
		u.Role = auth.RoleAdmin
		if err := s.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		byEmail, err := s.UserByEmail(ctx, "a@example.com")
		if err != nil || byEmail.ID != u.ID || byEmail.Role != auth.RoleAdmin || byEmail.PasswordHash != "hash" {
			t.Errorf("UserByEmail = %+v, %v", byEmail, err)
		}
		byID, err := s.UserByID(ctx, u.ID)
		if err != nil || byID.Email != u.Email {
			t.Errorf("UserByID = %+v, %v", byID, err)
		}
		if err := s.CreateUser(ctx, user("a@example.com")); !errors.Is(err, auth.ErrEmailTaken) {
			t.Errorf("duplicate: %v", err)
		}
		if _, err := s.UserByEmail(ctx, "x@example.com"); !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("unknown e-mail: %v", err)
		}
		if _, err := s.UserByID(ctx, uuid.NewString()); !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("unknown id: %v", err)
		}
	})

	t.Run("refresh tokens: single use, expiry, reuse revokes the family, revoke", func(t *testing.T) {
		s := newStore(t)
		u := user("a@example.com")
		other := user("b@example.com")
		_ = s.CreateUser(ctx, u)
		_ = s.CreateUser(ctx, other)
		now := time.Now().UTC()
		save := func(hash, userID string, ttl time.Duration) {
			t.Helper()
			if err := s.SaveRefreshToken(ctx, auth.RefreshToken{Hash: hash, UserID: userID, ExpiresAt: now.Add(ttl)}); err != nil {
				t.Fatal(err)
			}
		}
		save("t1", u.ID, time.Hour)
		save("t2", u.ID, time.Hour)
		save("o1", other.ID, time.Hour)
		save("old", u.ID, -time.Minute)

		if id, err := s.ConsumeRefreshToken(ctx, "t1", now); err != nil || id != u.ID {
			t.Fatalf("first use: %q, %v", id, err)
		}
		if _, err := s.ConsumeRefreshToken(ctx, "nope", now); !errors.Is(err, auth.ErrInvalidToken) {
			t.Errorf("unknown: %v", err)
		}
		if _, err := s.ConsumeRefreshToken(ctx, "old", now); !errors.Is(err, auth.ErrInvalidToken) {
			t.Errorf("expired: %v", err)
		}
		// Reusing t1 is refused AND kills the user's other tokens (t2), not other users' (o1).
		if _, err := s.ConsumeRefreshToken(ctx, "t1", now); !errors.Is(err, auth.ErrInvalidToken) {
			t.Errorf("reuse: %v", err)
		}
		if _, err := s.ConsumeRefreshToken(ctx, "t2", now); !errors.Is(err, auth.ErrInvalidToken) {
			t.Errorf("t2 should have been revoked by the reuse: %v", err)
		}
		if _, err := s.ConsumeRefreshToken(ctx, "o1", now); err != nil {
			t.Errorf("other user's token must survive: %v", err)
		}
		if err := s.RevokeRefreshToken(ctx, "o1"); err != nil {
			t.Fatal(err)
		}
		if err := s.RevokeRefreshToken(ctx, "o1"); err != nil {
			t.Errorf("revoke must be idempotent: %v", err)
		}
	})
}
