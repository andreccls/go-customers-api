package customer_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/andreccls/go-customers-api/internal/customer"
	"github.com/andreccls/go-customers-api/internal/memstore"
	"github.com/andreccls/go-customers-api/internal/validation"
)

var ctx = context.Background()

func clock() func() time.Time {
	t := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	return func() time.Time { t = t.Add(time.Minute); return t }
}

func newService() *customer.Service { return customer.NewService(memstore.NewCustomers(), clock()) }

func valid() customer.Input {
	return customer.Input{
		Name: "Maria Silva", Email: "maria@example.com", Document: "529.982.247-25",
		Phone: "(31) 99999-0000",
		Address: customer.Address{Street: "Rua A", Number: "10", City: "Belo Horizonte", State: "mg", ZipCode: "30130-000"},
	}
}

func fieldsOf(t *testing.T, err error) map[string]bool {
	t.Helper()
	var v validation.Errors
	if !errors.As(err, &v) {
		t.Fatalf("expected validation errors, got %v", err)
	}
	m := map[string]bool{}
	for _, fe := range v {
		m[fe.Field] = true
	}
	return m
}

func TestCreateNormalizesAndStores(t *testing.T) {
	s := newService()
	c, err := s.Create(ctx, valid())
	if err != nil {
		t.Fatal(err)
	}
	if _, perr := uuid.Parse(c.ID); perr != nil {
		t.Errorf("id is not a UUID: %q", c.ID)
	}
	if c.Document != "52998224725" || c.Phone != "31999990000" || c.Address.State != "MG" || c.Address.ZipCode != "30130000" {
		t.Errorf("not normalized: %+v", c)
	}
	if c.Status != customer.StatusActive || !c.CreatedAt.Equal(c.UpdatedAt) {
		t.Errorf("defaults wrong: %+v", c)
	}
	got, err := s.Get(ctx, c.ID)
	if err != nil || got.ID != c.ID {
		t.Errorf("Get = %+v, %v", got, err)
	}
}

func TestCreateWithoutOptionalFields(t *testing.T) {
	in := valid()
	in.Phone, in.Address, in.Status = "", customer.Address{}, customer.StatusInactive
	c, err := newService().Create(ctx, in)
	if err != nil || c.Status != customer.StatusInactive || c.Address != (customer.Address{}) {
		t.Errorf("%+v, %v", c, err)
	}
}

func TestCreateValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*customer.Input)
		field  string
	}{
		{"short name", func(i *customer.Input) { i.Name = " a " }, "name"},
		{"long name", func(i *customer.Input) { i.Name = strings.Repeat("x", 121) }, "name"},
		{"bad email", func(i *customer.Input) { i.Email = "nope" }, "email"},
		{"bad document", func(i *customer.Input) { i.Document = "123" }, "document"},
		{"phone too short", func(i *customer.Input) { i.Phone = "12345" }, "phone"},
		{"phone with letters", func(i *customer.Input) { i.Phone = "3199999000a" }, "phone"},
		{"bad status", func(i *customer.Input) { i.Status = "banned" }, "status"},
		{"address street", func(i *customer.Input) { i.Address.Street = "" }, "address.street"},
		{"address number", func(i *customer.Input) { i.Address.Number = "" }, "address.number"},
		{"address city", func(i *customer.Input) { i.Address.City = " " }, "address.city"},
		{"address state length", func(i *customer.Input) { i.Address.State = "MGG" }, "address.state"},
		{"address state digits", func(i *customer.Input) { i.Address.State = "1A" }, "address.state"},
		{"address zip", func(i *customer.Input) { i.Address.ZipCode = "123" }, "address.zip_code"},
		{"address zip letters", func(i *customer.Input) { i.Address.ZipCode = "3013000a0" }, "address.zip_code"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := valid()
			tt.mutate(&in)
			_, err := newService().Create(ctx, in)
			if !fieldsOf(t, err)[tt.field] {
				t.Errorf("expected an error on %q, got %v", tt.field, err)
			}
		})
	}
}

func TestCreateReportsEveryInvalidField(t *testing.T) {
	_, err := newService().Create(ctx, customer.Input{})
	f := fieldsOf(t, err)
	if !f["name"] || !f["email"] || !f["document"] {
		t.Errorf("fields = %v", f)
	}
}

func TestUniqueness(t *testing.T) {
	s := newService()
	if _, err := s.Create(ctx, valid()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, valid()); !errors.Is(err, customer.ErrEmailTaken) {
		t.Errorf("err = %v", err)
	}
	other := valid()
	other.Email = "other@example.com"
	if _, err := s.Create(ctx, other); !errors.Is(err, customer.ErrDocumentTaken) {
		t.Errorf("err = %v", err)
	}
}

