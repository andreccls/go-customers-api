package validation

import (
	"errors"
	"strings"
	"testing"
)

func TestEmail(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"Ana@Example.com ", "ana@example.com", true},
		{"a.b+tag@sub.example.org", "a.b+tag@sub.example.org", true},
		{"", "", false},
		{"no-at-sign", "no-at-sign", false},
		{"Ana <ana@example.com>", "ana <ana@example.com>", false},
		{"ana@localhost", "ana@localhost", false},
		{strings.Repeat("a", 250) + "@x.io", "", false},
	}
	for _, tt := range tests {
		got, ok := Email(tt.in)
		if ok != tt.ok || (tt.ok && got != tt.want) {
			t.Errorf("Email(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestErrors(t *testing.T) {
	var e Errors
	if e.Err() != nil {
		t.Fatal("empty Errors must be a nil error")
	}
	e.Add("name", "required")
	e.Add("email", "invalid")
	err := e.Err()
	var got Errors
	if !errors.As(err, &got) || len(got) != 2 {
		t.Fatalf("errors.As failed: %v", err)
	}
	if want := "validation failed: name: required; email: invalid"; err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestDigitsAndRuneLen(t *testing.T) {
	if got := Digits("(31) 99999-0000 x"); got != "31999990000" {
		t.Errorf("Digits = %q", got)
	}
	if RuneLen("João") != 4 {
		t.Error("RuneLen should count runes, not bytes")
	}
}
