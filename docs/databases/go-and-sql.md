# Go and SQL

## Why care?

Five tickets from Go services that talk to PostgreSQL:

- "Under load, requests hang. The database is at 5% CPU. `pg_stat_activity` shows 20 idle connections."
- "We scaled from 10 to 40 pods. Now new pods fail with `sorry, too many clients already`."
- "A user closed the browser tab. The 30-second report query kept running on the database anyway."
- "Some order lists are silently shorter than they should be. No error in the logs."
- "Under heavy checkout traffic, 5% of orders fail with `could not serialize access`."

None of these is a bug in PostgreSQL. They come from the layer between your code and the database: the **connection pool**, the **context**, the **rows** object, and the **transaction**. This page makes each one fail on purpose, with real runs, and then gives `ordersd` a real PostgreSQL store.

## The idea: a pool of reusable connections

Your service handles requests, and many of them need the database.

- Opening a new connection for every request is slow. It needs a TCP handshake, maybe TLS, authentication, and a new server process on the PostgreSQL side.
- So the service keeps a **connection pool**: a set of open connections. A request borrows one, runs its queries, and returns it to the pool. In Go, the pool is `*sql.DB`.
- The pool has a **maximum size**. With 4 connections and 20 concurrent requests, 16 requests wait for a free connection. The queries do not run faster; the requests just wait.
- Code that borrows a connection and never returns it (no `rows.Close()`) takes that connection out of the pool. After a few of those, the pool is empty and stays empty.
- The database has a limit on connections (`max_connections`). Ten pods with 50 connections each do not fit into a database that allows 100.

```mermaid
flowchart LR
    subgraph app ["ordersd (one pod)"]
        H1[handler] --> P
        H2[handler] --> P
        H3[handler] --> P
        P["*sql.DB pool: idle + in use, max N"]
    end
    P -->|"N TCP connections"| PG["PostgreSQL: one backend process per connection, max_connections = 100"]
```

## Setup

