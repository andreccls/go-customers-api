// Package validation holds the tiny, dependency-free validation toolkit shared by
// the customer and auth packages.
package validation

import (
	"net/mail"
	"strings"
	"unicode/utf8"
)

// FieldError describes one invalid field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Errors collects field errors. A nil/empty Errors means "valid".
type Errors []FieldError

// Add records a problem with a field.
func (e *Errors) Add(field, message string) {
	*e = append(*e, FieldError{Field: field, Message: message})
}

// Err returns e as an error, or nil when nothing was recorded.
func (e Errors) Err() error {
	if len(e) == 0 {
		return nil
	}
	return e
}

func (e Errors) Error() string {
	parts := make([]string, len(e))
	for i, fe := range e {
		parts[i] = fe.Field + ": " + fe.Message
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

// Email trims and lower-cases s and reports whether it is a plain address
// (no display name, at most 254 bytes).
func Email(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || len(s) > 254 {
		return s, false
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s || !strings.Contains(s[strings.LastIndex(s, "@"):], ".") {
		return s, false
	}
	return s, true
}

// RuneLen is utf8.RuneCountInString, named for readability at call sites.
func RuneLen(s string) int { return utf8.RuneCountInString(s) }

// Digits returns only the ASCII digits of s.
func Digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
