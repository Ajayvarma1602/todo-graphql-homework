# NOTES

## UI

![Todo board UI](docs/ui.png)

Three user columns with per-task status badges, tag chips, `dueDate` chips, and
overdue highlighting (Ada's `due 2026-07-01` in red). Status filter top-left;
GraphQL endpoint in the footer.

## 1. The bug

`go run ./cmd/seed` failed with `syntax error at or near "\" (SQLSTATE 42601)` on
Ada's first task — so users had inserted fine and the tasks `INSERT` was the
suspect. Grepping for backticks showed every query is a Go raw string except that
one: a double-quoted string with MySQL-style backticks around the column names,
which Postgres rejects. It hides well because backticks are all over the file
legitimately as raw-string delimiters.

**Fix:** raw string, unquoted column names. Re-runs were already safe
(`TRUNCATE ... RESTART IDENTITY` before insert).

## 2. Library / structure

`graphql-go/graphql`: schema in Go code, no codegen, no extra dependencies —
right size for this schema.

Layout:
- `internal/store` — all SQL, plain structs, `ErrNotFound`
- `internal/gql` — schema, resolvers, HTTP handler, timing extension
- `cmd/server` — wiring

Enum casing, ID string↔int64, and date formatting all translate at the gql
boundary. Tags load in one `IN (...)` query, not per task. Missing id on a query
→ `null`; missing id on a mutation → error; bad input errors before hitting the
DB.

All four nice-to-haves are in: slog JSON logs with request timing, resolver
timing behind `LOG_LEVEL=debug`, Dockerfile + extended compose
(postgres → seed → api), store integration tests.

## 3. New field

`dueDate` — it's nullable, so it proves a NULL survives DB → API → UI (chip
renders only when a date exists). Small extras: a status filter using the
`tasks(status:)` argument, and overdue highlighting.

## 4. Tradeoffs

- `Task.user` is still N+1 (a dataloader would fix it)
- no pagination
- `dueDate` is a `String`, not a `Date` scalar
- tests cover the store only
- CORS is `*`
- no delete mutation

## 5. How to run

```bash
docker compose up --build          # postgres → seed → API on :8080
```

or locally:

```bash
docker compose up -d postgres
go run ./cmd/seed
go run ./cmd/server                # http://localhost:8080/ (UI, /graphql, /healthz)
```

Tests: `go test ./...` (skip if Postgres is down). Verified with GraphiQL,
Postman (including error paths), and DBeaver.

---

I used AI tool while building this; I've reviewed the code and can walk through any of it.

Reviewed by: Ajay Varma 