You need the `tutor-pg` container from [How data is stored](storage.md#setup-your-own-postgresql), a database, and a Go module outside any repo:

```console
$ docker exec tutor-pg createdb -U postgres tutor_go
$ mkdir tutorsql && cd tutorsql
$ go mod init example.com/tutorsql
go: creating new go.mod: module example.com/tutorsql
$ go get github.com/jackc/pgx/v5@v5.13.0
go: added github.com/jackc/pgpassfile v1.0.0
go: added github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761
go: added github.com/jackc/pgx/v5 v5.13.0
go: added golang.org/x/text v0.41.0
```

`go list -m -versions github.com/jackc/pgx/v5` shows v5.13.0 as the newest when this was written. Each program below lives in its own `cmd/<name>/main.go` and runs with `go run ./cmd/<name>`.

## Under the hood: `database/sql` and the driver

`database/sql` is the standard library's generic SQL layer. It knows nothing about PostgreSQL. A **driver** does the talking, and **pgx** is the driver to use for PostgreSQL today. Its `stdlib` package registers itself under the name `"pgx"` when you import it:

```go
import (
	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
)

db, err := sql.Open("pgx", "postgres://postgres:tutor@127.0.0.1:55432/tutor_go")
```

Three facts that surprise people:

1. **`sql.Open` doesn't connect.** It only checks the arguments and builds the pool. The first query opens the first connection. To fail fast at start-up, call `db.PingContext` with a timeout.
2. **`*sql.DB` is the pool, not a connection.** Make one per database, at start-up, and share it. It is safe for concurrent use. Opening one per request means a new connection for every request, and it also leaks pools.
3. **Each query borrows a connection and returns it.** Two `db.Exec` calls in a row may run on two different connections. That's why `SET statement_timeout ...` followed by a query, as two calls on `db`, may not do what you want: use a transaction or `db.Conn(ctx)` when statements must share a session.

### Pool settings

Four knobs, from `go doc database/sql.DB`:

| Method                  | Meaning                                        | Default          |
| ----------------------- | ---------------------------------------------- | ---------------- |
| `SetMaxOpenConns(n)`    | most connections at once, in use plus idle     | 0: **unlimited** |
| `SetMaxIdleConns(n)`    | most idle connections kept for reuse           | 2                |
| `SetConnMaxLifetime(d)` | close a connection after it is this old        | 0: forever       |
| `SetConnMaxIdleTime(d)` | close a connection after it was idle this long | 0: forever       |

Both defaults are bad for a service. Unlimited open connections means a traffic spike opens hundreds, and PostgreSQL refuses them past `max_connections`. Two idle connections means that after the spike, all but two are closed, and the next spike opens them again (the same churn as `MaxIdleConnsPerHost` in [TCP](../networking/tcp.md)). Set `MaxIdleConns` equal to `MaxOpenConns`, and set a lifetime, so connections move to new database servers after a failover and load balancers can drain them.

Watch a pool of 4 handle 20 queries of 100 ms each:

```go
// Command pool runs 20 slow queries at once through a pool of 4
// connections and prints what the pool saw.
package main

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
)

func main() {
	db, err := sql.Open("pgx", "postgres://postgres:tutor@127.0.0.1:55432/tutor_go")
	if err != nil {
		panic(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)

	start := time.Now()
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if _, err := db.Exec("SELECT pg_sleep(0.1)"); err != nil {
				panic(err)
			}
		})
	}
	wg.Wait()
	s := db.Stats()
	fmt.Printf("20 queries of 100 ms in %v\n", time.Since(start).Round(10*time.Millisecond))
	fmt.Printf("open=%d inUse=%d idle=%d waitCount=%d waitDuration=%v\n",
		s.OpenConnections, s.InUse, s.Idle, s.WaitCount, s.WaitDuration.Round(10*time.Millisecond))
}
```

```console
$ go run ./cmd/pool
20 queries of 100 ms in 540ms
open=4 inUse=0 idle=4 waitCount=16 waitDuration=4.54s
```

- 20 queries through 4 connections: 5 rounds of 100 ms, plus connection set-up. 540 ms.
- `waitCount=16`: 16 of the 20 had to wait for a free connection. `waitDuration=4.54s` is the **sum** of all their waits.
- This is ticket 1. The database is idle, but requests wait **in your process**, in the pool. Export `db.Stats()` as metrics (`WaitCount` and `WaitDuration` growing means the pool is too small, or connections are held too long), and look at it before blaming the database.

### How big should the pool be?

Start from the database, not the app. This server allows:

```console
$ docker exec tutor-pg psql -U postgres -Atc 'SHOW max_connections'
100
```

Every connection is a PostgreSQL **process** with its own memory, and a few are reserved for superusers. More connections than CPU cores doing work at once makes the server slower, not faster: they compete for CPU, locks and memory. A useful starting point for the whole database is a few times the number of cores, not hundreds.

Then divide: **pods × `MaxOpenConns` must stay below `max_connections`**, with room for migrations, cron jobs and your own `psql`. 10 pods × 10 connections = 100: already too many here. That's ticket 2, which arrives the day the HPA (see [Scheduling and resources](../kubernetes/scheduling-and-resources.md)) scales you out.

When you need more clients than the database can hold, put **PgBouncer** in between. It is a small proxy that keeps a few real connections to PostgreSQL and hands them out to many client connections. In **transaction pooling** mode, a client gets a server connection only for the length of one transaction. That is efficient, but anything tied to a session breaks: `SET` without `LOCAL`, advisory locks held across transactions, `LISTEN`, temporary tables. Prepared statements also need care: pgx by default prepares and caches every statement on its connection (`default_query_exec_mode=cache_statement`). The pgx docs say this "may be incompatible with proxies such as PgBouncer unless they are configured to support protocol-level prepared statements" and suggest `exec` or `simple_protocol` mode in that case; recent PgBouncer versions support them with `max_prepared_statements`. Managed databases often have PgBouncer built in.

## Context: timeouts and cancellation

Every method has a `...Context` version. Use those, always, with the request's context (see [Context](../go/context.md)). When the context ends, pgx stops waiting **and** asks PostgreSQL to stop the query:

```go
// Command timeout runs a 5-second query with a 300 ms deadline, then
// asks the server what happened to it.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	db, err := sql.Open("pgx", "postgres://postgres:tutor@127.0.0.1:55432/tutor_go?application_name=tutor-timeout")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = db.ExecContext(ctx, "SELECT pg_sleep(5)")
	fmt.Printf("after %v: %v\n", time.Since(start).Round(time.Millisecond), err)
	fmt.Println("errors.Is(err, context.DeadlineExceeded):", errors.Is(err, context.DeadlineExceeded))
	var pgErr *pgconn.PgError
	fmt.Println("errors.As(err, *pgconn.PgError):", errors.As(err, &pgErr))

	var n int
	db.QueryRow(`SELECT count(*) FROM pg_stat_activity
		WHERE application_name = 'tutor-timeout' AND query = 'SELECT pg_sleep(5)' AND state = 'active'`).Scan(&n)
	fmt.Println("pg_sleep(5) still running on the server:", n)
	s := db.Stats()
	fmt.Printf("pool: open=%d idle=%d\n", s.OpenConnections, s.Idle)
}
```

```console
$ go run ./cmd/timeout
after 302ms: timeout: context deadline exceeded
errors.Is(err, context.DeadlineExceeded): true
errors.As(err, *pgconn.PgError): false
pg_sleep(5) still running on the server: 0
pool: open=1 idle=1
$ docker logs tutor-pg 2>&1 | grep -B1 'STATEMENT:  SELECT pg_sleep(5)'
2026-11-26 22:25:40.789 UTC [2101] ERROR:  canceling statement due to user request
2026-11-26 22:25:40.789 UTC [2101] STATEMENT:  SELECT pg_sleep(5)
```

- The call returned at 302 ms, with an error that `errors.Is` matches as `context.DeadlineExceeded`. It is **not** a `*pgconn.PgError`: the error came from the client side. Map it to a 503 or 504, not a 500.
- The server **really stopped**: the log says `canceling statement due to user request`, and no `pg_sleep(5)` is left in `pg_stat_activity`. pgx sent a PostgreSQL cancel request on a separate connection. So ticket 3 is fixed by passing `r.Context()` down: when the client goes away, the query goes away too.
- `db.Exec` without `Context` uses `context.Background()`: no deadline, ever. A linter rule against the non-context methods is worth having.

Context deadlines protect one call. Also set a server-side `statement_timeout` for the app's role (see [MVCC and locks](mvcc-and-locks.md#timeouts)), as a safety net for code paths that forget.

## Rows: always `Close`, always check `Err`

`db.Query` returns `*sql.Rows`, and **the connection stays busy until the rows are closed**. `rows.Next()` closes them when it reaches the end, but an early `return` doesn't. This function returns as soon as it finds a match, and forgot `defer rows.Close()`:

```go
// Command leak forgets rows.Close on an early return, and runs out of
// connections after two calls.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// firstPaid returns the first paid order's customer. The bug: the early
// return skips rows.Close, so the connection stays busy.
func firstPaid(ctx context.Context, db *sql.DB) (string, error) {
	rows, err := db.QueryContext(ctx, "SELECT 'c-' || g, 'paid' FROM generate_series(1, 1000) g")
	if err != nil {
		return "", err
	}
	// missing: defer rows.Close()
	for rows.Next() {
		var customer, status string
		if err := rows.Scan(&customer, &status); err != nil {
			return "", err
		}
		if status == "paid" {
			return customer, nil // rows still open: 999 rows unread
		}
	}
	return "", rows.Err()
}

func main() {
	db, err := sql.Open("pgx", "postgres://postgres:tutor@127.0.0.1:55432/tutor_go")
	if err != nil {
		panic(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(2)

	for i := 1; i <= 3; i++ {
		ctx := context.Background() // no deadline: nothing ever closes the rows
		if i == 3 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, time.Second)
			defer cancel()
		}
		start := time.Now()
		c, err := firstPaid(ctx, db)
		s := db.Stats()
		fmt.Printf("call %d after %v: %q err=%v (inUse=%d waitCount=%d)\n",
			i, time.Since(start).Round(time.Millisecond), c, err, s.InUse, s.WaitCount)
	}
}
```

```console
$ go run ./cmd/leak
call 1 after 10ms: "c-1" err=<nil> (inUse=1 waitCount=0)
call 2 after 9ms: "c-1" err=<nil> (inUse=2 waitCount=0)
call 3 after 1.001s: "" err=context deadline exceeded (inUse=2 waitCount=1)
```

Each call leaks one connection (`inUse` goes 1, 2). The third call finds the pool empty, waits, and gives up after its one-second deadline. Without that deadline, it would wait **forever**, and so would every request after it. In production that looks exactly like ticket 1: requests hang, and the database is idle.

One detail softens this: `database/sql` closes the rows by itself when the query's **context ends**. In an earlier version of this program, each call had its own context that was cancelled right after the call, and `inUse` stayed at 1. Don't rely on it. `defer rows.Close()` right after the error check, every time.

The second half of the rule: **`rows.Next()` returns `false` both at the end and on an error.** Only `rows.Err()` tells you which. This query fails at its fourth row:

```go
// Command rowserr reads a result that fails in the middle, once
// without checking rows.Err and once with it.
package main

import (
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const q = "SELECT 100 / (4 - g) FROM generate_series(1, 6) g" // fails at g = 4

func sum(db *sql.DB, checkErr bool) (int, error) {
	rows, err := db.Query(q)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	total := 0
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return 0, err
		}
		total += n
	}
	if checkErr {
		if err := rows.Err(); err != nil {
			return 0, err
		}
	}
	return total, nil
}

func main() {
	db, err := sql.Open("pgx", "postgres://postgres:tutor@127.0.0.1:55432/tutor_go")
	if err != nil {
		panic(err)
	}
	defer db.Close()
	fmt.Println(sum(db, false))
	fmt.Println(sum(db, true))
}
```

```console
$ go run ./cmd/rowserr
183 <nil>
0 ERROR: division by zero (SQLSTATE 22012)
```

Without `rows.Err()`, the function returned **183** (33 + 50 + 100, the first three rows) and **no error**: a wrong answer that looks right. That's ticket 4. In production, the error in the middle is a network reset, a `statement_timeout`, or a failover, and your list is silently cut short.

`QueryRow(...).Scan(...)` closes for you. It returns `sql.ErrNoRows` when there's no row; check it with `errors.Is`.

## Transactions, and retrying them

`db.BeginTx` returns a `*sql.Tx` that is pinned to **one** connection until `Commit` or `Rollback`. Run every statement of the transaction on `tx`, never on `db`: a `db.Exec` in the middle runs on another connection, outside the transaction. The standard shape is "defer `Rollback`, then `Commit` at the end"; `Rollback` after a successful `Commit` does nothing.

In [Transactions and isolation](transactions.md), `REPEATABLE READ` and `SERIALIZABLE` transactions fail with SQLSTATE `40001` (`serialization_failure`) when they lose a race, and [MVCC and locks](mvcc-and-locks.md#under-the-hood-deadlocks) adds `40P01` (`deadlock_detected`). Both mean "nothing happened, run the whole transaction again". So the retry loop belongs **around the whole transaction**, not around one statement. [Transactions and isolation](transactions.md#serialization-failures-and-retry-loops) used such a loop for two sessions and one write skew. Here is a reusable version (it takes the isolation level, also retries deadlocks, and stops when the context ends), and what happens to it under real contention, eight goroutines on one hot row:

```go
// retryable reports whether Postgres asks us to run the transaction again:
// 40001 serialization_failure, 40P01 deadlock_detected.
func retryable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "40001" || pgErr.Code == "40P01")
}

var retries atomic.Int64

// inTx runs fn in a transaction and retries it, up to
// maxAttempts times, when Postgres reports a serialization failure.
func inTx(ctx context.Context, db *sql.DB, iso sql.IsolationLevel, maxAttempts int, fn func(*sql.Tx) error) error {
	for attempt := 1; ; attempt++ {
		err := func() error {
			tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: iso})
			if err != nil {
				return err
			}
			defer tx.Rollback() // no-op after Commit
			if err := fn(tx); err != nil {
				return err
			}
			return tx.Commit()
		}()
		if err == nil || !retryable(err) || attempt == maxAttempts {
			return err
		}
		retries.Add(1)
		// Back off a little, with jitter, so the same transactions don't collide again.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(rand.N(time.Duration(attempt) * 10 * time.Millisecond)):
		}
	}
}

func takeOne(tx *sql.Tx) error {
	var q int
	if err := tx.QueryRow("SELECT quantity FROM stock WHERE sku = 'A'").Scan(&q); err != nil {
		return err
	}
	_, err := tx.Exec("UPDATE stock SET quantity = $1 WHERE sku = 'A'", q-1)
	return err
}

// takeOneAtomic lets the database do read and write in one statement.
func takeOneAtomic(tx *sql.Tx) error {
	_, err := tx.Exec("UPDATE stock SET quantity = quantity - 1 WHERE sku = 'A'")
	return err
}
```

`main` (in the same file, `cmd/retry`) creates `CREATE TABLE stock (sku text PRIMARY KEY, quantity int NOT NULL CHECK (quantity >= 0))` with 1,000 of SKU `A`, and starts 8 goroutines that each take one item 25 times: 200 in total, so 800 should be left. Three modes: `once` is `takeOne` in `SERIALIZABLE` with no retry, `retry` is the same with up to 10 attempts, and `atomic` is `takeOneAtomic` in `READ COMMITTED`:

```console
$ go run ./cmd/retry once
once    quantity=972 (want 800), failed=172, retries=0
last error: ERROR: could not serialize access due to concurrent update (SQLSTATE 40001)
$ go run ./cmd/retry retry
retry   quantity=805 (want 800), failed=5, retries=129
last error: ERROR: could not serialize access due to concurrent update (SQLSTATE 40001)
$ go run ./cmd/retry atomic
atomic  quantity=800 (want 800), failed=0, retries=0
```

- **`once`**: 172 of 200 attempts failed with `40001`. The data is still correct (972 = 1000 − 28 successes): `SERIALIZABLE` refused every lost update instead of letting it through. But the users saw errors.
- **`retry`**: 129 retries, and 5 operations still failed after 10 attempts each. A retry loop is **required** with these isolation levels, but it doesn't make a hot row cheap. Eight workers that compete for one row keep colliding.
- **`atomic`**: no failures, no retries. One `UPDATE ... SET quantity = quantity - 1` reads and writes under the row lock, so concurrent updates queue up instead of conflicting. When you can push the logic into one statement (or use `SELECT ... FOR UPDATE`, see [MVCC and locks](mvcc-and-locks.md)), do it. Keep `SERIALIZABLE` plus retries for logic that can't be expressed that way.

Rules for the retry loop:

- **Retry the whole function**, including the reads. The decision you made from the old read is what was wrong.
- **No side effects inside `fn`.** No HTTP calls, no messages to a queue, no emails. They would happen once per attempt. Do them after `Commit`, or use an outbox table (see [Distributed systems](../distributed-systems/index.md)).
- **Bounded attempts, with jitter**, and stop when the context ends.
- **Only `40001` and `40P01`.** A unique violation (`23505`) or a check violation (`23514`) won't go away on retry.

## pgx without `database/sql`: `pgxpool`

pgx also has its own API, and its own pool, `pgxpool`. It skips the `database/sql` layer:

```go
pool, err := pgxpool.New(ctx, "postgres://postgres:tutor@127.0.0.1:55432/tutor_go?pool_max_conns=10")
if err != nil {
	return err
}
defer pool.Close()
var n int
err = pool.QueryRow(ctx, "SELECT count(*) FROM orders WHERE status = $1", "paid").Scan(&n)
```

| You want                                                          | Use                              |
| ----------------------------------------------------------------- | -------------------------------- |
| Code that works with any SQL database, tools that take `*sql.DB`  | `database/sql` + `pgx/v5/stdlib` |
| PostgreSQL features: `COPY`, batches, `LISTEN/NOTIFY`, more types | `pgxpool` and the pgx API        |
| A bit less overhead per query                                     | `pgxpool`                        |

The pool settings are in the connection string or `pgxpool.Config`. From `go doc github.com/jackc/pgx/v5/pgxpool.ParseConfig`: `pool_max_conns` defaults to "the greater of 4 or `runtime.NumCPU()`", `pool_max_conn_lifetime` to 1 hour, `pool_max_conn_idle_time` to 30 minutes. Unlike `database/sql`, it's limited by default. `pool.Stat()` gives the same kind of numbers as `db.Stats()`: `AcquireCount`, `EmptyAcquireCount` (had to wait), `AcquireDuration`. Pick one style per service; both are fine.

## sqlc: generate the query code

The Scan code in the store below is repetitive and easy to get wrong: a column added in the wrong place, a type that doesn't match. **sqlc** (v1.31.1 when this was written) reads your schema and your SQL queries, checks them, and **generates** typed Go functions. You write SQL; it writes the Go:

```sql
-- queries/orders.sql
-- name: GetOrder :one
SELECT id, customer_id, status, created_at FROM orders WHERE id = $1;

-- name: ListOrdersByCustomer :many
SELECT id, customer_id, status, created_at FROM orders
WHERE customer_id = $1 ORDER BY created_at;
```

```yaml
# sqlc.yaml
version: "2"
sql:
  - engine: postgresql
    schema: internal/storage/postgres/schema.sql
    queries: queries
    gen:
      go:
        package: pgdb
        out: internal/storage/postgres/pgdb
        sql_package: database/sql
```

```sh
sqlc generate
```

<!-- TODO(real-output): sqlc generate output and the generated GetOrder function. Running the downloaded sqlc tool was not done in this session (running a downloaded tool with `go run ...@version` was refused for goose). Expect files db.go, models.go and orders.sql.go in internal/storage/postgres/pgdb, with a `func (q *Queries) GetOrder(ctx context.Context, id uuid.UUID) (Order, error)`; check how the generated code maps `uuid` (sqlc may use github.com/google/uuid by default, which needs an override to the standard library `uuid` package). -->

sqlc gives you a `Queries` type with one method per query, plus a struct per table. A typo in a column name fails at `sqlc generate`, in CI, not in production. The SQL stays plain SQL: you can paste it into `psql` and `EXPLAIN` it. The cost is a code-generation step, and dynamic queries (filters the user picks) still need hand-written SQL. Keep the generated code in a package behind your `Repository`, like any other storage detail.

## A PostgreSQL store for `ordersd`

The orders service from the [Go chapter](../go/index.md) keeps orders in memory, behind the `order.Repository` interface in `examples/orders/internal/order/service.go`. Its wiring comment says: "Swapping the store for a database means changing this one line." Here is the store. First the schema:

```sql
-- internal/storage/postgres/schema.sql
CREATE TABLE IF NOT EXISTS orders (
  id          uuid        PRIMARY KEY,
  customer_id text        NOT NULL,
  status      text        NOT NULL,
  created_at  timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS order_items (
  order_id    uuid   NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
  line        int    NOT NULL,
  sku         text   NOT NULL,
  quantity    int    NOT NULL CHECK (quantity > 0),
  price_cents bigint NOT NULL CHECK (price_cents >= 0),
  PRIMARY KEY (order_id, line)
);
```

(In a real service, this is the first migration, see [Schema migrations](migrations.md).) Then the store, `internal/storage/postgres/postgres.go`:

```go
// Package postgres is an order.Repository backed by PostgreSQL, through
// database/sql and the pgx driver.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/acme/orders/internal/order"
)

// Store is an order.Repository on a *sql.DB. It is safe for concurrent use.
type Store struct {
	db *sql.DB
}

// New returns a Store that uses db. The caller owns db and closes it.
func New(db *sql.DB) *Store { return &Store{db: db} }

var _ order.Repository = (*Store)(nil)

// Create implements order.Repository. The order and its items are written
// in one transaction: either all rows are there, or none.
func (s *Store) Create(ctx context.Context, o order.Order) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after a successful Commit

	_, err = tx.ExecContext(ctx,
		`INSERT INTO orders (id, customer_id, status, created_at) VALUES ($1, $2, $3, $4)`,
		o.ID, o.CustomerID, o.Status, o.CreatedAt)
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
		return fmt.Errorf("order %s already exists", o.ID)
	}
	if err != nil {
		return err
	}
	for i, it := range o.Items {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO order_items (order_id, line, sku, quantity, price_cents) VALUES ($1, $2, $3, $4, $5)`,
			o.ID, i, it.SKU, it.Quantity, it.PriceCents); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Get implements order.Repository.
func (s *Store) Get(ctx context.Context, id uuid.UUID) (order.Order, error) {
	o := order.Order{ID: id}
	err := s.db.QueryRowContext(ctx,
		`SELECT customer_id, status, created_at FROM orders WHERE id = $1`, id).
		Scan(&o.CustomerID, &o.Status, &o.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return order.Order{}, order.ErrNotFound
	}
	if err != nil {
		return order.Order{}, err
	}
	o.CreatedAt = o.CreatedAt.UTC() // pgx returns timestamptz in time.Local
	items, err := loadItems(ctx, s.db, `WHERE order_id = $1`, id)
	if err != nil {
		return order.Order{}, err
	}
	o.Items = items[id]
	return o, nil
}

// List implements order.Repository. Two queries, not one per order (an
// N+1 loop costs a round trip per order). They run in one REPEATABLE READ
// transaction, so both see the same snapshot: no order without its items.
func (s *Store) List(ctx context.Context) ([]order.Order, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx,
		`SELECT id, customer_id, status, created_at FROM orders ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []order.Order
	for rows.Next() {
		var o order.Order
		if err := rows.Scan(&o.ID, &o.CustomerID, &o.Status, &o.CreatedAt); err != nil {
			return nil, err
		}
		o.CreatedAt = o.CreatedAt.UTC()
		all = append(all, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items, err := loadItems(ctx, tx, ``)
	if err != nil {
		return nil, err
	}
	for i := range all {
		all[i].Items = items[all[i].ID]
	}
	return all, tx.Commit()
}

// querier is what *sql.DB and *sql.Tx have in common.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// loadItems loads order lines, grouped by order ID, in line order.
func loadItems(ctx context.Context, q querier, where string, args ...any) (map[uuid.UUID][]order.Item, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT order_id, sku, quantity, price_cents FROM order_items `+where+` ORDER BY order_id, line`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[uuid.UUID][]order.Item)
	for rows.Next() {
		var id uuid.UUID
		var it order.Item
		if err := rows.Scan(&id, &it.SKU, &it.Quantity, &it.PriceCents); err != nil {
			return nil, err
		}
		m[id] = append(m[id], it)
	}
	return m, rows.Err()
}
```

What to notice:

- **Parameters, always** (`$1`, `$2`). Never build SQL with `fmt.Sprintf` from input: that is SQL injection (see [Security](../security/index.md)). The `where` string in `loadItems` is a constant from this file, never user input.
- **`Create` is one transaction**: the order and its items together. A test below proves that a failing item leaves no half order behind.
- **`List` reads with two queries in one snapshot.** Without the `REPEATABLE READ` transaction, an order created between the two queries would show up without items.
- **`uuid.UUID` from the standard library works as a parameter and a scan target** with pgx v5.13.0, as the tests show.
- **The time zone.** The first version didn't have the `.UTC()` lines. The API then returned `"created_at":"2026-11-27T07:00:00+01:00"` for an order stored as `06:00:00+00`: the same moment, but pgx returns `timestamptz` in the process's local time zone (`time.Local`), and this laptop is at UTC+1. The memory store and the domain use UTC, so the store converts.

Wire it in `cmd/ordersd/main.go`, in place of `memory.New()`:

```go
	var repo order.Repository = memory.New()
	if dsn := getenv("ORDERS_DATABASE_URL"); dsn != "" {
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			return err
		}
		defer db.Close()
		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(10)
		db.SetConnMaxLifetime(30 * time.Minute)
		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := db.PingContext(pingCtx); err != nil {
			return fmt.Errorf("connect to database: %w", err)
		}
		repo = postgres.New(db)
	}
	svc := order.NewService(repo)
