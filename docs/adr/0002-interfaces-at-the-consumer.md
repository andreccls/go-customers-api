# ADR 0002 — Estrutura de pacotes e interfaces definidas no consumidor

- **Status:** aceito
- **Data:** 2026-10-06

## Contexto

Em Go, "arquitetura hexagonal" costuma virar pastas `domain/ports/adapters/usecases` com uma interface para cada struct
("Java em Go"). O provérbio da linguagem é o oposto: *aceite interfaces, devolva structs* e declare a interface **onde ela é
usada**, com o mínimo de métodos.

## Decisão

Pacotes por **assunto**, não por camada técnica:

| Pacote | Conteúdo | Interface que *declara* (por precisar dela) |
|---|---|---|
| `customer` | modelo, validação, `Service` | `customer.Repository` (5 métodos) |
| `auth` | usuários, bcrypt, JWT, refresh | `auth.Store` |
| `httpapi` | rotas, middleware, JSON, docs | `CustomerService`, `AuthService` |
| `postgres` | implementa `customer.Repository` e `auth.Store` | — |
| `memstore` | idem, em memória (testes) | — |
| `app` | composition root | — |

- `httpapi` **não** importa `postgres`; `customer`/`auth` **não** importam `httpapi` nem `postgres`. Só `app` e `cmd/api`
  conhecem todos. O compilador impede ciclos; não é preciso uma ferramenta para policiar a regra.
- Os handlers chamam o `Service` diretamente: não há camada "use case" separada (o `Service` já é a camada de aplicação) nem
  DTOs duplicados — os tipos do domínio têm tags `json` (`Customer`, `Input`, `Patch`). Se o contrato HTTP divergir do domínio,
  introduza DTOs nesse dia.
- Repositório é **uma interface por recurso**, sem genérico (`Repository[T]`): `Create/Get/List/Update/Delete` de clientes não
  é o mesmo contrato que usuários + refresh tokens.

## Consequências

- (+) Cada interface é pequena e existe porque há dois usos reais (Postgres e memória); fakes nos testes unitários são triviais.
- (+) Adicionar um recurso é copiar um pacote (veja `docs/ARCHITECTURE.md`).
- (−) `customer.Customer` carrega tags `json`, um pequeno vazamento de HTTP para o domínio — troca consciente por menos código.
