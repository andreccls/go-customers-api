package memstore

import (
	"testing"

	"github.com/andreccls/go-customers-api/internal/auth"
	"github.com/andreccls/go-customers-api/internal/repotest"
)

func TestUsersContract(t *testing.T) {
	repotest.AuthStore(t, func(*testing.T) auth.Store { return NewUsers() })
}
