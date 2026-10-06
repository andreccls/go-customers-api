package httpapi

import (
	"net/http"
	"time"

	"github.com/andreccls/go-customers-api/internal/auth"
)

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type userResponse struct {
	ID    string    `json:"id"`
	Email string    `json:"email"`
	Role  auth.Role `json:"role"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"` // seconds
	RefreshToken string `json:"refresh_token"`
}

func newTokenResponse(t auth.Tokens) tokenResponse {
	return tokenResponse{AccessToken: t.AccessToken, TokenType: "Bearer",
		ExpiresIn: int(t.ExpiresIn / time.Second), RefreshToken: t.RefreshToken}
}

func (s *server) register(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !s.decode(w, r, &in) {
		return
	}
	u, err := s.auth.Register(r.Context(), in.Email, in.Password)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, userResponse{ID: u.ID, Email: u.Email, Role: u.Role})
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !s.decode(w, r, &in) {
		return
	}
	t, err := s.auth.Login(r.Context(), in.Email, in.Password)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newTokenResponse(t))
}

func (s *server) refresh(w http.ResponseWriter, r *http.Request) {
	var in refreshRequest
	if !s.decode(w, r, &in) {
		return
	}
	t, err := s.auth.Refresh(r.Context(), in.RefreshToken)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newTokenResponse(t))
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	var in refreshRequest
	if !s.decode(w, r, &in) {
		return
	}
	if err := s.auth.Logout(r.Context(), in.RefreshToken); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
