package memstore

import (
	"context"
	"sync"
	"time"

	"github.com/andreccls/go-customers-api/internal/auth"
)

type refresh struct {
	userID  string
	expires time.Time
	used    bool
}

// Users is an in-memory auth.Store.
type Users struct {
	mu      sync.Mutex
	byID    map[string]auth.User
	tokens  map[string]*refresh
}

// NewUsers returns an empty store.
func NewUsers() *Users {
	return &Users{byID: map[string]auth.User{}, tokens: map[string]*refresh{}}
}

func (m *Users) CreateUser(_ context.Context, u auth.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, o := range m.byID {
		if o.Email == u.Email {
			return auth.ErrEmailTaken
		}
	}
	m.byID[u.ID] = u
	return nil
}

func (m *Users) UserByEmail(_ context.Context, email string) (auth.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.byID {
		if u.Email == email {
			return u, nil
		}
	}
	return auth.User{}, auth.ErrNotFound
}

func (m *Users) UserByID(_ context.Context, id string) (auth.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.byID[id]
	if !ok {
		return auth.User{}, auth.ErrNotFound
	}
	return u, nil
}

func (m *Users) SaveRefreshToken(_ context.Context, t auth.RefreshToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[t.Hash] = &refresh{userID: t.UserID, expires: t.ExpiresAt}
	return nil
}

func (m *Users) ConsumeRefreshToken(_ context.Context, hash string, now time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tokens[hash]
	if !ok {
		return "", auth.ErrInvalidToken
	}
	if t.used {
		for h, o := range m.tokens {
			if o.userID == t.userID {
				delete(m.tokens, h)
			}
		}
		return "", auth.ErrInvalidToken
	}
	if !t.expires.After(now) {
		return "", auth.ErrInvalidToken
	}
	t.used = true
	return t.userID, nil
}

func (m *Users) RevokeRefreshToken(_ context.Context, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tokens, hash)
	return nil
}
