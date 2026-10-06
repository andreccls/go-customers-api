// Package memstore has in-memory implementations of the storage interfaces. They
// back the unit tests (no database needed) and are held to the same contract as the
// PostgreSQL repositories by the shared tests in package repotest.
package memstore

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/andreccls/go-customers-api/internal/customer"
)

// Customers is an in-memory customer.Repository.
type Customers struct {
	mu   sync.Mutex
	byID map[string]customer.Customer
}

// NewCustomers returns an empty store.
func NewCustomers() *Customers { return &Customers{byID: map[string]customer.Customer{}} }

func (m *Customers) Create(_ context.Context, c customer.Customer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.conflict(c); err != nil {
		return err
	}
	m.byID[c.ID] = c
	return nil
}

func (m *Customers) Get(_ context.Context, id string) (customer.Customer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.byID[id]
	if !ok {
		return customer.Customer{}, customer.ErrNotFound
	}
	return c, nil
}

func (m *Customers) List(_ context.Context, f customer.Filter) ([]customer.Customer, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q := strings.ToLower(f.Query)
	var all []customer.Customer
	for _, c := range m.byID {
		if f.Status != "" && c.Status != f.Status {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(c.Name), q) &&
			!strings.Contains(c.Email, q) && !strings.Contains(c.Document, q) {
			continue
		}
		all = append(all, c)
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].CreatedAt.After(all[j].CreatedAt)
		}
		return all[i].ID > all[j].ID
	})
	total := len(all)
	lo := min((f.Page-1)*f.PageSize, total)
	hi := min(lo+f.PageSize, total)
	return all[lo:hi], total, nil
}

func (m *Customers) Update(_ context.Context, c customer.Customer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byID[c.ID]; !ok {
		return customer.ErrNotFound
	}
	if err := m.conflict(c); err != nil {
		return err
	}
	c.CreatedAt = m.byID[c.ID].CreatedAt
	m.byID[c.ID] = c
	return nil
}

func (m *Customers) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byID[id]; !ok {
		return customer.ErrNotFound
	}
	delete(m.byID, id)
	return nil
}

// conflict emulates the UNIQUE constraints on e-mail and document.
func (m *Customers) conflict(c customer.Customer) error {
	for _, o := range m.byID {
		if o.ID == c.ID {
			continue
		}
		if o.Email == c.Email {
			return customer.ErrEmailTaken
		}
		if o.Document == c.Document {
			return customer.ErrDocumentTaken
		}
	}
	return nil
}
