package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/andreccls/go-customers-api/internal/auth"
	"github.com/andreccls/go-customers-api/internal/memstore"
	"github.com/andreccls/go-customers-api/internal/validation"
)

var ctx = context.Background()

const secret = "unit-test-secret-unit-test-secret-0123"

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func newService(t *testing.T) (*auth.Service, *fakeClock) {
	t.Helper()
	clk := &fakeClock{t: time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)}
	svc := auth.NewService(memstore.NewUsers(), auth.Config{
		Secret: []byte(secret), Issuer: "test", AccessTTL: 15 * time.Minute, RefreshTTL: time.Hour, BcryptCost: bcrypt.MinCost,
	}, clk.now)
	return svc, clk
}

func TestRegisterAndLogin(t *testing.T) {
	svc, _ := newService(t)
	u, err := svc.Register(ctx, " Ana@Example.com ", "s3cret-pass")
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "ana@example.com" || u.Role != auth.RoleUser || u.PasswordHash == "" || u.PasswordHash == "s3cret-pass" {
		t.Errorf("unexpected user: %+v", u)
	}
	if _, err := svc.Register(ctx, "ana@example.com", "another-pass"); !errors.Is(err, auth.ErrEmailTaken) {
		t.Errorf("duplicate: %v", err)
	}

	tok, err := svc.Login(ctx, "ANA@example.com", "s3cret-pass")
	if err != nil {
		t.Fatal(err)
	}
	if tok.ExpiresIn != 15*time.Minute || tok.RefreshToken == "" {
		t.Errorf("tokens: %+v", tok)
	}
	p, err := svc.Authenticate(tok.AccessToken)
	if err != nil || p.UserID != u.ID || p.Role != auth.RoleUser {
		t.Errorf("principal = %+v, %v", p, err)
	}
}

func TestRegisterValidation(t *testing.T) {
	svc, _ := newService(t)
	for name, tc := range map[string][3]string{
		"bad e-mail":     {"nope", "long-enough", "email"},
		"short password": {"a@example.com", "short", "password"},
		"72+ bytes":      {"a@example.com", string(make([]byte, 73)), "password"},
	} {
		_, err := svc.Register(ctx, tc[0], tc[1])
		var v validation.Errors
		if !errors.As(err, &v) || v[0].Field != tc[2] {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestLoginRejectsBadCredentialsUniformly(t *testing.T) {
	svc, _ := newService(t)
	_, _ = svc.Register(ctx, "ana@example.com", "s3cret-pass")
	for _, c := range [][2]string{{"ana@example.com", "wrong-pass"}, {"ghost@example.com", "s3cret-pass"}} {
		if _, err := svc.Login(ctx, c[0], c[1]); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Errorf("Login(%q): %v", c[0], err)
		}
	}
}

func TestEnsureAdmin(t *testing.T) {
	svc, _ := newService(t)
	if err := svc.EnsureAdmin(ctx, "root@example.com", "admin-pass-1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureAdmin(ctx, "root@example.com", "different-pass"); err != nil {
		t.Errorf("must be idempotent: %v", err)
	}
	tok, err := svc.Login(ctx, "root@example.com", "admin-pass-1")
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := svc.Authenticate(tok.AccessToken); p.Role != auth.RoleAdmin {
		t.Errorf("role = %v", p.Role)
	}
	if err := svc.EnsureAdmin(ctx, "bad", "admin-pass-1"); err == nil {
		t.Error("invalid admin e-mail must be an error")
	}
}

func TestRefreshRotation(t *testing.T) {
	svc, clk := newService(t)
	_, _ = svc.Register(ctx, "ana@example.com", "s3cret-pass")
	first, _ := svc.Login(ctx, "ana@example.com", "s3cret-pass")

	clk.t = clk.t.Add(time.Minute)
	second, err := svc.Refresh(ctx, first.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if second.RefreshToken == first.RefreshToken || second.AccessToken == first.AccessToken {
		t.Error("refresh must rotate both tokens")
	}
	// Reusing the first refresh token fails and burns the second one too.
	if _, err := svc.Refresh(ctx, first.RefreshToken); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("reuse: %v", err)
	}
	if _, err := svc.Refresh(ctx, second.RefreshToken); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("family should be revoked: %v", err)
	}
	if _, err := svc.Refresh(ctx, "garbage"); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("garbage: %v", err)
	}
}

func TestRefreshExpires(t *testing.T) {
	svc, clk := newService(t)
	_, _ = svc.Register(ctx, "ana@example.com", "s3cret-pass")
	tok, _ := svc.Login(ctx, "ana@example.com", "s3cret-pass")
	clk.t = clk.t.Add(2 * time.Hour)
	if _, err := svc.Refresh(ctx, tok.RefreshToken); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("expired refresh accepted: %v", err)
	}
}

func TestLogoutRevokes(t *testing.T) {
	svc, _ := newService(t)
	_, _ = svc.Register(ctx, "ana@example.com", "s3cret-pass")
	tok, _ := svc.Login(ctx, "ana@example.com", "s3cret-pass")
	if err := svc.Logout(ctx, tok.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if err := svc.Logout(ctx, tok.RefreshToken); err != nil {
		t.Errorf("logout must be idempotent: %v", err)
	}
	if _, err := svc.Refresh(ctx, tok.RefreshToken); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("revoked token accepted: %v", err)
	}
}

// goneStore simulates a user deleted after the refresh token was issued.
type goneStore struct{ auth.Store }

func (goneStore) UserByID(context.Context, string) (auth.User, error) {
	return auth.User{}, auth.ErrNotFound
}

