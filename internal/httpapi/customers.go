package httpapi

import (
	"net/http"
	"strconv"

	"github.com/andreccls/go-customers-api/internal/customer"
	"github.com/andreccls/go-customers-api/internal/validation"
)

type customerList struct {
	Data     []customer.Customer `json:"data"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
	Total    int                 `json:"total"`
}

func (s *server) createCustomer(w http.ResponseWriter, r *http.Request) {
	var in customer.Input
	if !s.decode(w, r, &in) {
		return
	}
	c, err := s.customers.Create(r.Context(), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/customers/"+c.ID)
	writeJSON(w, http.StatusCreated, c)
}

func (s *server) listCustomers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var verrs validation.Errors
	atoi := func(name string, def int) int {
		v := q.Get(name)
		if v == "" {
			return def
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			verrs.Add(name, "must be an integer")
		}
		return n
	}
	f := customer.Filter{
		Status:   customer.Status(q.Get("status")),
		Query:    q.Get("q"),
		Page:     atoi("page", 1),
		PageSize: atoi("page_size", customer.DefaultPageSize),
	}
	if err := verrs.Err(); err != nil {
		s.fail(w, r, err)
		return
	}
	items, total, err := s.customers.List(r.Context(), f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if items == nil {
		items = []customer.Customer{}
	}
	writeJSON(w, http.StatusOK, customerList{Data: items, Page: f.Page, PageSize: f.PageSize, Total: total})
}

func (s *server) getCustomer(w http.ResponseWriter, r *http.Request) {
	c, err := s.customers.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *server) replaceCustomer(w http.ResponseWriter, r *http.Request) {
	var in customer.Input
	if !s.decode(w, r, &in) {
		return
	}
	c, err := s.customers.Replace(r.Context(), r.PathValue("id"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *server) patchCustomer(w http.ResponseWriter, r *http.Request) {
	var p customer.Patch
	if !s.decode(w, r, &p) {
		return
	}
	c, err := s.customers.Update(r.Context(), r.PathValue("id"), p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *server) deleteCustomer(w http.ResponseWriter, r *http.Request) {
	if err := s.customers.Delete(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
