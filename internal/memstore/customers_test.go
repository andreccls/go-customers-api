package memstore

import (
	"testing"

	"github.com/andreccls/go-customers-api/internal/customer"
	"github.com/andreccls/go-customers-api/internal/repotest"
)

func TestCustomersContract(t *testing.T) {
	repotest.Customers(t, func(*testing.T) customer.Repository { return NewCustomers() })
}
