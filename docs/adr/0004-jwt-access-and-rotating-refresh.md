# ADR 0004 — JWT de acesso curto + refresh token opaco com rotação

- **Status:** aceito
- **Data:** 2026-10-06

## Contexto

A API precisa autenticar usuários (e-mail + senha) e distinguir `admin` de `user`, sem depender de um provedor externo.
Opções: sessão em cookie, JWT longo, ou JWT curto + refresh. (Para OAuth2 completo com Passport, veja o projeto
`laravel-oauth2-api` deste portfólio.)

## Decisão

- **Senhas:** bcrypt (custo padrão; 8–72 bytes — o bcrypt ignora o que passa de 72). No login com e-mail inexistente, compara-se
  com um hash falso para não revelar, pelo tempo de resposta, quais e-mails existem.
- **Access token:** JWT **HS256**, 15 min, claims `sub`, `role`, `iss`, `iat`, `exp`. A validação **fixa o algoritmo**
  (`WithValidMethods`), exige `exp` e `iss` — barrando `alg: none` e confusão de algoritmos. Sem consulta ao banco por requisição.
- **Refresh token:** 32 bytes aleatórios (opaco), guardado **apenas como SHA-256** no banco, uso único. `POST /v1/auth/refresh`
  consome o token e emite um par novo (**rotação**). Reapresentar um token já usado revoga **todos** os refresh tokens daquele
  usuário (reuso = provável roubo). `POST /v1/auth/logout` revoga um token.
- **Papéis:** `user` lê/escreve clientes; `admin` também remove. `POST /v1/auth/register` cria sempre `user`; o primeiro
  admin vem de `ADMIN_EMAIL`/`ADMIN_PASSWORD` na inicialização (idempotente).
- **Rate limiting:** *token bucket* em memória — por **IP** nas rotas `/v1/auth/*` (10/min) e por **usuário** nas demais (120/min).
  `429` com `Retry-After`. O IP é o do par TCP; `X-Forwarded-For` **não** é confiado (seria forjável).
- **Segredo:** `JWT_SECRET` só por variável de ambiente. Com `APP_ENV=release` (o **padrão**), a API recusa subir se o segredo
  tiver < 32 caracteres, baixa entropia ou parecer placeholder (`dev-only`, `change-me`…); o mesmo vale para `ADMIN_PASSWORD`.

## Consequências

- (+) Roubo de refresh token é detectável; vazamento do banco não entrega tokens utilizáveis.
- (−) **O access token não é revogável**: vale até expirar (15 min) mesmo após logout ou remoção do usuário. Para revogação
  imediata, acrescente uma lista de `jti` revogados ou reduza o TTL.
- (−) HS256 usa um segredo compartilhado; se outros serviços precisarem *verificar* tokens, migre para RS256/EdDSA.
- (−) O limitador é por processo: com N réplicas o limite efetivo é N vezes maior e zera no restart. Em produção, use o
  gateway/WAF ou Redis. Atrás de um proxy/NAT (inclusive o Docker), todos os clientes aparecem com o IP do proxy.
- (−) `register` é aberto (qualquer um cria um `user`); aceitável numa demo, não num sistema real.
