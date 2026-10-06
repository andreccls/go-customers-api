# go-customers-api (English summary)

A **reference / example project**: a lean customer-management REST API in **Go 1.24** with **PostgreSQL 16**.
The full documentation is in Portuguese ([README.md](README.md), [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), [docs/adr](docs/adr)).
It is **not production-ready** (see "Limitações conhecidas" in the README).

## What you get

- Customers CRUD: create, get, list (offset pagination, status filter, text search), full (`PUT`) and partial (`PATCH`) update,
  hard delete (admin only). CPF/CNPJ check digits are validated; e-mail and document are unique.
- JWT access tokens (15 min) + opaque, single-use refresh tokens with rotation and reuse detection; bcrypt passwords;
  `admin` / `user` roles; per-IP and per-user in-memory rate limiting.
- Standard library first: `net/http` ServeMux, `log/slog`; only pgx, golang-jwt, bcrypt and uuid as direct dependencies.
  No framework, no ORM. Interfaces are declared by their consumers; handlers are thin.
- RFC 9457 `application/problem+json` errors with stable `code`s; request IDs; graceful shutdown; health/readiness probes;
  release mode (the default) refuses to start with a weak or placeholder `JWT_SECRET`.
- OpenAPI 3 + Swagger UI embedded in the binary (`/docs/`, no CDN). A test compares the router's route table with the spec, and
  every status returned by the HTTP tests must be declared there.
- Tests: table-driven unit tests, a repository **contract suite** run against an in-memory fake *and* a real PostgreSQL, HTTP tests,
  `-race`. 96.9 % total coverage, gated at 90 % (and 100 % on the core packages) in `make test` and CI.
- Distroless, non-root Docker image; `docker-compose.yml` with a PostgreSQL healthcheck; GitHub Actions workflow.

## Run it

```bash
make up          # PostgreSQL 16 + API (Docker only)  ->  http://localhost:8094  (Swagger UI at /docs/)
make demo        # curl tour: register, login, CRUD, pagination, 401/403/409/422/429
make test        # unit + PostgreSQL integration, -race, coverage gates
make lint        # gofmt + go vet + staticcheck
make down
```

No Go toolchain is needed on the host. Dev passwords in `docker-compose.yml` / `.env.example` are **development-only**.

License: [MIT](LICENSE) © André Coura