func TestRefreshForDeletedUser(t *testing.T) {
	cfg := auth.Config{Secret: []byte(secret), Issuer: "test", AccessTTL: time.Minute, RefreshTTL: time.Hour, BcryptCost: bcrypt.MinCost}
	mem := memstore.NewUsers()
	healthy := auth.NewService(mem, cfg, nil)
	_, _ = healthy.Register(ctx, "a@example.com", "s3cret-pass")
	tok, _ := healthy.Login(ctx, "a@example.com", "s3cret-pass")
	if _, err := auth.NewService(goneStore{mem}, cfg, nil).Refresh(ctx, tok.RefreshToken); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("err = %v", err)
	}
}

func signed(t *testing.T, method jwt.SigningMethod, key any, c jwt.MapClaims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, c).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAuthenticateRejectsBadTokens(t *testing.T) {
	svc, clk := newService(t)
	exp := clk.t.Add(time.Minute).Unix()
	good := jwt.MapClaims{"sub": "u1", "role": "user", "iss": "test", "exp": exp}
	with := func(k string, v any) jwt.MapClaims {
		c := jwt.MapClaims{}
		for a, b := range good {
			c[a] = b
		}
		if v == nil {
			delete(c, k)
		} else {
			c[k] = v
		}
		return c
	}
	key := []byte(secret)
	tests := map[string]string{
		"empty":           "",
		"garbage":         "not.a.jwt",
		"wrong secret":    signed(t, jwt.SigningMethodHS256, []byte("another-secret-another-secret-0000"), good),
		"wrong algorithm": signed(t, jwt.SigningMethodHS512, key, good),
		"alg none":        signed(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, good),
		"expired":         signed(t, jwt.SigningMethodHS256, key, with("exp", clk.t.Add(-time.Minute).Unix())),
		"no expiry":       signed(t, jwt.SigningMethodHS256, key, with("exp", nil)),
		"wrong issuer":    signed(t, jwt.SigningMethodHS256, key, with("iss", "someone-else")),
		"no subject":      signed(t, jwt.SigningMethodHS256, key, with("sub", nil)),
		"unknown role":    signed(t, jwt.SigningMethodHS256, key, with("role", "root")),
	}
	for name, tok := range tests {
		if _, err := svc.Authenticate(tok); !errors.Is(err, auth.ErrInvalidToken) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if p, err := svc.Authenticate(signed(t, jwt.SigningMethodHS256, key, good)); err != nil || p.UserID != "u1" {
		t.Errorf("control token rejected: %+v, %v", p, err)
	}
}

// failingStore exercises the error paths that need a broken database.
type failingStore struct {
	auth.Store
	failSave, failUser, failConsume bool
}

var errBoom = errors.New("boom")

func (f failingStore) SaveRefreshToken(ctx context.Context, t auth.RefreshToken) error {
	if f.failSave {
		return errBoom
	}
	return f.Store.SaveRefreshToken(ctx, t)
}
func (f failingStore) UserByEmail(ctx context.Context, e string) (auth.User, error) {
	if f.failUser {
		return auth.User{}, errBoom
	}
	return f.Store.UserByEmail(ctx, e)
}
func (f failingStore) UserByID(ctx context.Context, id string) (auth.User, error) {
	if f.failUser {
		return auth.User{}, errBoom
	}
	return f.Store.UserByID(ctx, id)
}
func (f failingStore) ConsumeRefreshToken(ctx context.Context, h string, n time.Time) (string, error) {
	if f.failConsume {
		return "", errBoom
	}
	return f.Store.ConsumeRefreshToken(ctx, h, n)
}

func TestStoreErrorsPropagate(t *testing.T) {
	cfg := auth.Config{Secret: []byte(secret), Issuer: "test", AccessTTL: time.Minute, RefreshTTL: time.Hour, BcryptCost: bcrypt.MinCost}
	mem := memstore.NewUsers()
	healthy := auth.NewService(mem, cfg, nil)
	_, _ = healthy.Register(ctx, "a@example.com", "s3cret-pass")
	tok, _ := healthy.Login(ctx, "a@example.com", "s3cret-pass")

	if _, err := auth.NewService(failingStore{Store: mem, failSave: true}, cfg, nil).Login(ctx, "a@example.com", "s3cret-pass"); !errors.Is(err, errBoom) {
		t.Errorf("save: %v", err)
	}
	if _, err := auth.NewService(failingStore{Store: mem, failUser: true}, cfg, nil).Login(ctx, "a@example.com", "s3cret-pass"); !errors.Is(err, errBoom) {
		t.Errorf("lookup: %v", err)
	}
	if _, err := auth.NewService(failingStore{Store: mem, failConsume: true}, cfg, nil).Refresh(ctx, tok.RefreshToken); !errors.Is(err, errBoom) {
		t.Errorf("consume: %v", err)
	}
	if _, err := auth.NewService(failingStore{Store: mem, failUser: true}, cfg, nil).Refresh(ctx, tok.RefreshToken); !errors.Is(err, errBoom) {
		t.Errorf("user by id: %v", err)
	}
}

func TestInvalidBcryptCostIsAnError(t *testing.T) {
	svc := auth.NewService(memstore.NewUsers(), auth.Config{Secret: []byte(secret), Issuer: "test", BcryptCost: 99}, nil)
	if _, err := svc.Register(ctx, "a@example.com", "s3cret-pass"); err == nil {
		t.Error("hashing with an invalid cost must fail, not store a broken hash")
	}
}
