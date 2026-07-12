# NOTES

## The UI

![Todo board UI](docs/ui.png)

Each user gets a column with their tasks. Every task shows its status, its tags,
and its due date. Overdue tasks are highlighted in red (see Ada's first task).
The dropdown at the top filters tasks by status, and the GraphQL endpoint is
shown in the footer.

## 1. The bug

Running `go run ./cmd/seed` failed with this error:

```
syntax error at or near "\" (SQLSTATE 42601)
```

The users were inserted fine, so the problem was in the query that inserts
tasks. That one query used backticks around the column names (MySQL style),
which Postgres does not accept. It was easy to miss because backticks appear all
over the file for a normal reason — Go uses them to write multi-line strings.

**Fix:** rewrite that one query the same way as the others (a normal Go
multi-line string, no backticks around column names). Re-running seed was
already safe because it clears the tables first.

## 2. Library and project layout

I used `graphql-go/graphql`. It lets me define the schema directly in Go with no
code generation and no extra tooling — a good fit for a schema this size.

The code is split into three parts:

- `internal/store` — all the database code (SQL queries and the data types)
- `internal/gql` — the GraphQL schema, the resolvers, and the HTTP handler
- `cmd/server` — starts everything up

A few things worth pointing out:

- All tags are loaded in **one** query instead of one query per task.
- Asking for a user that doesn't exist returns `null`; trying to create a task
  for a user that doesn't exist returns a clear error. Bad input is rejected
  before it ever reaches the database.
- All four "nice to have" items are done: JSON logs, per-resolver timing
  (turn on with `LOG_LEVEL=debug`), a Dockerfile with an extended
  docker-compose, and tests for the database layer.

## 3. The field I added

I added **`dueDate`**. I picked it because it can be empty, which is a good test:
it proves an empty value travels correctly from the database, through the API,
and into the UI (the due-date chip only appears when a task actually has a date).

I also added two small extras: filtering tasks by status, and highlighting
overdue tasks in red.

## 4. Things I left out (and why)

- Loading each task's user still runs one query per task. A "dataloader" would
  batch these, but it wasn't needed at this scale.
- `dueDate` is sent as plain text rather than a dedicated date type.
- Tests cover the database layer only.
- There's no delete-task mutation.

## 5. How to run it

Everything in Docker (easiest):

```bash
docker compose up --build          # starts Postgres, seeds it, then the API on :8080
```

Or run it locally:

```bash
docker compose up -d postgres      # just the database
go run ./cmd/seed                  # load sample data
go run ./cmd/server                # start the API
```

Then open http://localhost:8080/ for the UI. GraphQL is at `/graphql` and a
health check is at `/healthz`.

Run the tests with `go test ./...` (they skip automatically if Postgres isn't
running). I tested everything with GraphiQL, Postman (including the error
cases), and DBeaver.

---

I used AI tools while building this. I've reviewed all the code and can walk
through any part of it.

Reviewed by: Ajay Varma
