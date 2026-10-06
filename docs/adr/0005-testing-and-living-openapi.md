# ADR 0005 — Testes, cobertura e OpenAPI que não desatualiza

- **Status:** aceito
- **Data:** 2026-10-06

## Contexto

Quer-se confiança real (inclusive no SQL), um gate de cobertura honesto e documentação da API que **não possa** divergir do
código — e tudo isso reproduzível num clone limpo, sem `.env` local.

## Decisão

1. **Stdlib `testing`**, tabelas de casos, `httptest`. Sem frameworks de teste/mocks: *fakes* em memória (`internal/memstore`).
2. **Teste de contrato compartilhado** (`internal/repotest`): a mesma suíte roda contra o `memstore` **e** contra o PostgreSQL
   real. O fake usado nos testes unitários, portanto, não pode divergir do banco (unicidade, ordem, busca com `%`/`_`, expiração…).
3. **Integração com PostgreSQL real** via `docker compose` + `TEST_DATABASE_URL` (não Testcontainers: ele precisa do
   `docker.sock`, e o `make test` roda *dentro* de um container). Cada teste cria um **schema isolado** (`search_path`) e o
   derruba no fim — roda em paralelo e não toca nos dados de desenvolvimento. Com `REQUIRE_DB=1` (Makefile e CI) a falta do banco
   **falha** o teste em vez de pulá-lo: suíte pulada não é build verde.
4. **OpenAPI escrito à mão** (`internal/httpapi/openapi.json`, embutido no binário) + **tabela de rotas única** que monta o
   roteador. `TestOpenAPIMatchesRouter` compara as duas (mesmas operações; `security` ⇔ rota autenticada; `403` ⇔ admin; `429` ⇔
   rate limit; corpo ⇔ POST/PUT/PATCH) e todo teste HTTP verifica que o status devolvido **está declarado** na spec para aquela
   operação. É determinístico: não lê `.env`, rede nem banco. (Gerar a spec a partir do código exigiria anotações ou reflexão;
   uma spec manual vigiada por teste é mais simples e legível.)
5. **Swagger UI embutido** (`go:embed`, Apache-2.0, versão em `swagger/VERSION`): `/docs` funciona offline, com CSP `default-src 'self'`.
6. **Cobertura** (`-coverpkg`, perfil único): gate de **≥ 90 % no total** e **100 %** em `customer`, `validation` e `ratelimit`
   (**98 %** em `auth`: o único statement fora é o `return err` de `jwt.SignedString`, que não falha com chave HS256).
   Excluídos da medição, por serem apoio de teste: `internal/testdb` e `internal/repotest`; e `cmd/api` (só liga sinais e `os.Exit`).
7. `go test -race` sempre; `go vet` + `staticcheck` + `gofmt` no `make lint`.

## Consequências

- (+) Mudar rota sem atualizar a spec (ou o contrário) quebra o build; regressões de SQL aparecem no banco real.
- (−) Escrever o JSON da spec à mão é verboso. O ganho é controle total de descrições e exemplos.
- (−) Os testes de integração exigem PostgreSQL (`make test` o sobe sozinho); `make test-unit` roda sem banco, mas sem o gate.
- (−) 100 % de cobertura não prova ausência de bugs — prova que nada do núcleo ficou sem ser exercitado.