```

(plus the imports `database/sql`, `.../internal/storage/postgres` and `_ "github.com/jackc/pgx/v5/stdlib"`). In a real service, the DSN comes from the config package and a Secret, see [Configuration and secrets](../kubernetes/config-and-secrets.md). Create the schema, add an order with `psql`, and ask the service:

```console
$ docker exec -i tutor-pg psql -U postgres -d tutor_go -q < internal/storage/postgres/schema.sql
$ docker exec tutor-pg psql -U postgres -d tutor_go \
    -c "INSERT INTO orders VALUES ('019ac3e4-0000-7000-8000-000000000001', 'c-42', 'paid', '2026-11-27 06:00:00+00')" \
    -c "INSERT INTO order_items VALUES ('019ac3e4-0000-7000-8000-000000000001', 0, 'BOOK-1', 2, 1999), ('019ac3e4-0000-7000-8000-000000000001', 1, 'PEN-7', 1, 250)"
INSERT 0 1
INSERT 0 2
$ ORDERS_DATABASE_URL=postgres://postgres:tutor@127.0.0.1:55432/tutor_go ./bin/ordersd -addr 127.0.0.1:18081 &
$ curl -s 127.0.0.1:18081/v1/orders/019ac3e4-0000-7000-8000-000000000001
{"id":"019ac3e4-0000-7000-8000-000000000001","customer_id":"c-42","items":[{"sku":"BOOK-1","quantity":2,"price_cents":1999},{"sku":"PEN-7","quantity":1,"price_cents":250}],"status":"paid","total_cents":4248,"created_at":"2026-11-27T06:00:00Z"}
$ curl -s 127.0.0.1:18081/v1/orders/019ac3e4-0000-7000-8000-000000000002
{"type":"about:blank","title":"Not Found","status":404,"detail":"order not found","request_id":"01a21b93-9ca3-7755-b38f-4602a0edc081"}
```

The HTTP layer, the domain, and the error mapping (`ErrNotFound` to 404) didn't change at all. That's what the `Repository` interface was for.

## Testing with a real database

A fake repository can't tell you that your SQL is right. The [integration tests](../go/testing.md#integration-tests) pattern from the Go chapter applies: a build tag, an environment variable with the database address, and a skip when it's missing. To let tests run in parallel without seeing each other's rows, each test gets its **own schema**:

```go
//go:build integration

