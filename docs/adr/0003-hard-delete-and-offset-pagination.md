# ADR 0003 — Remoção definitiva (hard delete) e paginação por offset

- **Status:** aceito
- **Data:** 2026-10-06

## Contexto

**Remoção.** *Soft delete* (`deleted_at`) preserva histórico, mas contamina toda consulta (`WHERE deleted_at IS NULL`),
exige índices únicos parciais para e-mail/documento e mantém **dados pessoais** (nome, CPF, telefone, endereço) depois de
o titular pedir exclusão — algo que a LGPD (art. 18, VI) torna um direito. O conceito de "cliente que parou de comprar"
já existe como `status = inactive`.

**Paginação.** Cursor (keyset) escala melhor para tabelas enormes e é estável sob inserções concorrentes, mas complica o
contrato (token opaco, sem "ir para a página N", sem total). Offset é o que listagens de cadastro tipicamente precisam.

## Decisão

- `DELETE /v1/customers/{id}` apaga a linha (**hard delete**), restrito ao papel `admin`. Desativar é `PATCH {"status":"inactive"}`.
  Um efeito colateral desejável: o e-mail e o documento ficam livres para um novo cadastro.
- Paginação por **offset** (`page`, `page_size` ≤ 100) com ordem **estável** `created_at DESC, id DESC` (o `id` desempata
  timestamps iguais; há índice correspondente) e `total` na resposta. A busca `q` é `ILIKE` escapado em nome/e-mail e `LIKE` no documento.

## Consequências

- (+) Modelo e consultas simples; direito de exclusão atendido.
- (−) Sem trilha de auditoria: quem apagou o quê não fica registrado (apenas o log de acesso, com `user_id` e `request_id`).
  Se for exigido, acrescente uma tabela de eventos — não soft delete.
- (−) Offset fica lento em páginas muito profundas (o banco conta e descarta as linhas anteriores) e uma inserção entre duas
  requisições pode repetir/pular um item. Para dezenas de milhares de clientes é irrelevante; acima disso, migre para keyset
  `(created_at, id) < ($1, $2)` — o índice já existe.
- (−) A busca `q` com `ILIKE '%…%'` faz varredura sequencial; um índice `pg_trgm` resolveria quando importar.
