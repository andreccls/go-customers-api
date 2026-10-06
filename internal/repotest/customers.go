// Package repotest holds contract tests shared by every implementation of the
// storage interfaces (in-memory and PostgreSQL), so the fake used by the unit tests
// cannot drift from the real thing.
package repotest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/andreccls/go-customers-api/internal/customer"
)

var base = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func sample(i int) customer.Customer {
	return customer.Customer{
		ID:        uuid.NewString(),
		Name:      fmt.Sprintf("Customer %02d", i),
		Email:     fmt.Sprintf("c%02d@example.com", i),
		Document:  fmt.Sprintf("%011d", i),
		Phone:     "31999990000",
		Address:   customer.Address{Street: "Rua A", Number: "10", City: "Belo Horizonte", State: "MG", ZipCode: "30130000"},
		Status:    customer.StatusActive,
		CreatedAt: base.Add(time.Duration(i) * time.Minute),
		UpdatedAt: base.Add(time.Duration(i) * time.Minute),
	}
}

// Customers runs the customer.Repository contract; newRepo must return an empty repository.
func Customers(t *testing.T, newRepo func(t *testing.T) customer.Repository) {
	ctx := context.Background()

	t.Run("create then get round-trips every field", func(t *testing.T) {
		r := newRepo(t)
		c := sample(1)
		if err := r.Create(ctx, c); err != nil {
			t.Fatal(err)
		}
		got, err := r.Get(ctx, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !got.CreatedAt.Equal(c.CreatedAt) || !got.UpdatedAt.Equal(c.UpdatedAt) {
			t.Errorf("timestamps differ: %v / %v", got.CreatedAt, got.UpdatedAt)
		}
		got.CreatedAt, got.UpdatedAt, c.CreatedAt, c.UpdatedAt = time.Time{}, time.Time{}, time.Time{}, time.Time{}
		if got != c {
			t.Errorf("got %+v, want %+v", got, c)
		}
	})

	t.Run("customer without address and phone round-trips", func(t *testing.T) {
		r := newRepo(t)
		c := sample(1)
		c.Address, c.Phone = customer.Address{}, ""
		if err := r.Create(ctx, c); err != nil {
			t.Fatal(err)
		}
		got, _ := r.Get(ctx, c.ID)
		if got.Address != (customer.Address{}) || got.Phone != "" {
			t.Errorf("expected empty address/phone, got %+v", got)
		}
	})

	t.Run("get unknown id is ErrNotFound", func(t *testing.T) {
		if _, err := newRepo(t).Get(ctx, uuid.NewString()); !errors.Is(err, customer.ErrNotFound) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("unique e-mail and document", func(t *testing.T) {
		r := newRepo(t)
		a := sample(1)
		if err := r.Create(ctx, a); err != nil {
			t.Fatal(err)
		}
		dupEmail := sample(2)
		dupEmail.Email = a.Email
		if err := r.Create(ctx, dupEmail); !errors.Is(err, customer.ErrEmailTaken) {
			t.Errorf("duplicate e-mail: err = %v", err)
		}
		dupDoc := sample(3)
		dupDoc.Document = a.Document
		if err := r.Create(ctx, dupDoc); !errors.Is(err, customer.ErrDocumentTaken) {
			t.Errorf("duplicate document: err = %v", err)
		}
	})

	t.Run("update replaces fields, keeps created_at, enforces uniqueness", func(t *testing.T) {
		r := newRepo(t)
		a, b := sample(1), sample(2)
		for _, c := range []customer.Customer{a, b} {
			if err := r.Create(ctx, c); err != nil {
				t.Fatal(err)
			}
		}
		a.Name, a.Status, a.Address = "Renamed", customer.StatusInactive, customer.Address{}
		a.CreatedAt = base.Add(99 * time.Hour) // must be ignored
		a.UpdatedAt = base.Add(time.Hour)
		if err := r.Update(ctx, a); err != nil {
			t.Fatal(err)
		}
		got, _ := r.Get(ctx, a.ID)
		if got.Name != "Renamed" || got.Status != customer.StatusInactive || got.Address != (customer.Address{}) {
			t.Errorf("update not applied: %+v", got)
		}
		if !got.CreatedAt.Equal(base.Add(time.Minute)) || !got.UpdatedAt.Equal(base.Add(time.Hour)) {
			t.Errorf("timestamps wrong: created=%v updated=%v", got.CreatedAt, got.UpdatedAt)
		}
		a.Email = b.Email
		if err := r.Update(ctx, a); !errors.Is(err, customer.ErrEmailTaken) {
			t.Errorf("e-mail clash: err = %v", err)
		}
		a.Email, a.Document = "other@example.com", b.Document
		if err := r.Update(ctx, a); !errors.Is(err, customer.ErrDocumentTaken) {
			t.Errorf("document clash: err = %v", err)
		}
		ghost := sample(9)
		if err := r.Update(ctx, ghost); !errors.Is(err, customer.ErrNotFound) {
			t.Errorf("unknown id: err = %v", err)
		}
	})

	t.Run("delete removes and frees e-mail", func(t *testing.T) {
		r := newRepo(t)
		c := sample(1)
		_ = r.Create(ctx, c)
		if err := r.Delete(ctx, c.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Get(ctx, c.ID); !errors.Is(err, customer.ErrNotFound) {
			t.Errorf("after delete: err = %v", err)
		}
		if err := r.Delete(ctx, c.ID); !errors.Is(err, customer.ErrNotFound) {
			t.Errorf("second delete: err = %v", err)
		}
		again := sample(1)
		if err := r.Create(ctx, again); err != nil {
			t.Errorf("e-mail should be reusable after delete: %v", err)
		}
	})

	t.Run("list: newest first, pagination, total", func(t *testing.T) {
		r := newRepo(t)
		for i := 1; i <= 5; i++ {
			if err := r.Create(ctx, sample(i)); err != nil {
				t.Fatal(err)
			}
		}
		page, total, err := r.List(ctx, customer.Filter{Page: 1, PageSize: 2})
		if err != nil || total != 5 || len(page) != 2 {
			t.Fatalf("page1: %v total=%d len=%d", err, total, len(page))
		}
		if page[0].Name != "Customer 05" || page[1].Name != "Customer 04" {
			t.Errorf("order: %s, %s", page[0].Name, page[1].Name)
		}
		last, _, _ := r.List(ctx, customer.Filter{Page: 3, PageSize: 2})
		if len(last) != 1 || last[0].Name != "Customer 01" {
			t.Errorf("last page: %+v", last)
		}
		beyond, total, _ := r.List(ctx, customer.Filter{Page: 9, PageSize: 2})
		if len(beyond) != 0 || total != 5 {
			t.Errorf("beyond range: len=%d total=%d", len(beyond), total)
		}
	})

	t.Run("list: equal timestamps fall back to id order, so pages are stable", func(t *testing.T) {
		r := newRepo(t)
		for i := 0; i < 3; i++ {
			c := sample(i + 1)
			c.CreatedAt, c.UpdatedAt = base, base
			if err := r.Create(ctx, c); err != nil {
				t.Fatal(err)
			}
		}
		got, _, err := r.List(ctx, customer.Filter{Page: 1, PageSize: 10})
		if err != nil || len(got) != 3 {
			t.Fatalf("%v len=%d", err, len(got))
		}
		if !(got[0].ID > got[1].ID && got[1].ID > got[2].ID) {
			t.Errorf("ids not in descending order: %s %s %s", got[0].ID, got[1].ID, got[2].ID)
		}
	})

	t.Run("list: status filter and search", func(t *testing.T) {
		r := newRepo(t)
		for i := 1; i <= 4; i++ {
			c := sample(i)
			if i%2 == 0 {
				c.Status = customer.StatusInactive
			}
			_ = r.Create(ctx, c)
		}
		odd := sample(5)
		odd.Name, odd.Email = "100%_Match", "pct@example.com"
		_ = r.Create(ctx, odd)

		cases := []struct {
			name string
			f    customer.Filter
			want int
		}{
			{"inactive only", customer.Filter{Status: customer.StatusInactive}, 2},
			{"active only", customer.Filter{Status: customer.StatusActive}, 3},
			{"name, case-insensitive", customer.Filter{Query: "customer 0"}, 4},
			{"e-mail", customer.Filter{Query: "C03@EXAMPLE"}, 1},
			{"document", customer.Filter{Query: "00000000002"}, 1},
			{"percent and underscore are literals", customer.Filter{Query: "100%_"}, 1},
			{"lone percent is literal", customer.Filter{Query: "%"}, 1},
			{"no match", customer.Filter{Query: "zzz"}, 0},
			{"status + query", customer.Filter{Status: customer.StatusInactive, Query: "04"}, 1},
		}
		for _, tc := range cases {
			tc.f.Page, tc.f.PageSize = 1, 50
			got, total, err := r.List(ctx, tc.f)
			if err != nil || len(got) != tc.want || total != tc.want {
				t.Errorf("%s: err=%v len=%d total=%d want=%d", tc.name, err, len(got), total, tc.want)
			}
		}
	})
}
