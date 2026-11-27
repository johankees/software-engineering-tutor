# Schema migrations

## Why care?

Four messages from the deploy channel:

- "We added one nullable column. The deploy took 7 seconds, and for those 7 seconds every request to `orders` hung."
- "`CREATE INDEX` on the big table ran for 20 minutes. Checkout was down the whole time. Reads were fine."
- "The migration passed in staging. In production it failed halfway, and now the index is there, but `INVALID`."
- "The new version renamed a column. For two minutes, the old pods still running threw `column does not exist`."

A schema migration is a change to the database's structure (`CREATE`, `ALTER`, `DROP`), run while the service is live. Almost all trouble comes from two facts. **DDL takes strong locks**, and locks wait in a **queue**. And **old and new code run at the same time** during a deploy, so the schema must work for both. This page shows both facts with real sessions, and the recipes that work around them.

Setup: the `tutor-pg` container from [How data is stored](storage.md#setup-your-own-postgresql), and a database for this page:

```sh
docker exec tutor-pg createdb -U postgres tutor_migrations
```

The examples use an `orders` table with 2 million rows (135 MB), like a small production table:

```sql
CREATE TABLE orders (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  customer_id text NOT NULL,
  status      text NOT NULL DEFAULT 'pending',
  created_at  timestamptz NOT NULL DEFAULT now()
);
INSERT INTO orders (customer_id, status, created_at)
SELECT 'c-' || (g % 50000), (ARRAY['pending','paid','shipped','cancelled'])[1 + g % 4],
       now() - (g || ' seconds')::interval
FROM generate_series(1, 2000000) g;
VACUUM ANALYZE orders;
```

## The idea: renovating a shop while it is open

You own a busy shop and want to move a shelf. To do that safely, you need the aisle empty: no customers in it.

- You put up a sign "aisle closed, wait here" and wait for the customers who are already in the aisle to leave.
- New customers arrive. They see the sign and **wait behind it**, even the ones who only want to look.
- One customer in the aisle is on a long phone call. He doesn't leave for ten minutes. Now the queue is out of the door, and **you haven't even started moving the shelf**.

The shelf move takes 2 seconds. The queue costs ten minutes. A good shop owner gives up after a few seconds, takes the sign down, lets the queue through, and tries again later. And for big jobs he builds the new shelf next to the old one, moves goods over a bit at a time, and removes the old shelf at the end.

That's the whole page: `ALTER TABLE` is the sign, a long transaction is the phone call, `lock_timeout` is giving up, and **expand and contract** is the new shelf next to the old one.

```mermaid
flowchart LR
    A["report: open transaction, holds ACCESS SHARE"] --> B["migration: ALTER TABLE wants ACCESS EXCLUSIVE, waits"]
    B --> C["app: SELECT wants ACCESS SHARE, waits behind the migration"]
    B --> D["app: INSERT waits"]
    B --> E["app: every new query waits"]
```

## Under the hood: the lock queue

The lock modes and what conflicts with what are in [MVCC and locks](mvcc-and-locks.md#under-the-hood-table-locks). The one that matters here: almost every `ALTER TABLE` takes `ACCESS EXCLUSIVE`, which conflicts with **everything**, even a plain `SELECT`. And the one rule that makes it dangerous: **a waiting lock request blocks the requests that come after it**. PostgreSQL grants locks in order, so a `SELECT` can't jump ahead of a waiting `ALTER`, even though the `SELECT` would not conflict with the report that holds the lock now.

See it with three sessions. A script starts them one second apart:

```sh
#!/bin/sh
# Three sessions: a long report, a migration, an ordinary app query.
P="docker exec -i tutor-pg psql -U postgres -d tutor_migrations -X -q"
$P -c "SET application_name = 'report'" -c "BEGIN" \
   -c "SELECT count(*) FROM orders WHERE status = 'paid'" \
   -c "SELECT pg_sleep(8)" -c "COMMIT" > a.out 2>&1 &
sleep 1
$P <<SQL > b.out 2>&1 &
SET application_name = 'migration';
\timing on
ALTER TABLE orders ADD COLUMN note text;
SQL
sleep 1
$P <<SQL > c.out 2>&1 &
SET application_name = 'app';
\timing on
SELECT status FROM orders WHERE customer_id = 'c-42' LIMIT 1;
SQL
sleep 1
$P <<SQL
SELECT pid, application_name AS app, state, wait_event_type AS wait,
       pg_blocking_pids(pid) AS blocked_by, left(query, 45) AS query
FROM pg_stat_activity
WHERE datname = 'tutor_migrations' AND pid <> pg_backend_pid() ORDER BY backend_start;
SELECT a.application_name AS app, l.mode, l.granted
FROM pg_locks l JOIN pg_stat_activity a USING (pid)
WHERE l.relation = 'orders'::regclass ORDER BY a.backend_start;
SQL
wait
echo '--- migration:'; cat b.out; echo '--- app:'; cat c.out
```

```console
$ ./lockq.sh
 pid |    app    | state  |  wait   | blocked_by |                     query
-----+-----------+--------+---------+------------+-----------------------------------------------
 290 | report    | active | Timeout | {}         | SELECT pg_sleep(8)
 299 | migration | active | Lock    | {290}      | ALTER TABLE orders ADD COLUMN note text;
 331 | app       | active | Lock    | {299}      | SELECT status FROM orders WHERE customer_id =
(3 rows)

    app    |        mode         | granted
-----------+---------------------+---------
 report    | AccessShareLock     | t
 migration | AccessExclusiveLock | f
 app       | AccessShareLock     | f
(3 rows)

--- migration:
Time: 7087.681 ms (00:07.088)
--- app:
 status
---------
 shipped
(1 row)

Time: 6080.598 ms (00:06.081)
```

Read it line by line:

- The **report** holds `AccessShareLock`, granted. It is sleeping, not even reading. Any open transaction that touched `orders` holds this lock until it ends, also one that is `idle in transaction`.
- The **migration** waits for `AccessExclusiveLock`. `blocked_by {290}`: the report.
- The **app** query wants only `AccessShareLock`, the same mode the report has. Those two don't conflict. But it is `blocked_by {299}`: **the migration**, which doesn't even have its lock yet.
- Adding a nullable column is a metadata change that takes about a millisecond. It "took" 7 seconds, and a query that should take 1 ms took 6 seconds. With a 30-minute report instead of an 8-second sleep, the service is down for 30 minutes. Every new request piles up behind the migration, your connection pool fills, and health checks start failing.

`pg_blocking_pids(pid)` is the function to remember. During an incident, run the first query above and follow the `blocked_by` chain to its end. The process at the end, the one blocked by nobody, is the cause.

### `lock_timeout`: give up quickly, try again

The fix is to make the migration give up. Same script, with one more line before the `ALTER`:

```sql
SET lock_timeout = '2s';
ALTER TABLE orders ADD COLUMN note text;
```

```console
$ ./lockq2.sh
...
--- migration:
Time: 0.124 ms
ERROR:  canceling statement due to lock timeout
Time: 2015.404 ms (00:02.015)
--- app:
 status
---------
 shipped
(1 row)

Time: 989.474 ms
```

(The `...` is the same `pg_stat_activity` picture as before.) The app query waited one second, not six: it arrived one second after the migration started waiting, and the migration gave up at two. The cost of a failed attempt is now bounded: at most `lock_timeout` of stalled traffic.

Then retry. A failed `ALTER` changed nothing, so it is safe to run again. In a deploy script:

```sh
for attempt in 1 2 3 4 5; do
  if psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -c "SET lock_timeout = '2s'" -f 0042_add_note.sql; then
    exit 0
  fi
  echo "attempt $attempt failed, retrying in $((attempt * 5))s" >&2
  sleep $((attempt * 5))
done
exit 1
```

Rules that follow from the queue:

- **Every migration sets `lock_timeout`.** 1 to 5 seconds, depending on how much stall your service can take. Put it at the top of the migration file, or set it for the migration role: `ALTER ROLE migrator SET lock_timeout = '3s'`.
- **One table per transaction, if you can.** A migration that changes three tables in one transaction holds the first table's `ACCESS EXCLUSIVE` lock while it waits for the second.
- **Find the phone call.** Long transactions block migrations, and `VACUUM` too (see [MVCC and locks](mvcc-and-locks.md#the-long-transaction-that-stops-vacuum)). `idle_in_transaction_session_timeout` ends sessions that sit idle inside a transaction.
- **Don't fight autovacuum.** An `ALTER` that conflicts with a running autovacuum doesn't wait long: after `deadlock_timeout` (1 s), the autovacuum cancels itself. You see `ERROR: canceling autovacuum task` in the log, and your migration takes one second longer. That's normal. An autovacuum that runs "to prevent wraparound" does **not** cancel itself.

## Adding a column

Adding a column with a constant default, or no default, is a **metadata-only** change since PostgreSQL 11. The table is not rewritten: PostgreSQL stores the default once in the catalog and hands it out for old rows that don't have the column on disk. A **volatile** default (one that is different for each row, like `clock_timestamp()` or `random()`) must be computed per row, so PostgreSQL rewrites the whole table, under `ACCESS EXCLUSIVE`, the whole time.

```sql
\timing on
SELECT pg_relation_filenode('orders') AS filenode, pg_size_pretty(pg_table_size('orders')) AS size;
ALTER TABLE orders ADD COLUMN currency text NOT NULL DEFAULT 'EUR';
SELECT pg_relation_filenode('orders') AS filenode;
SELECT attname, atthasmissing, attmissingval FROM pg_attribute
WHERE attrelid = 'orders'::regclass AND attname = 'currency';
ALTER TABLE orders ADD COLUMN updated_at timestamptz NOT NULL DEFAULT clock_timestamp();
SELECT pg_relation_filenode('orders') AS filenode, pg_size_pretty(pg_table_size('orders')) AS size;
```

```console
 filenode |  size
----------+--------
    16645 | 135 MB
(1 row)

Time: 2.543 ms
 filenode
----------
    16645
(1 row)

 attname  | atthasmissing | attmissingval
----------+---------------+---------------
 currency | t             | {EUR}
(1 row)

Time: 2683.271 ms (00:02.683)
 filenode |  size
----------+--------
    16663 | 166 MB
(1 row)
```

(Timing lines for the small `SELECT`s are left out.)

- `currency ... DEFAULT 'EUR'`: **2.5 ms** for 2 million rows. The file is the same (`filenode` 16645). `atthasmissing = t` and `attmissingval = {EUR}`: "rows without this column on disk have the value `EUR`".
- `updated_at ... DEFAULT clock_timestamp()`: **2.7 seconds**, and a new file (16663, 166 MB). That was a full rewrite. On a 100 GB table it is an hour of `ACCESS EXCLUSIVE`: an outage.

The file you're looking at is the heap from [How data is stored](storage.md). A new `filenode` means PostgreSQL wrote a complete new copy of the table. Other changes that rewrite: changing a column's type in most cases (`ALTER COLUMN ... TYPE`, except a few safe ones like `varchar(50)` to `varchar(100)` or to `text`), adding a stored generated column, and `VACUUM FULL`. If you're not sure, try it on a copy and compare `pg_relation_filenode` before and after.

To get a per-row value without a rewrite: add the column with no default (or a constant one), set the default for new rows, and backfill the old rows in batches (below).

## Building an index: `CONCURRENTLY`

A plain `CREATE INDEX` takes a `SHARE` lock: reads go on, **writes wait** until the build is done. `CREATE INDEX CONCURRENTLY` takes `SHARE UPDATE EXCLUSIVE`, which allows writes. A script starts the build, and half a second later inserts one order:

```console
$ ./idx.sh "CREATE INDEX orders_customer_idx ON orders (customer_id);"
  backend_type   |                  query                   |       mode       | granted
-----------------+------------------------------------------+------------------+---------
 client backend  | CREATE INDEX orders_customer_idx ON orde | ShareLock        | t
 client backend  | INSERT INTO orders (customer_id) VALUES  | RowExclusiveLock | f
 parallel worker | CREATE INDEX orders_customer_idx ON orde | ShareLock        | t
(3 rows)

--- index build:
Time: 2101.444 ms (00:02.101)
--- insert:
Time: 1575.412 ms (00:01.575)
$ ./idx.sh "CREATE INDEX CONCURRENTLY orders_customer_idx ON orders (customer_id);"
  backend_type   |                  query                   |           mode           | granted
-----------------+------------------------------------------+--------------------------+---------
 client backend  | CREATE INDEX CONCURRENTLY orders_custome | ShareUpdateExclusiveLock | t
 parallel worker | CREATE INDEX CONCURRENTLY orders_custome | ShareUpdateExclusiveLock | t
(2 rows)

--- index build:
Time: 2304.562 ms (00:02.305)
--- insert:
Time: 2.217 ms
```

The insert waited 1.6 seconds for the plain build, and 2 ms during the concurrent one. (The build also used a parallel worker, which holds the same lock.) On a real table, the plain build is minutes to hours of no checkout.

`CONCURRENTLY` is slower (it scans the table twice and waits for older transactions in between), and it has rules:

```sql
-- invalid.sql
BEGIN;
CREATE INDEX CONCURRENTLY orders_status_idx ON orders (status);
ROLLBACK;
CREATE UNIQUE INDEX CONCURRENTLY orders_customer_uniq ON orders (customer_id);
\d orders
```

```console
$ docker exec -i tutor-pg psql -U postgres -d tutor_migrations < invalid.sql
BEGIN
ERROR:  CREATE INDEX CONCURRENTLY cannot run inside a transaction block
ROLLBACK
ERROR:  could not create unique index "orders_customer_uniq"
DETAIL:  Key (customer_id)=(c-18573) is duplicated.
...
Indexes:
    "orders_pkey" PRIMARY KEY, btree (id)
    "orders_customer_idx" btree (customer_id)
    "orders_customer_uniq" UNIQUE, btree (customer_id) INVALID
```

- **No transaction.** Most migration tools wrap each migration in a transaction. You must turn that off for this one migration (goose: `-- +goose NO TRANSACTION`, below).
- **A failed build leaves an `INVALID` index behind.** It isn't used for reads, but **it is still updated on every write**, so it costs you. Find them with `SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid`, drop with `DROP INDEX CONCURRENTLY`, then build again. Write the migration as `CREATE INDEX CONCURRENTLY IF NOT EXISTS`? Careful: on a retry, `IF NOT EXISTS` sees the invalid index and does nothing. Drop invalid leftovers first.
- **It still waits for a lock at the start**, and for old transactions to finish in the middle. So `lock_timeout` still matters.

The same idea exists for other operations: `DROP INDEX CONCURRENTLY`, `REINDEX CONCURRENTLY`, and `DETACH PARTITION CONCURRENTLY` (see [Replication and scaling](replication.md)). Which index to build is the topic of [Indexes](indexes.md).

## Adding `NOT NULL` and other constraints

`ALTER COLUMN ... SET NOT NULL` must check that no row has a NULL, so it scans the whole table, under `ACCESS EXCLUSIVE`. The safe way splits "promise" from "check":

1. **Add the constraint as `NOT VALID`.** It is checked for new and changed rows from now on, but old rows are not scanned. This needs `ACCESS EXCLUSIVE`, but only for milliseconds.
2. **`VALIDATE CONSTRAINT`.** It scans the old rows, under the weaker `SHARE UPDATE EXCLUSIVE` lock: reads and writes go on.

Up to PostgreSQL 17, you do that with a `CHECK` constraint, and then `SET NOT NULL` uses the validated check as proof and skips the scan. Timed on the 2-million-row table, with a `total_cents` column that was just filled:

```console
tutor_migrations=# ALTER TABLE orders ALTER COLUMN total_cents SET NOT NULL;
ALTER TABLE
Time: 935.889 ms
tutor_migrations=# ALTER TABLE orders ALTER COLUMN total_cents DROP NOT NULL;
ALTER TABLE
Time: 2.414 ms
tutor_migrations=# ALTER TABLE orders ADD CONSTRAINT orders_total_not_null CHECK (total_cents IS NOT NULL) NOT VALID;
ALTER TABLE
Time: 1.684 ms
tutor_migrations=# ALTER TABLE orders VALIDATE CONSTRAINT orders_total_not_null;
ALTER TABLE
Time: 184.659 ms
tutor_migrations=# ALTER TABLE orders ALTER COLUMN total_cents SET NOT NULL;
ALTER TABLE
Time: 1.686 ms
tutor_migrations=# ALTER TABLE orders DROP CONSTRAINT orders_total_not_null;
ALTER TABLE
Time: 1.505 ms
```

The direct `SET NOT NULL` held `ACCESS EXCLUSIVE` for 936 ms; on a 100 GB table, that's minutes. The safe path holds it for about 2 ms per step. The scan (185 ms) ran under a lock that lets traffic through.

**PostgreSQL 18 can do it in one step**: `NOT NULL` is now a real, named constraint, and it can be `NOT VALID` too:

```console
tutor_migrations=# ALTER TABLE orders ADD CONSTRAINT orders_total_not_null NOT NULL total_cents NOT VALID;
ALTER TABLE
Time: 2.116 ms
tutor_migrations=# SELECT conname, contype, convalidated FROM pg_constraint WHERE conname = 'orders_total_not_null';
        conname        | contype | convalidated
-----------------------+---------+--------------
 orders_total_not_null | n       | f
(1 row)

tutor_migrations=# INSERT INTO orders (customer_id) VALUES ('c-new');
ERROR:  null value in column "total_cents" of relation "orders" violates not-null constraint
DETAIL:  Failing row contains (01a219f4-467d-7708-90ad-ba910ae9b320, c-new, pending, 2026-11-26 22:22:48.957279+00, EUR, 2026-11-26 22:22:48.957452+00, null).
tutor_migrations=# ALTER TABLE orders VALIDATE CONSTRAINT orders_total_not_null;
ALTER TABLE
Time: 230.851 ms
```

New rows are checked right away (the `INSERT` failed), old rows by `VALIDATE`. Checking the locks inside a transaction (`SELECT mode FROM pg_locks WHERE relation = 'orders'::regclass AND pid = pg_backend_pid()`) shows `AccessExclusiveLock` for the `ADD ... NOT VALID` step and `ShareUpdateExclusiveLock` for `VALIDATE`, the same as the `CHECK` recipe.

Foreign keys work the same way: `ADD CONSTRAINT ... FOREIGN KEY ... NOT VALID`, then `VALIDATE CONSTRAINT`. Unique constraints don't have `NOT VALID`; build a unique index `CONCURRENTLY`, then `ADD CONSTRAINT ... UNIQUE USING INDEX`.

## Backfills: many small transactions

Filling a new column for all old rows looks like one statement:

```console
tutor_migrations=# UPDATE orders SET total_cents = 1999;
UPDATE 2000004
Time: 16226.405 ms (00:16.226)
```

16 seconds in **one transaction**. For those 16 seconds it holds a row lock on every row: any app `UPDATE` of an order waits. It writes a new version of every row (see [MVCC and locks](mvcc-and-locks.md)), so the table doubles in size until `VACUUM` cleans up, and the WAL gets 2 million records at once, which replicas must replay (see [Replication and scaling](replication.md)). If it fails at row 1,999,999, everything is rolled back.

Do it in batches instead, each one its own short transaction, walking the primary key:

```sql
ALTER TABLE orders ADD COLUMN channel text;
DO $$
DECLARE
  last_id uuid := '00000000-0000-0000-0000-000000000000';
  n       int;
  total   bigint := 0;
BEGIN
  LOOP
    WITH batch AS (
      SELECT id FROM orders WHERE id > last_id ORDER BY id LIMIT 10000
    ), upd AS (
      UPDATE orders o SET channel = 'web' FROM batch b WHERE o.id = b.id RETURNING o.id
    )
    SELECT count(*), (array_agg(id ORDER BY id DESC))[1] INTO n, last_id FROM upd;
    EXIT WHEN n = 0;
    total := total + n;
    COMMIT;  -- one short transaction per batch: locks are held for milliseconds
    IF total % 500000 = 0 THEN RAISE NOTICE '% rows done', total; END IF;
  END LOOP;
  RAISE NOTICE 'finished: % rows', total;
END $$;
```

```console
ALTER TABLE
Time: 9.248 ms
NOTICE:  500000 rows done
NOTICE:  1000000 rows done
NOTICE:  1500000 rows done
NOTICE:  2000000 rows done
NOTICE:  finished: 2000004 rows
DO
Time: 26930.642 ms (00:26.931)
```

It took longer in total (27 s instead of 16 s), and that's fine. Each batch locks 10,000 rows for a few milliseconds, then lets go. `VACUUM` can clean up behind it, replicas keep up, and if it stops halfway you start again from the last ID.

- **Walk an index** (`id > last_id ORDER BY id`), don't search for "rows still NULL" each time. `WHERE channel IS NULL LIMIT 10000` has to skip all the rows already done, so every batch is slower than the one before.
- **`COMMIT` inside `DO`** works because a `DO` block run outside a transaction may control transactions (PostgreSQL 11+). In a real service, the loop usually lives in a Go job or a script instead, with a pause between batches when replicas lag.
- **New rows need the value too.** Set the column's default for new rows (or write it from the new app version) **before** the backfill, or rows inserted during the backfill stay NULL.

## Expand and contract: old and new code at the same time

A rolling deploy (see [Workloads](../kubernetes/workloads.md#a-rolling-update-step-by-step)) runs old pods and new pods side by side for minutes. A migration that makes the schema incompatible with the old code breaks the old pods. Rename a column, and the old code fails at once:

```sql
BEGIN;
ALTER TABLE orders RENAME COLUMN customer_id TO customer;
SELECT status FROM orders WHERE customer_id = 'c-42' LIMIT 1; -- what the old pods run
ROLLBACK;
```

```console
BEGIN
ALTER TABLE
ERROR:  column "customer_id" does not exist
LINE 1: SELECT status FROM orders WHERE customer_id = 'c-42' LIMIT 1...
                                        ^
HINT:  Perhaps you meant to reference the column "orders.customer".
ROLLBACK
```

So every breaking change becomes a series of small, compatible steps. **Expand** (add the new thing), **migrate** (move data and code over), **contract** (remove the old thing), each with its own deploy:

```mermaid
flowchart LR
    E1["1 expand: ADD COLUMN customer (nullable)"] --> E2["2 deploy app v2: writes both columns, reads old"]
    E2 --> E3["3 backfill customer in batches"]
    E3 --> E4["4 deploy app v3: reads and writes only customer"]
    E4 --> C1["5 contract: DROP COLUMN customer_id"]
```

- After **every** step, the running code and the schema fit together, and you can stop or roll back the app one step.
- The schema change always goes **first** when it adds something (old code ignores a new nullable column), and **last** when it removes something (no running code uses it anymore).
- Rollback of the app is easy until step 5. After step 5, there is no rolling back without restoring data. That's why contract steps often wait a week.

The same pattern covers changing a type (new column of the new type, dual write, backfill, switch, drop), splitting a table, and moving data to another service. It is slow on purpose. In return, no deploy ever needs downtime.

## Migrations in CI/CD

Write migrations as numbered files in the repo, apply them with a tool, and let the tool record which ones ran in a table in the database. Three common choices for Go services (versions from `go list -m -versions` on the day this page was written):

| Tool                                                        | Version                           | Style                                                                                           |
| ----------------------------------------------------------- | --------------------------------- | ----------------------------------------------------------------------------------------------- |
| [goose](https://github.com/pressly/goose)                   | v3.30.2                           | SQL files with `-- +goose Up` / `Down`, or Go functions; CLI or library                         |
| [golang-migrate](https://github.com/golang-migrate/migrate) | v4.20.1                           | Pairs of files, `0001_x.up.sql` and `0001_x.down.sql`; CLI or library                           |
| [Atlas](https://atlasgo.io)                                 | Go module `ariga.io/atlas` v1.4.0 | Declarative: you write the desired schema, it computes the plan, and lints it for risky changes |

The two migrations from this page, as goose files:

```sql
-- migrations/00001_create_order_items.sql
-- +goose Up
SET lock_timeout = '5s';
CREATE TABLE order_items (
  order_id    uuid   NOT NULL REFERENCES orders (id),
  sku         text   NOT NULL,
  quantity    int    NOT NULL CHECK (quantity > 0),
  price_cents bigint NOT NULL CHECK (price_cents >= 0),
  PRIMARY KEY (order_id, sku)
);

-- +goose Down
DROP TABLE order_items;
```

```sql
-- migrations/00002_index_orders_status.sql
-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY IF NOT EXISTS orders_status_created_idx ON orders (status, created_at);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS orders_status_created_idx;
```

(The SQL in both files ran fine through `psql` on the `tutor_migrations` database: `CREATE TABLE` and `CREATE INDEX`.) Run them with:

```sh
export GOOSE_DRIVER=postgres GOOSE_MIGRATION_DIR=migrations
export GOOSE_DBSTRING=postgres://postgres:tutor@127.0.0.1:55432/tutor_migrations
go run github.com/pressly/goose/v3/cmd/goose@v3.30.2 up
go run github.com/pressly/goose/v3/cmd/goose@v3.30.2 status
```

<!-- TODO(real-output): goose up / status output. Running a downloaded tool with `go run ...@version` was refused by this session's safety check. Expect `up` to print one "OK" line per migration with its duration and "goose: successfully migrated database to version: 2"; `status` lists both files as "Applied At <time>". -->

`up` applies each pending file in order and records it in the table `goose_db_version`. `status` lists what ran and when.

How to run migrations in a pipeline:

- **As their own step, before the app deploy**, not from the app at start-up. Ten pods starting at once would all try to migrate. Tools take an advisory lock so only one runs, but the other nine wait, and their readiness probes fail. A separate step (a CI job, a Kubernetes `Job`, or a Helm hook) runs once, and the pipeline stops if it fails.
- **With its own database role.** The app's role shouldn't be allowed to `DROP TABLE`. The migration role can, and it has `lock_timeout` set.
- **Every migration must work with the code that's running now.** That's expand and contract again. The pipeline order is: migrate (expand), deploy app, and later, in a separate change, migrate (contract).
- **Test them in CI against a real PostgreSQL**, with a copy of production's size if you can. A migration that is instant on an empty test database can rewrite a 100 GB table in production. [Go and SQL](go-and-sql.md) shows how to run tests against a real database.
- **Down migrations are rarely used in production.** Rolling back a schema often means losing data written since. Prefer rolling **forward** with a new migration. Keep `Down` for local development.
- **Never edit a migration that already ran** somewhere. The tool won't run it again, so the change never reaches that database. Add a new one.

## Try it

1. Run the three-session script from the lock queue section. While it runs, run the `pg_stat_activity` query yourself, and follow the `blocked_by` chain.
2. Add `SET lock_timeout = '2s';` and watch the app query get through after the migration gives up.
3. Add a column with `DEFAULT 'x'` and one with `DEFAULT random()`. Compare `pg_relation_filenode('orders')` before and after each.
4. Build an index with and without `CONCURRENTLY` while a loop inserts rows, and time the inserts.
5. Make a `CREATE UNIQUE INDEX CONCURRENTLY` fail, find the `INVALID` index with `pg_index`, and clean it up.
6. Clean up: `docker exec tutor-pg dropdb -U postgres tutor_migrations`.

## Traps

- **No `lock_timeout` on DDL.** One long transaction plus one quick `ALTER` stops all traffic on the table. Always set it, and retry.
- **"It's only a metadata change."** It still needs `ACCESS EXCLUSIVE` for a moment, and it waits in the queue for that moment.
- **Volatile defaults and type changes.** `DEFAULT now()` is fine (it's evaluated once per statement, so it's a constant here), `DEFAULT clock_timestamp()` or `gen_random_uuid()` rewrites the table. So do most `ALTER COLUMN TYPE` changes.
- **`CREATE INDEX` without `CONCURRENTLY`** on a live table blocks all writes for the whole build.
- **Leftover `INVALID` indexes.** They slow writes and help no reads. And `IF NOT EXISTS` skips the retry.
- **One big backfill `UPDATE`.** Long row locks, table bloat, a WAL spike, lagging replicas. Batch it.
- **Breaking changes in one step.** Renames and drops break the old pods during a rolling deploy. Expand, migrate, contract.
- **Migrating from the app's `main`.** Many pods race, start-up becomes slow and fragile, and the app needs DDL rights. Run migrations as a pipeline step.
- **A migration tool that wraps `CONCURRENTLY` in a transaction.** It fails with "cannot run inside a transaction block". Mark that migration as non-transactional.

## Check yourself

??? question "1. A migration that adds a nullable column has been 'running' for 3 minutes. It should take milliseconds. What is going on, and what do you do right now?"

    It is waiting for its `ACCESS EXCLUSIVE` lock, behind some transaction that has used the table
    (often one that is `idle in transaction`). Every new query on the table is queued behind it.
    Cancel the migration first (`pg_cancel_backend(pid)`), which frees the queue at once. Then find
    the blocker with `pg_blocking_pids`, deal with it, and rerun the migration with `lock_timeout`.

??? question "2. Why does a plain `SELECT` wait behind the `ALTER TABLE`, when it doesn't conflict with the report that holds the lock?"

    Lock requests are granted in order. The `SELECT` conflicts with the **waiting** `ALTER`
    (`ACCESS EXCLUSIVE` conflicts with everything), so it queues behind it. It doesn't matter that
    the lock currently granted would allow it.

??? question "3. Which of these rewrite the table? `ADD COLUMN a int`, `ADD COLUMN b text DEFAULT 'x'`, `ADD COLUMN c timestamptz DEFAULT clock_timestamp()`, `ALTER COLUMN id TYPE bigint` (from `int`)."

    `c` and the type change. `a` and `b` only change the catalog (PostgreSQL 11+ stores a constant
    default as the "missing value"). `clock_timestamp()` is volatile, so every row needs its own
    value, and `int` to `bigint` changes the size of every row on disk. Check with
    `pg_relation_filenode` before and after.

??? question "4. You need `NOT NULL` on `orders.customer_id`, 200 GB table, PostgreSQL 18. Give the steps and the lock each one takes."

    Make sure no NULLs remain (backfill in batches, and make the app always write it). Then
    `ADD CONSTRAINT orders_customer_nn NOT NULL customer_id NOT VALID` (`ACCESS EXCLUSIVE`,
    milliseconds, with `lock_timeout`), then `VALIDATE CONSTRAINT orders_customer_nn` (a full scan,
    under `SHARE UPDATE EXCLUSIVE`, so reads and writes go on). Before 18: the same with a `CHECK
    (customer_id IS NOT NULL) NOT VALID`, then `SET NOT NULL`, which then skips the scan, then drop the check.

??? question "5. You want to rename `orders.status` to `orders.state` without downtime. List the deploys."

    1. Migration: add `state` (nullable). 2. App: write both columns, read `status`. 3. Backfill
    `state` in batches, then add the `NOT NULL` safely if needed. 4. App: read and write only
    `state`. 5. Later migration: drop `status`. Old and new code fit the schema at every step.

??? question "6. Your `CREATE UNIQUE INDEX CONCURRENTLY` failed on a duplicate. You fix the data and rerun the same migration with `IF NOT EXISTS`. It says OK. Is the index usable?"

    No. The failed build left an `INVALID` index with that name, and `IF NOT EXISTS` saw the name
    and did nothing. Check `pg_index.indisvalid`, `DROP INDEX CONCURRENTLY` the invalid one, and
    build it again.

## Recap

- Almost every `ALTER TABLE` needs `ACCESS EXCLUSIVE`. Locks are granted **in order**, so a waiting migration blocks every query after it. `pg_blocking_pids` shows the chain.
- **`lock_timeout`** (1-5 s) on every migration, plus a retry, bounds the damage to a few seconds.
- Adding a column with a **constant default** is instant (PostgreSQL 11+). **Volatile defaults** and most **type changes** rewrite the table.
- **`CREATE INDEX CONCURRENTLY`** lets writes go on. It can't run in a transaction, and a failure leaves an `INVALID` index.
- Constraints: add them **`NOT VALID`**, then **`VALIDATE`** under a weak lock. PostgreSQL 18 does this for `NOT NULL` directly.
- **Backfill in batches**, walking the primary key, one short transaction each.
- **Expand and contract**: every step fits both the old and the new code. Add first, remove last.
- Run migrations with a tool (goose, golang-migrate, Atlas) **as a pipeline step**, with their own role, tested against a real database.

Next: [Go and SQL](go-and-sql.md), where your service talks to all of this through a connection pool.
