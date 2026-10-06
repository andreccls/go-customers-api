# ADR 0001 — Biblioteca padrão primeiro, poucas dependências

- **Status:** aceito
- **Data:** 2026-10-06

## Contexto

A API é pequena (CRUD + autenticação). Frameworks web (Gin, Echo, Fiber), ORMs (GORM) e bibliotecas de validação por
tags adicionam superfície, "mágica" e estilo emprestado de outras linguagens. Desde o Go 1.22, o `http.ServeMux` entende
métodos e parâmetros de caminho (`GET /v1/customers/{id}`, `r.PathValue("id")`).

## Decisão

- **HTTP:** `net/http` + `ServeMux`. Middleware é `func(http.Handler) http.Handler`. Logs com `log/slog` (JSON), JSON com
  `encoding/json`.
- **Dependências diretas (4):** `pgx/v5` (driver PostgreSQL), `golang-jwt/jwt/v5` (assinatura/validação de JWT — não se
  escreve criptografia à mão), `golang.org/x/crypto/bcrypt` e `google/uuid`.
- **Validação manual** em vez de `go-playground/validator`: as regras são poucas, mas **não** são "de tag" (dígito
  verificador de CPF/CNPJ, normalização de telefone, endereço tudo-ou-nada). Código simples que devolve uma lista de
  `{field, message}` é mais legível que tags + mensagens customizadas, e a normalização acontece no mesmo lugar.
- **Sem ORM:** SQL explícito com `pgx`. A tabela é uma só; o SQL cabe na tela e é o que realmente roda.
- **Migrations:** arquivos `.sql` embutidos (`embed`) e um runner de ~60 linhas (tabela `schema_migrations`, uma transação
  por arquivo, `pg_advisory_lock` para duas instâncias subindo juntas). Só "up": veja Consequências.

## Consequências

- (+) Binário estático pequeno, build rápido, pouco para atualizar/auditar.
- (+) O código lê como Go: `if err != nil`, interfaces pequenas, sem reflexão escondida.
- (−) Escreve-se um pouco de código que um framework daria pronto (decodificação JSON estrita, mapa erro→HTTP, paginação).
  Está concentrado em `httpapi/problem.go` e `customers.go`, e é testado.
- (−) O runner de migrations não tem *down* nem detecta *drift* de arquivos já aplicados. Para um projeto maior, troque
  por `golang-migrate`/`goose`/`atlas`; a interface é só `Migrate(ctx, pool)`, então a troca fica em `internal/postgres`.
- (−) O `ServeMux` não responde `405` com `Allow` (cai no handler `/` e vira `404 route_not_found` em JSON). Aceitável aqui.
