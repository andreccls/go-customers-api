// Package auth registers API users, verifies passwords (bcrypt) and issues and
// checks tokens: short-lived JWT access tokens plus opaque, rotating refresh tokens.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/andreccls/go-customers-api/internal/validation"
)

// Role decides what a user may do: admin can delete customers; user reads/writes.
type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

// User is an API user (not a customer).
type User struct {
	ID           string
	Email        string
	PasswordHash string
	Role         Role
	CreatedAt    time.Time
}

// RefreshToken is the stored (hashed) form of an issued refresh token.
type RefreshToken struct {
	Hash      string
	UserID    string
	ExpiresAt time.Time
}

// Principal is the authenticated caller extracted from an access token.
type Principal struct {
	UserID string
	Role   Role
}

// Tokens is what login and refresh hand back.
type Tokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    time.Duration
}

// Errors returned by Service and Store.
var (
	ErrEmailTaken         = errors.New("e-mail already registered")
	ErrNotFound           = errors.New("user not found")
	ErrInvalidCredentials = errors.New("invalid e-mail or password")
	ErrInvalidToken       = errors.New("invalid or expired token")
)

// Store is what the service needs from storage.
type Store interface {
	CreateUser(ctx context.Context, u User) error // ErrEmailTaken
	UserByEmail(ctx context.Context, email string) (User, error)
	UserByID(ctx context.Context, id string) (User, error) // ErrNotFound
	SaveRefreshToken(ctx context.Context, t RefreshToken) error
	// ConsumeRefreshToken marks the token as used and returns its owner. Unknown,
	// expired or already-used tokens give ErrInvalidToken; presenting an already-used
	// token also revokes every refresh token of that user (reuse = likely theft).
	ConsumeRefreshToken(ctx context.Context, hash string, now time.Time) (userID string, err error)
	RevokeRefreshToken(ctx context.Context, hash string) error // idempotent
}

// Config parameterizes Service.
type Config struct {
	Secret     []byte
	Issuer     string
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	BcryptCost int // 0 = bcrypt.DefaultCost
}

// Service holds the authentication use cases.
type Service struct {
	store     Store
	cfg       Config
	now       func() time.Time
	dummyHash []byte // compared against when the e-mail is unknown, to even out timing
}

// NewService builds a Service. now is injectable for tests (nil = time.Now).
func NewService(store Store, cfg Config, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	if cfg.BcryptCost == 0 {
		cfg.BcryptCost = bcrypt.DefaultCost
	}
	dummy, _ := bcrypt.GenerateFromPassword([]byte("dummy-password"), cfg.BcryptCost)
	return &Service{store: store, cfg: cfg, now: now, dummyHash: dummy}
}

// Register creates a regular user.
func (s *Service) Register(ctx context.Context, email, password string) (User, error) {
	return s.create(ctx, email, password, RoleUser)
}

// EnsureAdmin creates the bootstrap admin if no user has that e-mail yet.
func (s *Service) EnsureAdmin(ctx context.Context, email, password string) error {
	_, err := s.create(ctx, email, password, RoleAdmin)
	if errors.Is(err, ErrEmailTaken) {
		return nil
	}
	return err
}

func (s *Service) create(ctx context.Context, email, password string, role Role) (User, error) {
	var v validation.Errors
	email, ok := validation.Email(email)
	if !ok {
		v.Add("email", "must be a valid e-mail address")
	}
	if n := len(password); n < 8 || n > 72 { // bcrypt ignores bytes past 72
		v.Add("password", "must have between 8 and 72 bytes")
	}
	if err := v.Err(); err != nil {
		return User{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.cfg.BcryptCost)
	if err != nil {
		return User{}, err
	}
	u := User{ID: uuid.NewString(), Email: email, PasswordHash: string(hash), Role: role, CreatedAt: s.now().UTC()}
	if err := s.store.CreateUser(ctx, u); err != nil {
		return User{}, err
	}
	return u, nil
}

// Login checks the credentials and issues a token pair.
func (s *Service) Login(ctx context.Context, email, password string) (Tokens, error) {
	email, _ = validation.Email(email)
	u, err := s.store.UserByEmail(ctx, email)
	switch {
	case errors.Is(err, ErrNotFound):
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return Tokens{}, ErrInvalidCredentials
	case err != nil:
		return Tokens{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return Tokens{}, ErrInvalidCredentials
	}
	return s.issue(ctx, u)
}

// Refresh exchanges a refresh token for a new pair; the old token stops working.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (Tokens, error) {
	userID, err := s.store.ConsumeRefreshToken(ctx, hashToken(refreshToken), s.now())
	if err != nil {
		return Tokens{}, err
	}
	u, err := s.store.UserByID(ctx, userID)
	if errors.Is(err, ErrNotFound) {
		return Tokens{}, ErrInvalidToken
	}
	if err != nil {
		return Tokens{}, err
	}
	return s.issue(ctx, u)
}

// Logout revokes a refresh token. Unknown tokens are ignored (idempotent).
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	return s.store.RevokeRefreshToken(ctx, hashToken(refreshToken))
}

type claims struct {
	Role Role `json:"role"`
	jwt.RegisteredClaims
}

// Authenticate validates an access token and returns its principal.
func (s *Service) Authenticate(token string) (Principal, error) {
	var c claims
	_, err := jwt.ParseWithClaims(token, &c, func(*jwt.Token) (any, error) { return s.cfg.Secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(s.cfg.Issuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(s.now),
	)
	if err != nil || c.Subject == "" || (c.Role != RoleAdmin && c.Role != RoleUser) {
		return Principal{}, ErrInvalidToken
	}
	return Principal{UserID: c.Subject, Role: c.Role}, nil
}

func (s *Service) issue(ctx context.Context, u User) (Tokens, error) {
	now := s.now()
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Role: u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.cfg.Issuer,
			Subject:   u.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.cfg.AccessTTL)),
		},
	}).SignedString(s.cfg.Secret)
	if err != nil {
		return Tokens{}, err
	}
	raw := make([]byte, 32)
	_, _ = rand.Read(raw) // never fails: since Go 1.24 crypto/rand.Read crashes the program instead
	refresh := base64.RawURLEncoding.EncodeToString(raw)
	if err := s.store.SaveRefreshToken(ctx, RefreshToken{
		Hash: hashToken(refresh), UserID: u.ID, ExpiresAt: now.Add(s.cfg.RefreshTTL),
	}); err != nil {
		return Tokens{}, err
	}
	return Tokens{AccessToken: access, RefreshToken: refresh, ExpiresIn: s.cfg.AccessTTL}, nil
}

// hashToken is what is stored: a leaked database does not leak usable refresh tokens.
func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}