package postgres_test

import (
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
	"uuid"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/acme/orders/internal/order"
	"github.com/acme/orders/internal/storage/postgres"
)

//go:embed schema.sql
var schema string

// openTestDB connects to ORDERS_TEST_DATABASE_URL and gives the test its own, empty
// schema, so tests can run in parallel and never see each other's rows.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("ORDERS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ORDERS_TEST_DATABASE_URL not set")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	name := fmt.Sprintf("test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec("DROP SCHEMA " + name + " CASCADE") })

	db, err := sql.Open("pgx", dsn+"?search_path="+name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCreateIsAtomic(t *testing.T) {
	t.Parallel()
	s := postgres.New(openTestDB(t))
	o := order.Order{
		ID: uuid.NewV7(), CustomerID: "c-1", Status: order.StatusPending, CreatedAt: time.Now(),
		Items: []order.Item{{SKU: "A", Quantity: 1}, {SKU: "B", Quantity: 0}}, // 0 breaks the CHECK
	}
	if err := s.Create(t.Context(), o); err == nil {
		t.Fatal("Create() error = nil, want a CHECK violation")
	}
	if _, err := s.Get(t.Context(), o.ID); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("after a failed Create, Get() error = %v, want ErrNotFound", err)
	}
}
```

The same file has `TestCreateGetList` (create, read back, list, and a duplicate ID must fail) and `TestGetNotFound`. Cleanups run in reverse order, so the test's own pool is closed before the schema is dropped.

```console
$ go test ./internal/storage/postgres
?   	github.com/acme/orders/internal/storage/postgres	[no test files]
$ ORDERS_TEST_DATABASE_URL=postgres://postgres:tutor@127.0.0.1:55432/tutor_go \
    go test -tags integration -race -count=1 -v ./internal/storage/postgres
