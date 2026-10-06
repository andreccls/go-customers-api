package postgres

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreccls/go-customers-api/internal/customer"
)

// Customers is the PostgreSQL customer.Repository.
type Customers struct{ pool *pgxpool.Pool }

// NewCustomers returns a repository on pool.
func NewCustomers(pool *pgxpool.Pool) *Customers { return &Customers{pool: pool} }

const customerColumns = `id, name, email, document, phone, street, number, city, state, zip_code, status, created_at, updated_at`

func scanCustomer(row pgx.Row) (customer.Customer, error) {
	var c customer.Customer
	var status string
	err := row.Scan(&c.ID, &c.Name, &c.Email, &c.Document, &c.Phone,
		&c.Address.Street, &c.Address.Number, &c.Address.City, &c.Address.State, &c.Address.ZipCode,
		&status, &c.CreatedAt, &c.UpdatedAt)
	c.Status = customer.Status(status)
	c.CreatedAt, c.UpdatedAt = c.CreatedAt.UTC(), c.UpdatedAt.UTC()
	return c, err
}

func mapCustomerErr(err error) error {
	switch constraint, _ := uniqueViolation(err); constraint {
	case "customers_email_key":
		return customer.ErrEmailTaken
	case "customers_document_key":
		return customer.ErrDocumentTaken
	}
	return err
}

func (r *Customers) Create(ctx context.Context, c customer.Customer) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO customers (`+customerColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		c.ID, c.Name, c.Email, c.Document, c.Phone,
		c.Address.Street, c.Address.Number, c.Address.City, c.Address.State, c.Address.ZipCode,
		string(c.Status), c.CreatedAt, c.UpdatedAt)
	return mapCustomerErr(err)
}

func (r *Customers) Get(ctx context.Context, id string) (customer.Customer, error) {
	c, err := scanCustomer(r.pool.QueryRow(ctx, `SELECT `+customerColumns+` FROM customers WHERE id = $1`, id))
	if isNoRows(err) {
		return customer.Customer{}, customer.ErrNotFound
	}
	return c, err
}

func (r *Customers) List(ctx context.Context, f customer.Filter) ([]customer.Customer, int, error) {
	pattern := ""
	if f.Query != "" {
		pattern = "%" + likeEscaper.Replace(f.Query) + "%"
	}
	const where = ` WHERE ($1 = '' OR status = $1)
		AND ($2 = '' OR name ILIKE $2 OR email ILIKE $2 OR document LIKE $2)`

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM customers`+where, string(f.Status), pattern).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT `+customerColumns+` FROM customers`+where+
		` ORDER BY created_at DESC, id DESC LIMIT $3 OFFSET $4`,
		string(f.Status), pattern, f.PageSize, (f.Page-1)*f.PageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []customer.Customer{}
	for rows.Next() {
		c, err := scanCustomer(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

func (r *Customers) Update(ctx context.Context, c customer.Customer) error {
	tag, err := r.pool.Exec(ctx, `UPDATE customers SET name=$2, email=$3, document=$4, phone=$5,
		street=$6, number=$7, city=$8, state=$9, zip_code=$10, status=$11, updated_at=$12 WHERE id=$1`,
		c.ID, c.Name, c.Email, c.Document, c.Phone,
		c.Address.Street, c.Address.Number, c.Address.City, c.Address.State, c.Address.ZipCode,
		string(c.Status), c.UpdatedAt)
	if err != nil {
		return mapCustomerErr(err)
	}
	if tag.RowsAffected() == 0 {
		return customer.ErrNotFound
	}
	return nil
}

func (r *Customers) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM customers WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return customer.ErrNotFound
	}
	return nil
}

// likeEscaper makes %, _ and \ literal inside a LIKE pattern.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