func TestInvalidIDIsNotFound(t *testing.T) {
	s := newService()
	if _, err := s.Get(ctx, "not-a-uuid"); !errors.Is(err, customer.ErrNotFound) {
		t.Errorf("Get: %v", err)
	}
	if err := s.Delete(ctx, "not-a-uuid"); !errors.Is(err, customer.ErrNotFound) {
		t.Errorf("Delete: %v", err)
	}
	if _, err := s.Replace(ctx, "x", valid()); !errors.Is(err, customer.ErrNotFound) {
		t.Errorf("Replace: %v", err)
	}
	if _, err := s.Update(ctx, "x", customer.Patch{}); !errors.Is(err, customer.ErrNotFound) {
		t.Errorf("Update: %v", err)
	}
	if _, err := s.Get(ctx, uuid.NewString()); !errors.Is(err, customer.ErrNotFound) {
		t.Errorf("Get unknown: %v", err)
	}
}

func TestReplace(t *testing.T) {
	s := newService()
	c, _ := s.Create(ctx, valid())
	in := valid()
	in.Name, in.Phone, in.Address, in.Status = "Maria S. Costa", "", customer.Address{}, customer.StatusInactive
	got, err := s.Replace(ctx, c.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Maria S. Costa" || got.Phone != "" || got.Address != (customer.Address{}) || got.Status != customer.StatusInactive {
		t.Errorf("not replaced: %+v", got)
	}
	if !got.UpdatedAt.After(c.UpdatedAt) || !got.CreatedAt.Equal(c.CreatedAt) {
		t.Errorf("timestamps: %+v vs %+v", got, c)
	}
	if _, err := s.Replace(ctx, c.ID, customer.Input{}); err == nil {
		t.Error("invalid replace must fail")
	}
	if _, err := s.Replace(ctx, uuid.NewString(), valid()); !errors.Is(err, customer.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
}

func TestPatchChangesOnlyGivenFields(t *testing.T) {
	s := newService()
	c, _ := s.Create(ctx, valid())
	name, email, doc, phone := "Maria Souza", "MARIA.S@example.com", "11.222.333/0001-81", ""
	addr := customer.Address{Street: "Av B", Number: "S/N", City: "Contagem", State: "MG", ZipCode: "32000000"}
	st := customer.StatusInactive

	got, err := s.Update(ctx, c.ID, customer.Patch{Name: &name, Email: &email, Document: &doc, Phone: &phone, Address: &addr, Status: &st})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != name || got.Email != "maria.s@example.com" || got.Document != "11222333000181" ||
		got.Phone != "" || got.Address != addr || got.Status != st {
		t.Errorf("patch not applied: %+v", got)
	}

	// An empty patch is a no-op apart from updated_at.
	same, err := s.Update(ctx, c.ID, customer.Patch{})
	if err != nil || same.Name != name || same.Status != st {
		t.Errorf("empty patch changed data: %+v, %v", same, err)
	}

	bad := "nope"
	if _, err := s.Update(ctx, c.ID, customer.Patch{Email: &bad}); !fieldsOf(t, err)["email"] {
		t.Errorf("invalid patch accepted: %v", err)
	}
	if _, err := s.Update(ctx, uuid.NewString(), customer.Patch{}); !errors.Is(err, customer.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
}

func TestDelete(t *testing.T) {
	s := newService()
	c, _ := s.Create(ctx, valid())
	if err := s.Delete(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, c.ID); !errors.Is(err, customer.ErrNotFound) {
		t.Errorf("still there: %v", err)
	}
}

func TestListDefaultsAndValidation(t *testing.T) {
	s := newService()
	for i, doc := range []string{"52998224725", "11222333000181", "00000003700"} {
		in := valid()
		in.Email = string(rune('a'+i)) + "@example.com"
		in.Document = doc
		if _, err := s.Create(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	page, total, err := s.List(ctx, customer.Filter{Query: "  a@example "})
	if err != nil || total != 1 || len(page) != 1 {
		t.Fatalf("defaults + trimmed query: %v total=%d len=%d", err, total, len(page))
	}
	_, _, err = s.List(ctx, customer.Filter{Page: -1, PageSize: 101, Status: "x"})
	f := fieldsOf(t, err)
	if !f["page"] || !f["page_size"] || !f["status"] {
		t.Errorf("fields = %v", f)
	}
	if _, _, err := s.List(ctx, customer.Filter{PageSize: -5}); !fieldsOf(t, err)["page_size"] {
		t.Errorf("negative page_size accepted: %v", err)
	}
}

// failingRepo shows the service passes storage errors through untouched.
type failingRepo struct {
	customer.Repository
	err error
}

func (f failingRepo) Create(context.Context, customer.Customer) error { return f.err }
func (f failingRepo) Update(context.Context, customer.Customer) error { return f.err }

func TestStorageErrorsPropagate(t *testing.T) {
	boom := errors.New("boom")
	mem := memstore.NewCustomers()
	seed, _ := customer.NewService(mem, nil).Create(ctx, valid()) // also covers the default clock
	s := customer.NewService(failingRepo{Repository: mem, err: boom}, nil)
	if _, err := s.Create(ctx, valid()); !errors.Is(err, boom) {
		t.Errorf("Create: %v", err)
	}
	if _, err := s.Replace(ctx, seed.ID, valid()); !errors.Is(err, boom) {
		t.Errorf("Replace: %v", err)
	}
	if _, err := s.Update(ctx, seed.ID, customer.Patch{}); !errors.Is(err, boom) {
		t.Errorf("Update: %v", err)
	}
}
