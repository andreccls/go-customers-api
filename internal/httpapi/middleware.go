package httpapi

import (
	"context"
	"log/slog"
	"math"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/andreccls/go-customers-api/internal/auth"
	"github.com/andreccls/go-customers-api/internal/ratelimit"
)

// requestInfo is created once per request and filled in as it travels inward, so the
// outermost middleware (the access log) can report the route and user at the end.
type requestInfo struct {
	id     string
	route  string
	userID string
}

type ctxKey int

const (
	infoKey ctxKey = iota
	principalKey
)

func requestIDFrom(ctx context.Context) string {
	if i, ok := ctx.Value(infoKey).(*requestInfo); ok {
		return i.id
	}
	return ""
}

// PrincipalFrom returns the authenticated caller, if any.
func PrincipalFrom(ctx context.Context) (auth.Principal, bool) {
	p, ok := ctx.Value(principalKey).(auth.Principal)
	return p, ok
}

// statusRecorder remembers what was written, for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the real writer.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// validRequestID accepts a client-supplied X-Request-ID only if it is short and
// plain, so it is safe to echo into headers and logs.
func validRequestID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

// observe is the outermost middleware: request ID, security header, access log.
func (s *server) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := &requestInfo{id: r.Header.Get("X-Request-ID")}
		if !validRequestID(info.id) {
			info.id = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", info.id)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		r = r.WithContext(context.WithValue(r.Context(), infoKey, info))

		rec := &statusRecorder{ResponseWriter: w}
		start := time.Now()
		next.ServeHTTP(rec, r)

		s.log.LogAttrs(r.Context(), levelFor(rec.status), "request",
			slog.String("request_id", info.id),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("route", info.route),
			slog.Int("status", rec.status),
			slog.Int("bytes", rec.bytes),
			slog.Duration("duration", time.Since(start)),
			slog.String("remote", clientIP(r)),
			slog.String("user_id", info.userID),
		)
	})
}

func levelFor(status int) slog.Level {
	if status >= 500 {
		return slog.LevelError
	}
	return slog.LevelInfo
}

// recoverer turns a panic into a 500 problem instead of a dropped connection.
func (s *server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.log.ErrorContext(r.Context(), "panic recovered",
					slog.Any("panic", v), slog.String("stack", string(debug.Stack())),
					slog.String("request_id", requestIDFrom(r.Context())))
				writeProblem(w, r, http.StatusInternalServerError, "internal_error", "An unexpected error occurred.", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// limits bounds the request body and the time the handler (and its queries) may take.
func (s *server) limits(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), s.requestTimeout)
		defer cancel()
		r = r.WithContext(ctx)
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// route records the matched pattern for the access log.
func named(pattern string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if i, ok := r.Context().Value(infoKey).(*requestInfo); ok {
			i.route = pattern
		}
		next.ServeHTTP(w, r)
	})
}

// authenticate requires a valid Bearer access token.
func (s *server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, token, _ := strings.Cut(r.Header.Get("Authorization"), " ")
		if !strings.EqualFold(scheme, "Bearer") || token == "" {
			unauthorized(w, r, "missing_token", "Send a Bearer access token in the Authorization header.")
			return
		}
		p, err := s.auth.Authenticate(strings.TrimSpace(token))
		if err != nil {
			unauthorized(w, r, "invalid_token", "The access token is invalid or expired.")
			return
		}
		if i, ok := r.Context().Value(infoKey).(*requestInfo); ok {
			i.userID = p.UserID
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey, p)))
	})
}

// requireRole answers 403 unless the caller has the role.
func requireRole(role auth.Role, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p, _ := PrincipalFrom(r.Context()); p.Role != role {
			writeProblem(w, r, http.StatusForbidden, "forbidden", "Your role is not allowed to perform this action.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// rateLimit answers 429 when key(r) exceeded its budget.
func rateLimit(l *ratelimit.Limiter, key func(*http.Request) string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, retry := l.Allow(key(r)); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retry.Seconds()))))
			writeProblem(w, r, http.StatusTooManyRequests, "rate_limited", "Too many requests; slow down.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP is the TCP peer address. X-Forwarded-For is deliberately NOT trusted:
// anyone could spoof it to dodge the limiter. Behind a proxy, every client would
// share the proxy's IP (see "Limitations" in the README).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func byIP(r *http.Request) string { return clientIP(r) }

func byUser(r *http.Request) string {
	p, _ := PrincipalFrom(r.Context())
	return p.UserID
}
