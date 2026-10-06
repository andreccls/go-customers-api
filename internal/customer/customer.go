// Package customer is the customer domain: the model, its validation rules and the
// service that orchestrates them. It knows nothing about HTTP or SQL; storage is
// reached through the small Repository interface declared next to its consumer.
package customer

import (
	"errors"
	"time"
)

// Status is the lifecycle state of a customer.
type Status string

const (
	StatusActive   Status = "active"
	StatusInactive Status = "inactive"
)

// Valid reports whether s is a known status.
func (s Status) Valid() bool { return s == StatusActive || s == StatusInactive }

// Address is a simple Brazilian postal address. The zero value means "no address".
type Address struct {
	Street  string `json:"street"`
	Number  string `json:"number"`
	City    string `json:"city"`
	State   string `json:"state"`
	ZipCode string `json:"zip_code"`
}

// Customer is the stored entity. Email, Document and Phone are held normalized
// (lower-case e-mail, digits-only document and phone).
type Customer struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Document  string    `json:"document"`
	Phone     string    `json:"phone,omitempty"`
	Address   Address   `json:"address,omitzero"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Input is the writable part of a customer (create and full update).
type Input struct {
	Name     string  `json:"name"`
	Email    string  `json:"email"`
	Document string  `json:"document"`
	Phone    string  `json:"phone"`
	Address  Address `json:"address"`
	Status   Status  `json:"status"` // optional: defaults to active
}

// Patch is a partial update: nil fields are left untouched. A non-nil Address
// replaces the whole address.
type Patch struct {
	Name     *string  `json:"name"`
	Email    *string  `json:"email"`
	Document *string  `json:"document"`
	Phone    *string  `json:"phone"`
	Address  *Address `json:"address"`
	Status   *Status  `json:"status"`
}

// Filter selects a page of customers.
type Filter struct {
	Status   Status // empty = any
	Query    string // case-insensitive substring of name, e-mail or document
	Page     int    // 1-based
	PageSize int
}

// Domain errors. Handlers map them to HTTP; the repository returns them.
var (
	ErrNotFound      = errors.New("customer not found")
	ErrEmailTaken    = errors.New("e-mail already registered")
	ErrDocumentTaken = errors.New("document already registered")
)
