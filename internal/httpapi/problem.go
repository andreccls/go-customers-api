package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"

	"github.com/andreccls/go-customers-api/internal/auth"
	"github.com/andreccls/go-customers-api/internal/customer"
	"github.com/andreccls/go-customers-api/internal/validation"
)

// problem is an RFC 9457 problem details document. "type" is always about:blank
// (the HTTP status says it all); `code` is a stable machine-readable extension.
type problem struct {
	Type      string            `json:"type"`
	Title     string            `json:"title"`
	Status    int               `json:"status"`
	Detail    string            `json:"detail,omitempty"`
	Code      string            `json:"code"`
	RequestID string            `json:"request_id,omitempty"`
	Errors    validation.Errors `json:"errors,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, code, detail string, errs validation.Errors) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{
		Type: "about:blank", Title: http.StatusText(status), Status: status,
		Detail: detail, Code: code, RequestID: requestIDFrom(r.Context()), Errors: errs,
	})
}

// fail translates a domain error into a problem response. Unknown errors become a
// generic 500: the cause is logged, never sent to the client.
func (s *server) fail(w http.ResponseWriter, r *http.Request, err error) {
	var verrs validation.Errors
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &verrs):
		writeProblem(w, r, http.StatusUnprocessableEntity, "validation_failed", "One or more fields are invalid.", verrs)
	case errors.As(err, &tooBig):
		writeProblem(w, r, http.StatusRequestEntityTooLarge, "body_too_large", "The request body is too large.", nil)
	case errors.Is(err, customer.ErrNotFound):
		writeProblem(w, r, http.StatusNotFound, "customer_not_found", "Customer not found.", nil)
	case errors.Is(err, customer.ErrEmailTaken), errors.Is(err, auth.ErrEmailTaken):
		writeProblem(w, r, http.StatusConflict, "email_taken", "That e-mail is already registered.", nil)
	case errors.Is(err, customer.ErrDocumentTaken):
		writeProblem(w, r, http.StatusConflict, "document_taken", "That document is already registered.", nil)
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeProblem(w, r, http.StatusUnauthorized, "invalid_credentials", "Invalid e-mail or password.", nil)
	case errors.Is(err, auth.ErrInvalidToken):
		unauthorized(w, r, "invalid_token", "The token is invalid or expired.")
	case errors.Is(err, context.DeadlineExceeded):
		writeProblem(w, r, http.StatusServiceUnavailable, "timeout", "The request took too long.", nil)
	default:
		s.log.ErrorContext(r.Context(), "unhandled error", slog.Any("error", err), slog.String("request_id", requestIDFrom(r.Context())))
		writeProblem(w, r, http.StatusInternalServerError, "internal_error", "An unexpected error occurred.", nil)
	}
}

func unauthorized(w http.ResponseWriter, r *http.Request, code, detail string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="api"`)
	writeProblem(w, r, http.StatusUnauthorized, code, detail, nil)
}

// decode reads a single JSON object from the body, rejecting unknown fields and
// trailing data. Malformed input is a 400; the size limit is a 413; wrong media type a 415.
func isJSON(contentType string) bool {
	mt, _, _ := mime.ParseMediaType(contentType)
	return mt == "application/json"
}

func (s *server) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if ct := r.Header.Get("Content-Type"); ct != "" && !isJSON(ct) {
		writeProblem(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.", nil)
		return false
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	if err == nil {
		if _, terr := dec.Token(); !errors.Is(terr, io.EOF) {
			err = errors.New("unexpected data after the JSON document")
		}
	}
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			s.fail(w, r, err)
		} else {
			writeProblem(w, r, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON for this endpoint: "+err.Error(), nil)
		}
		return false
	}
	return true
}