...
--- PASS: TestGetNotFound (0.06s)
--- PASS: TestCreateIsAtomic (0.07s)
--- PASS: TestCreateGetList (0.08s)
PASS
ok  	github.com/acme/orders/internal/storage/postgres	1.087s
```

Without the tag, the file isn't even compiled. With it, three tests run in parallel against real PostgreSQL in about 70 ms each. In CI, the database is a service container next to the job, or the test starts one with **testcontainers-go** (v0.45.0 when this was written, with a `modules/postgres` package), which runs a throwaway container from inside `go test`. Either way: **the same PostgreSQL major version as production**, because behavior (like the `NOT NULL NOT VALID` from [Schema migrations](migrations.md)) changes between versions.

## Try it

1. Run `cmd/pool` with `SetMaxOpenConns(20)`, then with `SetMaxOpenConns(1)`, and compare the time and `waitDuration`.
2. Run `cmd/timeout`, then look for the cancel in `docker logs tutor-pg`.
3. Fix `cmd/leak` with `defer rows.Close()` and run it again: all three calls should take milliseconds.
4. Run `cmd/retry` in all three modes. Then change the goroutines to 2 and see how the failure count changes.
5. Run the integration tests, then `docker exec tutor-pg psql -U postgres -d tutor_go -c '\dn'` to check no `test_` schema is left behind.
6. Clean up: `docker exec tutor-pg dropdb -U postgres tutor_go`.

## Traps

- **Default pool settings.** Unlimited open connections and only 2 idle. Set `MaxOpenConns`, set `MaxIdleConns` to the same value, set a lifetime.
- **Pods × pool size > `max_connections`.** Scaling out breaks the database. Do the math, or use PgBouncer.
- **Methods without `Context`.** No deadline, no cancel. A hung query holds its connection forever.
- **Forgetting `rows.Close()`.** One leaked connection per call, until the pool is empty and everything hangs. `defer rows.Close()` right after the error check.
- **Not checking `rows.Err()`.** A result cut short by an error looks like a short, valid result.
- **Using `db` inside a transaction.** The statement runs on another connection, outside the transaction, and may even wait on a lock the transaction holds: a deadlock inside your own process.
- **No retry for `40001`/`40P01`**, or a retry with side effects inside it, or a retry around one statement instead of the whole transaction.
- **Prepared statements and schema changes.** pgx caches prepared statements per connection. Its docs warn that after a schema change, "the first execution of a previously cached query may fail", e.g. when the columns of a `SELECT *` change. Another reason to list columns explicitly, and to follow [expand and contract](migrations.md#expand-and-contract-old-and-new-code-at-the-same-time).
- **`timestamptz` comes back in `time.Local`.** Convert to UTC in the store if your domain expects it, or set `TZ` in the container.
- **Session state through PgBouncer.** In transaction mode, `SET`, advisory locks and `LISTEN` don't stick to "your" connection.

## Check yourself

??? question "1. `db.Stats()` shows `InUse=10`, `Idle=0`, `MaxOpenConnections=10`, and `WaitCount` is growing fast. PostgreSQL is at 3% CPU. Name two possible causes and how you'd tell them apart."

    The pool is exhausted. Either the pool is simply too small for the load (connections are busy
    with real, short queries), or connections leak or are held too long: a missing
    `rows.Close()`, a transaction left open, slow work between `BEGIN` and `COMMIT`. Look at
    `pg_stat_activity` for those 10 connections: `active` with queries means real load; `idle` or
    `idle in transaction` while the app counts them as in use means a leak or long-held transactions.

??? question "2. A request's context is cancelled while its query runs. What happens in the client and on the server?"

    The call returns at once with an error that `errors.Is` matches as `context.Canceled` (or
    `DeadlineExceeded`). pgx sends a cancel request, and the server stops the query with
    `canceling statement due to user request`. The work is not left running.

??? question "3. Why did the `rowserr` program return 183 and no error, and what's the one-line fix?"

    `rows.Next()` returned `false` when the division by zero arrived, the same as at the end of
    the result, so the loop simply stopped. The error was waiting in `rows.Err()`, which nobody
    checked. Fix: `if err := rows.Err(); err != nil { return 0, err }` after the loop.

??? question "4. Your retry loop retries `tx.Exec` when it fails with `40001`. Why is that wrong?"

    After a serialization failure, the whole transaction is aborted: further statements on it
    fail. And the values you read earlier in the transaction are what caused the conflict. You
    must roll back and run the **whole** transaction again, reads included.

??? question "5. You run 30 pods with `SetMaxOpenConns(20)` against a database with `max_connections = 200`. What do you change?"

    30 × 20 = 600 possible connections, three times the limit. Options: lower the pool to about
    5 per pod (150 total, leaving room for migrations and admin), and check `WaitDuration` stays
    low; or put PgBouncer in transaction mode between the pods and the database, so 600 client
    connections share a few dozen server connections. Raising `max_connections` a lot costs
    memory and usually makes the database slower.

??? question "6. Why does `Store.List` use a `REPEATABLE READ` transaction for two read-only queries?"

    So both queries see one snapshot. In `READ COMMITTED`, each statement gets a new snapshot: an
    order committed between the two queries would appear in the first result but its items not
    in the second. `REPEATABLE READ` gives both statements the snapshot of the first one.

## Recap

- `*sql.DB` is a **pool**. One per database, made at start-up. `sql.Open` doesn't connect; `PingContext` does.
- Set **`MaxOpenConns`**, **`MaxIdleConns`** (same value), and **`ConnMaxLifetime`**. Watch `db.Stats()`: `WaitCount` and `WaitDuration`.
- **pods × pool size < `max_connections`**. Beyond that, PgBouncer in transaction mode, with care for session state and prepared statements.
- Use the **`Context`** methods. A cancelled context stops the query on the server too.
- **`defer rows.Close()`** and **check `rows.Err()`**. A leak empties the pool; an unchecked error gives a wrong answer.
- Run every statement of a transaction on the **`tx`**. Retry the **whole** transaction on `40001` and `40P01`, with a limit and jitter, and no side effects inside. Single-statement updates avoid many conflicts.
- `pgxpool` is the pgx-native alternative; sqlc generates the Scan code from SQL.
- Test the store against **real PostgreSQL**, one schema per test, behind a build tag.

Next: what happens when one database server isn't enough. [Replication and scaling](replication.md).
