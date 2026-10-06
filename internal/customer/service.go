package customer

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/andreccls/go-customers-api/internal/validation"
)

// Repository is what the service needs from storage. Implementations must return
// ErrNotFound, ErrEmailTaken and ErrDocumentTaken as documented here.
type Repository interface {
	// Create stores c; ErrEmailTaken / ErrDocumentTaken on unique violations.
	Create(ctx context.Context, c Customer) error
	// Get returns ErrNotFound when the id does not exist.
	Get(ctx context.Context, id string) (Customer, error)
	// List returns one page (newest first) and the total number of matches.
	List(ctx context.Context, f Filter) ([]Customer, int, error)
	// Update replaces the stored customer; ErrNotFound, ErrEmailTaken, ErrDocumentTaken.
	Update(ctx context.Context, c Customer) error
	// Delete removes the customer permanently; ErrNotFound when absent.
	Delete(ctx context.Context, id string) error
}

// Pagination bounds.
const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// Service holds the customer use cases.
type Service struct {
	repo Repository
	now  func() time.Time
}

// NewService builds a Service. now is injectable for deterministic tests.
func NewService(repo Repository, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repo: repo, now: now}
}

// Create validates in and stores a new customer.
func (s *Service) Create(ctx context.Context, in Input) (Customer, error) {
	n, err := normalize(in)
	if err != nil {
		return Customer{}, err
	}
	now := s.now().UTC()
	c := Customer{ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now}
	apply(&c, n)
	if err := s.repo.Create(ctx, c); err != nil {
		return Customer{}, err
	}
	return c, nil
}

// Get returns one customer.
func (s *Service) Get(ctx context.Context, id string) (Customer, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Customer{}, ErrNotFound
	}
	return s.repo.Get(ctx, id)
}

// List returns a page of customers. Invalid filters yield validation errors.
func (s *Service) List(ctx context.Context, f Filter) ([]Customer, int, error) {
	var verrs validation.Errors
	if f.Page == 0 {
		f.Page = 1
	}
	if f.PageSize == 0 {
		f.PageSize = DefaultPageSize
	}
	if f.Page < 1 {
		verrs.Add("page", "must be >= 1")
	}
	if f.PageSize < 1 || f.PageSize > MaxPageSize {
		verrs.Add("page_size", "must be between 1 and 100")
	}
	if f.Status != "" && !f.Status.Valid() {
		verrs.Add("status", "must be active or inactive")
	}
	if err := verrs.Err(); err != nil {
		return nil, 0, err
	}
	f.Query = strings.TrimSpace(f.Query)
	return s.repo.List(ctx, f)
}

// Replace is the full update (PUT): every writable field is overwritten.
func (s *Service) Replace(ctx context.Context, id string, in Input) (Customer, error) {
	n, err := normalize(in)
	if err != nil {
		return Customer{}, err
	}
	c, err := s.Get(ctx, id)
	if err != nil {
		return Customer{}, err
	}
	apply(&c, n)
	return s.save(ctx, c)
}

// Update is the partial update (PATCH): only non-nil fields change.
// It is read-modify-write without a version check: the last write wins.
func (s *Service) Update(ctx context.Context, id string, p Patch) (Customer, error) {
	c, err := s.Get(ctx, id)
	if err != nil {
		return Customer{}, err
	}
	merged := Input{Name: c.Name, Email: c.Email, Document: c.Document, Phone: c.Phone, Address: c.Address, Status: c.Status}
	if p.Name != nil {
		merged.Name = *p.Name
	}
	if p.Email != nil {
		merged.Email = *p.Email
	}
	if p.Document != nil {
		merged.Document = *p.Document
	}
	if p.Phone != nil {
		merged.Phone = *p.Phone
	}
	if p.Address != nil {
		merged.Address = *p.Address
	}
	if p.Status != nil {
		merged.Status = *p.Status
	}
	n, err := normalize(merged)
	if err != nil {
		return Customer{}, err
	}
	apply(&c, n)
	return s.save(ctx, c)
}

// Delete removes a customer permanently (see ADR 0003).
func (s *Service) Delete(ctx context.Context, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}
	return s.repo.Delete(ctx, id)
}

func (s *Service) save(ctx context.Context, c Customer) (Customer, error) {
	c.UpdatedAt = s.now().UTC()
	if err := s.repo.Update(ctx, c); err != nil {
		return Customer{}, err
	}
	return c, nil
}

func apply(c *Customer, in Input) {
	c.Name, c.Email, c.Document, c.Phone = in.Name, in.Email, in.Document, in.Phone
	c.Address, c.Status = in.Address, in.Status
}

// normalize validates in and returns it in canonical form.
func normalize(in Input) (Input, error) {
	var v validation.Errors

	in.Name = strings.TrimSpace(in.Name)
	if n := validation.RuneLen(in.Name); n < 2 || n > 120 {
		v.Add("name", "must have between 2 and 120 characters")
	}

	email, ok := validation.Email(in.Email)
	if !ok {
		v.Add("email", "must be a valid e-mail address")
	}
	in.Email = email

	doc, ok := NormalizeDocument(in.Document)
	if !ok {
		v.Add("document", "must be a valid CPF or CNPJ")
	}
	in.Document = doc

	if in.Phone = strings.TrimSpace(in.Phone); in.Phone != "" {
		p := validation.Digits(in.Phone)
		if strings.Trim(in.Phone, "0123456789()+- ") != "" || len(p) < 10 || len(p) > 13 {
			v.Add("phone", "must have 10 to 13 digits")
		}
		in.Phone = p
	}

	if in.Address != (Address{}) {
		in.Address = normalizeAddress(in.Address, &v)
	}

	switch in.Status {
	case "":
		in.Status = StatusActive
	case StatusActive, StatusInactive:
	default:
		v.Add("status", "must be active or inactive")
	}
	return in, v.Err()
}

func normalizeAddress(a Address, v *validation.Errors) Address {
	a.Street, a.Number, a.City = strings.TrimSpace(a.Street), strings.TrimSpace(a.Number), strings.TrimSpace(a.City)
	a.State = strings.ToUpper(strings.TrimSpace(a.State))
	if a.Street == "" {
		v.Add("address.street", "is required when address is present")
	}
	if a.Number == "" {
		v.Add("address.number", "is required when address is present (use S/N if none)")
	}
	if a.City == "" {
		v.Add("address.city", "is required when address is present")
	}
	if len(a.State) != 2 || strings.Trim(a.State, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
		v.Add("address.state", "must be a 2-letter state code")
	}
	zip := validation.Digits(a.ZipCode)
	if len(zip) != 8 || strings.Trim(a.ZipCode, "0123456789- ") != "" {
		v.Add("address.zip_code", "must have 8 digits")
	}
	a.ZipCode = zip
	return a
}
