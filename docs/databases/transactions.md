# Transactions and isolation

## Why care?

- "Two customers bought the last book. Both got a confirmation."
- "The stock counter is 3 lower than the number of orders says it should be."
- "We check that at least one engineer is on call before anyone signs off. Last night nobody was on call."
- "Since we switched to `SERIALIZABLE`, we get `could not serialize access` errors in the logs."

Every one of these is a **race between two transactions**. The database did exactly what its isolation level promised. The promise was weaker than the developer thought. This page shows each race for real, with two `psql` sessions, and then the Go code that survives them.

## The idea: a shared spreadsheet

Picture a team that keeps the shop's stock in one shared spreadsheet.

- **Atomicity**: you change five cells as one edit. Either all five land, or none. Nobody ever sees three of the five.
- **Consistency**: the sheet has rules ("stock is never negative"). An edit that breaks a rule is refused as a whole.
- **Isolation**: while you are editing, how much of other people's unfinished and finished work do you see? That is the interesting one, and it has **levels**.
- **Durability**: once the sheet says "saved", a power cut doesn't undo it. That is the WAL and `fsync` from [How data is stored](storage.md#under-the-hood-the-write-ahead-log).

For isolation, imagine each person gets a **photo** of the sheet:

- At **Read Committed**, you take a new photo before every single question you ask. Each answer is up to date, but two answers in one task may come from two different photos.
- At **Repeatable Read**, you take one photo when your task starts and use it until you are done. Everything you read agrees with itself. But you may be working from an old photo when you write.
- At **Serializable**, an assistant watches all the photos and edits. If the result could not have happened with people taking turns one after another, the assistant tears up one person's work and says "please do it again".

```mermaid
sequenceDiagram
    participant A as Transaction A
    participant DB as PostgreSQL
    participant B as Transaction B
    A->>DB: BEGIN; SELECT stock → 10
    B->>DB: BEGIN; SELECT stock → 10
    A->>DB: UPDATE stock = 9; COMMIT
    B->>DB: UPDATE stock = 9 (waits for A's row lock)
    Note over B,DB: Read Committed: B's update runs, stock = 9<br/>Repeatable Read: ERROR 40001
    B->>DB: COMMIT
```

PostgreSQL builds these photos with **MVCC**: every row version carries the transaction that created and deleted it, and a **snapshot** decides which versions you see. [MVCC and locks](mvcc-and-locks.md) shows the mechanics. This page is about what you **observe**.

## Setup: two sessions

You need the `tutor-pg` container from [How data is stored](storage.md#setup-your-own-postgresql) and a database with two small tables:

```console
$ docker exec tutor-pg createdb -U postgres tutor_transactions
$ docker exec -i tutor-pg psql -U postgres -d tutor_transactions <<'EOF'
CREATE TABLE products (sku text PRIMARY KEY, stock int NOT NULL);
INSERT INTO products VALUES ('book-42', 10);
CREATE TABLE orders (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, customer_id text NOT NULL, status text NOT NULL, total_cents bigint NOT NULL);
INSERT INTO orders (customer_id, status, total_cents) VALUES ('alice','pending',1500),('bob','pending',2500),('carol','paid',4000);
EOF
CREATE TABLE
INSERT 0 1
CREATE TABLE
INSERT 0 3
```

To try the races yourself, open two terminals and run `docker exec -it tutor-pg psql -U postgres -d tutor_transactions` in both. Type the statements in the order shown.

The output below was made with a small script that feeds two `psql` processes (with `-e`, so each statement is echoed before its result) and waits 0.4 s between steps. Every line starts with `A |` or `B |` for the session that printed it, in the order the lines appeared:

```bash
#!/usr/bin/env bash
# race.sh steps.txt — each line is "A: <sql>", "B: <sql>" or "sleep N".
set -u
dir=$(mktemp -d "$PWD/run.XXXX")
mkfifo "$dir/a" "$dir/b"
for s in a b; do
  S=$(echo $s | tr a-z A-Z)
  docker exec -i tutor-pg psql -X -e -U postgres -d tutor_transactions -v PROMPT1= <"$dir/$s" 2>&1 | sed -u "s/^/$S | /" &
done
exec 3>"$dir/a" 4>"$dir/b"
while IFS= read -r line; do
  case "$line" in
    A:*) printf '%s\n' "${line#A: }" >&3 ;;
    B:*) printf '%s\n' "${line#B: }" >&4 ;;
    sleep*) $line ;;
  esac
  sleep 0.4
done <"$1"
exec 3>&- 4>&-
wait
rm -r "$dir"
```

The default isolation level is Read Committed:

```console
$ docker exec tutor-pg psql -X -U postgres -c 'SHOW default_transaction_isolation'
 default_transaction_isolation
-------------------------------
 read committed
(1 row)
```

## Atomicity: all or nothing

Before the races, one transaction on its own. An order is inserted, then the stock update breaks a `CHECK` constraint:

```console
tutor_transactions=# ALTER TABLE products ADD CONSTRAINT stock_not_negative CHECK (stock >= 0);
ALTER TABLE
```

The statements are in a file, `/tmp/tutor-atom.sql` inside the container, run with `psql -f` (so error lines start with the file name and line):

```console
$ docker exec -t tutor-pg psql -X -P pager=off -e -U postgres -d tutor_transactions -f /tmp/tutor-atom.sql
BEGIN;
BEGIN
INSERT INTO orders (customer_id, status, total_cents) VALUES ('frank', 'paid', 1999);
INSERT 0 1
UPDATE products SET stock = stock - 100 WHERE sku = 'book-42';
psql:/tmp/tutor-atom.sql:3: ERROR:  new row for relation "products" violates check constraint "stock_not_negative"
DETAIL:  Failing row contains (book-42, -92).
SELECT count(*) FROM orders;
psql:/tmp/tutor-atom.sql:4: ERROR:  current transaction is aborted, commands ignored until end of transaction block
COMMIT;
ROLLBACK
SELECT count(*) FROM orders WHERE customer_id = 'frank';
 count
-------
     0
(1 row)
```

Three things to learn:

1. After an error, PostgreSQL refuses every further statement in the transaction (`current transaction is aborted`). Other databases let you carry on. PostgreSQL makes you decide: roll back, or use a `SAVEPOINT` if you planned for it.
2. `COMMIT` on a failed transaction answers `ROLLBACK`, not an error. In Go, `pgx` turns that into an error from `tx.Commit()`. The same three steps from a small Go program with `database/sql`:

   ```console
   $ go run ./c
   exec: ERROR: new row for relation "products" violates check constraint "stock_not_negative" (SQLSTATE 23514)
   exec2: ERROR: current transaction is aborted, commands ignored until end of transaction block (SQLSTATE 25P02)
   commit: commit unexpectedly resulted in rollback
   ```

   Code that ignores the `Commit` error believes the order was saved.

3. Frank's order is gone, even though its `INSERT` succeeded. That is atomicity.

## The anomalies, one by one

The SQL standard defines isolation levels by the **anomalies** (strange results) they allow. Here is each one, live.

### Dirty read: impossible in PostgreSQL

A **dirty read** is seeing another transaction's change before it commits. If that transaction then rolls back, you acted on data that never existed. Ask for the weakest level, `READ UNCOMMITTED`:

```console
A | BEGIN;
A | BEGIN
A | UPDATE products SET stock = 0 WHERE sku = 'book-42';
A | UPDATE 1
B | BEGIN ISOLATION LEVEL READ UNCOMMITTED;
B | BEGIN
B | SELECT stock FROM products WHERE sku = 'book-42';
B |  stock
B | -------
B |     10
B | (1 row)
B |
A | ROLLBACK;
A | ROLLBACK
B | COMMIT;
B | COMMIT
```

B sees 10, not A's uncommitted 0. PostgreSQL accepts `READ UNCOMMITTED` and quietly runs it as `READ COMMITTED`. With MVCC, uncommitted versions are simply not in anyone's snapshot. (SQL Server and MySQL do allow dirty reads at this level.)

### Non-repeatable read

You read a row twice in one transaction and get two different answers, because someone committed a change in between:

```console
A | BEGIN;
A | BEGIN
A | SELECT status FROM orders WHERE customer_id = 'alice';
A |  status
A | ---------
A |  pending
A | (1 row)
A |
B | UPDATE orders SET status = 'paid' WHERE customer_id = 'alice';
B | UPDATE 1
A | SELECT status FROM orders WHERE customer_id = 'alice';
A |  status
A | --------
A |  paid
A | (1 row)
A |
A | COMMIT;
A | COMMIT
```

B ran without `BEGIN`, so its `UPDATE` was a transaction of its own and committed at once. Read Committed takes a new snapshot for **each statement**, so A's second `SELECT` sees it. Now reset alice to `pending` and do the same in `REPEATABLE READ`:

```console
B | UPDATE orders SET status = 'pending' WHERE customer_id = 'alice';
B | UPDATE 1
A | BEGIN ISOLATION LEVEL REPEATABLE READ;
A | BEGIN
A | SELECT status FROM orders WHERE customer_id = 'alice';
A |  status
A | ---------
A |  pending
A | (1 row)
A |
B | UPDATE orders SET status = 'paid' WHERE customer_id = 'alice';
B | UPDATE 1
A | SELECT status FROM orders WHERE customer_id = 'alice';
A |  status
A | ---------
A |  pending
A | (1 row)
A |
A | COMMIT;
A | COMMIT
A | SELECT status FROM orders WHERE customer_id = 'alice';
A |  status
A | --------
A |  paid
A | (1 row)
A |
```

Repeatable Read takes **one snapshot at the first statement** and keeps it. A sees `pending` until it commits, even though `paid` was committed long before. Only the next transaction sees `paid`.

When does a non-repeatable read hurt? A report that reads the order list, then the order totals, and the two don't add up. A job that checks a status and then acts on it two statements later.

### Phantom read

Not one row changing, but **new rows appearing** in a query you repeat. B inserts a pending order between A's two counts:

```console
A | BEGIN;
A | BEGIN
A | SELECT count(*) FROM orders WHERE status = 'pending';
A |  count
A | -------
A |      1
A | (1 row)
A |
B | INSERT INTO orders (customer_id, status, total_cents) VALUES ('dave', 'pending', 999);
B | INSERT 0 1
A | SELECT count(*) FROM orders WHERE status = 'pending';
A |  count
A | -------
A |      2
A | (1 row)
A |
A | COMMIT;
A | COMMIT
A | BEGIN ISOLATION LEVEL REPEATABLE READ;
A | BEGIN
A | SELECT count(*) FROM orders WHERE status = 'pending';
A |  count
A | -------
A |      2
A | (1 row)
A |
B | INSERT INTO orders (customer_id, status, total_cents) VALUES ('erin', 'pending', 999);
B | INSERT 0 1
A | SELECT count(*) FROM orders WHERE status = 'pending';
A |  count
A | -------
A |      2
A | (1 row)
A |
```

A phantom (`dave`) under Read Committed, none (`erin` stays invisible) under Repeatable Read. The SQL standard allows phantoms at Repeatable Read. PostgreSQL's Repeatable Read is **snapshot isolation**, which is stronger: a snapshot hides new rows too. Don't count on that if your code must also run on another database.

### Lost update

The classic bug in application code: read a value, compute in Go, write it back. Two customers buy `book-42` at the same moment. Both transactions read stock 10 and write 9:

```console
A | BEGIN;
A | BEGIN
A | SELECT stock FROM products WHERE sku = 'book-42';
A |  stock
A | -------
A |     10
A | (1 row)
A |
B | BEGIN;
B | BEGIN
B | SELECT stock FROM products WHERE sku = 'book-42';
B |  stock
B | -------
B |     10
B | (1 row)
B |
A | UPDATE products SET stock = 9 WHERE sku = 'book-42';
A | UPDATE 1
B | UPDATE products SET stock = 9 WHERE sku = 'book-42';
A | COMMIT;
A | COMMIT
B | UPDATE 1
B | COMMIT;
B | COMMIT
A | SELECT stock FROM products WHERE sku = 'book-42';
A |  stock
A | -------
A |      9
A | (1 row)
A |
```

Two books sold, stock went down by one. Look at B's `UPDATE`: its result (`UPDATE 1`) only appears **after** A's `COMMIT`. A held a **row lock** on `book-42` from its `UPDATE` until it committed, and B waited. When A committed, B's `UPDATE` went ahead and wrote 9, the value B had computed from its stale read. The lock prevented two writes at the same moment. It did not prevent B from writing a decision based on old data.

The same race in Repeatable Read (stock reset to 10 first):

```console
A | BEGIN ISOLATION LEVEL REPEATABLE READ;
A | BEGIN
A | SELECT stock FROM products WHERE sku = 'book-42';
A |  stock
A | -------
A |     10
A | (1 row)
A |
B | BEGIN ISOLATION LEVEL REPEATABLE READ;
B | BEGIN
B | SELECT stock FROM products WHERE sku = 'book-42';
B |  stock
B | -------
B |     10
B | (1 row)
B |
A | UPDATE products SET stock = 9 WHERE sku = 'book-42';
A | UPDATE 1
B | UPDATE products SET stock = 9 WHERE sku = 'book-42';
A | COMMIT;
A | COMMIT
B | ERROR:  could not serialize access due to concurrent update
B | COMMIT;
B | ROLLBACK
```

B waited again, and when A committed, PostgreSQL saw that B wanted to change a row that changed **after B's snapshot**. Writing it would lose A's update, so B gets a **serialization failure** (SQLSTATE `40001`). The update is not lost. B must retry the whole transaction, and on the retry it reads 9.

There are two better fixes at Read Committed, and you should know both.

**Let the database do the arithmetic.** An `UPDATE` re-reads the newest committed version of the row after waiting for the lock, and applies the expression to it:

```console
A | BEGIN;
A | BEGIN
B | BEGIN;
B | BEGIN
A | UPDATE products SET stock = stock - 1 WHERE sku = 'book-42' RETURNING stock;
A |  stock
A | -------
A |      9
A | (1 row)
A |
A | UPDATE 1
B | UPDATE products SET stock = stock - 1 WHERE sku = 'book-42' RETURNING stock;
A | COMMIT;
A | COMMIT
B |  stock
B | -------
B |      8
B | (1 row)
B |
B | UPDATE 1
B | COMMIT;
B | COMMIT
```

8, correct. Add `AND stock > 0` to the `WHERE`, and an `UPDATE 0` tells you it's sold out. One statement, no race. Use this whenever the change fits in SQL.

**Lock the row when you read it**, with `SELECT ... FOR UPDATE`, when the decision needs Go code between the read and the write:

```console
A | BEGIN;
A | BEGIN
A | SELECT stock FROM products WHERE sku = 'book-42' FOR UPDATE;
A |  stock
A | -------
A |     10
A | (1 row)
A |
B | BEGIN;
B | BEGIN
B | SELECT stock FROM products WHERE sku = 'book-42' FOR UPDATE;
A | UPDATE products SET stock = 9 WHERE sku = 'book-42';
A | UPDATE 1
A | COMMIT;
A | COMMIT
B |  stock
B | -------
B |      9
B | (1 row)
B |
B | UPDATE products SET stock = 8 WHERE sku = 'book-42';
B | UPDATE 1
B | COMMIT;
B | COMMIT
```

B's **read** now waits for A, and gets the fresh value 9. The cost: B is blocked for as long as A's transaction lasts, so keep it short. `NOWAIT`, `SKIP LOCKED` and deadlocks are on [MVCC and locks](mvcc-and-locks.md).

### Write skew: the one Repeatable Read lets through

The support team has a rule: at least one engineer must stay on call. Ana and Ben both want to sign off. Each one's app checks "are at least two on call?" and then sets **their own** row to off:

```console
tutor_transactions=# CREATE TABLE support_rota (engineer text PRIMARY KEY, on_call boolean NOT NULL);
CREATE TABLE
tutor_transactions=# INSERT INTO support_rota VALUES ('ana', true), ('ben', true);
INSERT 0 2
```

Both sessions ran `\set VERBOSITY verbose` first, so errors show their SQLSTATE code.

```console
A | BEGIN ISOLATION LEVEL REPEATABLE READ;
A | BEGIN
A | SELECT count(*) FROM support_rota WHERE on_call;
A |  count
A | -------
A |      2
A | (1 row)
A |
B | BEGIN ISOLATION LEVEL REPEATABLE READ;
B | BEGIN
B | SELECT count(*) FROM support_rota WHERE on_call;
B |  count
B | -------
B |      2
B | (1 row)
B |
A | UPDATE support_rota SET on_call = false WHERE engineer = 'ana';
A | UPDATE 1
B | UPDATE support_rota SET on_call = false WHERE engineer = 'ben';
B | UPDATE 1
A | COMMIT;
A | COMMIT
B | COMMIT;
B | COMMIT
A | SELECT * FROM support_rota;
A |  engineer | on_call
A | ----------+---------
A |  ana      | f
A |  ben      | f
A | (2 rows)
A |
```

Nobody is on call. Each transaction was correct on its own snapshot, and they wrote **different rows**, so there was no row lock conflict and no "concurrent update" error. This is **write skew**: two transactions read overlapping data, make decisions, and write disjoint data. Snapshot isolation can't see it, because it only checks write-write conflicts.

Same steps, both rows back to `true`, `SERIALIZABLE`:

```console
A | BEGIN ISOLATION LEVEL SERIALIZABLE;
A | BEGIN
A | SELECT count(*) FROM support_rota WHERE on_call;
A |  count
A | -------
A |      2
A | (1 row)
A |
B | BEGIN ISOLATION LEVEL SERIALIZABLE;
B | BEGIN
B | SELECT count(*) FROM support_rota WHERE on_call;
B |  count
B | -------
B |      2
B | (1 row)
B |
A | UPDATE support_rota SET on_call = false WHERE engineer = 'ana';
A | UPDATE 1
B | UPDATE support_rota SET on_call = false WHERE engineer = 'ben';
B | UPDATE 1
A | COMMIT;
A | COMMIT
B | COMMIT;
B | ERROR:  40001: could not serialize access due to read/write dependencies among transactions
B | DETAIL:  Reason code: Canceled on identification as a pivot, during commit attempt.
B | HINT:  The transaction might succeed if retried.
B | LOCATION:  PreCommit_CheckForSerializationFailure, predicate.c:4723
A | SELECT * FROM support_rota;
A |  engineer | on_call
A | ----------+---------
A |  ben      | t
A |  ana      | f
A | (2 rows)
A |
```

PostgreSQL's Serializable is **SSI** (serializable snapshot isolation). It runs like Repeatable Read, and it also remembers what each transaction **read** (with "predicate locks" that never block anyone). When it finds a pattern of read/write dependencies that no one-after-another order could produce, it aborts one transaction. Here B read the rows A changed and A read the rows B changed: a cycle. B fails at `COMMIT`, with the hint to retry. On the retry, B would see `count = 1` and refuse.

Two other fixes, without Serializable: lock the rows you read the decision from (`SELECT ... FROM support_rota WHERE on_call FOR UPDATE`), or turn the rule into something the database enforces itself (a constraint, or one row that both transactions must update, so they conflict).

### Summary: what each level allows in PostgreSQL

| Anomaly             | Read Committed (default)   | Repeatable Read                       | Serializable    |
| ------------------- | -------------------------- | ------------------------------------- | --------------- |
| Dirty read          | No                         | No                                    | No              |
| Non-repeatable read | **Yes**                    | No                                    | No              |
| Phantom read        | **Yes**                    | No (PG is stricter than the standard) | No              |
| Lost update         | **Yes** (read, then write) | No: error 40001                       | No: error 40001 |
| Write skew          | **Yes**                    | **Yes**                               | No: error 40001 |

Higher levels don't make the races go away. They turn **wrong answers** into **errors**. Your code must handle those errors.

## Serialization failures and retry loops

At Repeatable Read and Serializable, a `40001` can come from any statement, including `COMMIT`. The only correct reaction is: **roll back, and run the whole transaction again from the start**, including its reads. A deadlock (`40P01`, see [MVCC and locks](mvcc-and-locks.md)) gets the same treatment.

In Go, with `database/sql` and the `pgx` driver (v5.13.0 here, see [Go and SQL](go-and-sql.md) for pools and timeouts):

```go
// Command offcall lets two engineers go off call at the same time,
// in SERIALIZABLE transactions, and retries on serialization failures.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
)

var errLastOnCall = errors.New("would leave nobody on call")

// inTx runs fn in a SERIALIZABLE transaction and retries it when
// PostgreSQL reports a serialization failure (SQLSTATE 40001).
func inTx(ctx context.Context, db *sql.DB, name string, fn func(*sql.Tx) error) error {
	const maxAttempts = 5
	for attempt := 1; ; attempt++ {
		err := func() error {
			tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
			if err != nil {
				return err
			}
			defer tx.Rollback() // no-op after a successful Commit
			if err := fn(tx); err != nil {
				return err
			}
			return tx.Commit()
		}()
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "40001" || attempt == maxAttempts {
			return err
		}
		fmt.Printf("%s: attempt %d: %s, retrying\n", name, attempt, pgErr.Message)
		time.Sleep(time.Duration(attempt) * 10 * time.Millisecond)
	}
}

func main() {
	ctx := context.Background()
	db, err := sql.Open("pgx", "postgres://postgres:tutor@127.0.0.1:55432/tutor_transactions")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `UPDATE support_rota SET on_call = true`); err != nil {
		log.Fatal(err)
	}

	var bothRead sync.WaitGroup // makes the first attempts overlap
	bothRead.Add(2)
	var once [2]sync.Once
	var wg sync.WaitGroup
	for i, name := range []string{"ana", "ben"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := inTx(ctx, db, name, func(tx *sql.Tx) error {
				var n int
				err := tx.QueryRowContext(ctx, `SELECT count(*) FROM support_rota WHERE on_call`).Scan(&n)
				if err != nil {
					return err
				}
				once[i].Do(func() { bothRead.Done(); bothRead.Wait() })
				if n < 2 {
					return errLastOnCall
				}
				_, err = tx.ExecContext(ctx, `UPDATE support_rota SET on_call = false WHERE engineer = $1`, name)
				return err
			})
			fmt.Printf("%s: done, err=%v\n", name, err)
		}()
	}
	wg.Wait()

	rows, err := db.QueryContext(ctx, `SELECT engineer, on_call FROM support_rota ORDER BY engineer`)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var e string
		var on bool
		if err := rows.Scan(&e, &on); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s on_call=%v\n", e, on)
	}
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}
}
```

The `bothRead` barrier makes both goroutines finish their `SELECT` before either one writes, only on the first attempt, so the race happens every time:

```console
$ go run .
ana: attempt 1: could not serialize access due to read/write dependencies among transactions, retrying
ben: done, err=<nil>
ana: done, err=would leave nobody on call
ana on_call=true
ben on_call=false
```

Ben signed off. Ana's first attempt failed with `40001`. Her retry started a new transaction, saw only one engineer on call, and the business rule refused. The rule holds, and no `40001` reached the user.

What makes this loop correct:

- **The whole transaction is the unit of retry.** `fn` does the reads too. Retrying only the failed statement would reuse stale data.
- **`defer tx.Rollback()`** cleans up after every error. After `Commit` it does nothing.
- **Only retry what is retryable.** `40001` (and `40P01`) yes. A constraint violation or `errLastOnCall` no: those are answers.
- **Limit attempts and back off.** Under heavy contention, retries without a pause collide again.
- **No side effects inside `fn`.** Don't send an e-mail, publish to Kafka or call another service from inside the transaction. It may run three times, or run and then roll back. Do it after the commit, or write it to an outbox table in the same transaction.
- **Pass `ctx` everywhere.** A cancelled request stops the retries too.

## Choosing a level

- **Read Committed** (the default) is fine for most code, **if** you write changes as single statements (`SET stock = stock - 1`), or lock what you read with `FOR UPDATE`. Most real bugs at this level are read-then-write in Go.
- **Repeatable Read** for reports and exports that must see one consistent state across many queries. Read-only transactions at this level never fail with `40001`.
- **Serializable** when the correctness rules span several rows or tables and are hard to express as locks or constraints. You pay with a little overhead and with retries. It only protects you if **every** transaction that touches that data runs at Serializable.

## Try it

1. Re-run the lost update at Read Committed, but with `UPDATE products SET stock = stock - 1 WHERE sku = 'book-42' AND stock > 0` and stock set to 1. What does each session's `UPDATE` report?
2. Run the write-skew example at Repeatable Read with `FOR UPDATE` on the `SELECT count(*) ...` (hint: you need to select the rows, `SELECT engineer FROM support_rota WHERE on_call FOR UPDATE`, because `count(*)` can't be locked). Does B wait, and what does it see?
3. In the Go program, change the isolation level to `sql.LevelRepeatableRead`. Run it. What does the table look like afterwards?
4. Make `maxAttempts` 1 and run the Go program. What error does the caller get, and how would you map it to an HTTP status?

Clean up:

```sh
docker exec tutor-pg dropdb -U postgres tutor_transactions
```

## Traps

- **Read in Go, decide in Go, write the result back.** That's a lost update at the default level. Write `SET x = x - 1`, or `SELECT ... FOR UPDATE`, or use a higher level and retry.
- **Raising the isolation level without a retry loop.** You swapped wrong data for random 500 errors.
- **Retrying only the last statement.** Retry the whole transaction, reads included.
- **Side effects inside a retried transaction.** E-mails sent twice, messages published for orders that rolled back.
- **Long transactions.** They hold row locks (blocking others), hold back `VACUUM` (see [MVCC and locks](mvcc-and-locks.md)), and at Serializable raise the chance of conflicts. Never wait for a user, an HTTP call or a queue inside a transaction.
- **Ignoring the `Commit` error.** In PostgreSQL, a `COMMIT` of an aborted transaction rolls back. `tx.Commit()` is where you find out.
- **Mixing levels on Serializable data.** One Read Committed writer can break the guarantee for everybody.
- **Assuming PostgreSQL behaviour elsewhere.** MySQL's Repeatable Read, for example, does not raise an error on the lost update above. Test the races on the database you run.

## Check yourself

??? question "1. Your code does `SELECT balance`, adds 10 in Go, and runs `UPDATE ... SET balance = $1`. Two requests run at once at Read Committed. What can happen, and what are two fixes?"

    A lost update: both read the same balance and one write overwrites the other, so 10 is added
    once instead of twice. Fixes: `UPDATE ... SET balance = balance + 10` (one statement), or
    `SELECT ... FOR UPDATE` before computing. A third: Repeatable Read or Serializable with a retry
    loop.

??? question "2. Why does PostgreSQL never show dirty reads, even at `READ UNCOMMITTED`?"

    MVCC: a snapshot only includes row versions from committed transactions (plus your own). There
    is no cheaper mode that reads uncommitted versions, so PostgreSQL runs `READ UNCOMMITTED` as
    `READ COMMITTED`.

??? question "3. Two Repeatable Read transactions update different rows and both commit, but a business rule is now broken. What is the anomaly, and which level prevents it?"

    Write skew. Snapshot isolation only detects write-write conflicts on the same row. Serializable
    (SSI) tracks read/write dependencies and aborts one of the transactions with `40001`.

??? question "4. A transaction at Serializable fails with `40001` at `COMMIT`. Its earlier statements all succeeded. What should the application do?"

    Roll back and run the whole transaction again from the beginning, re-reading everything,
    preferably with a small backoff and a limit on attempts. Nothing from the failed attempt was
    saved.

??? question "5. In the lost-update race at Read Committed, B's `UPDATE` waited for A. Why didn't that wait prevent the bug?"

    The row lock serialises the two **writes**, but B's decision (write 9) was based on a `SELECT`
    that ran before the lock, outside any lock. After A committed, B wrote its stale value. Either
    lock at read time (`FOR UPDATE`) or let the `UPDATE` compute from the current value.

## Recap

- **ACID**: all or nothing, rules enforced, isolation between concurrent transactions, and durability from the WAL.
- PostgreSQL has three real levels. **Read Committed** (default): a new snapshot per statement. **Repeatable Read**: one snapshot per transaction (snapshot isolation). **Serializable**: snapshot isolation plus detection of dangerous read/write patterns (SSI).
- **Dirty reads** never happen. **Non-repeatable reads** and **phantoms** happen at Read Committed. **Lost updates** happen at Read Committed when you read, then write; Repeatable Read turns them into `40001`. **Write skew** needs Serializable (or explicit locks).
- An `UPDATE` waits for row locks and then works on the newest version. `SET x = x - 1` and `SELECT ... FOR UPDATE` are the simple fixes at the default level.
- Higher levels turn wrong results into **serialization failures**. Retry the **whole** transaction on `40001` and `40P01`, with a limit, a backoff, and no side effects inside.

Next: the machinery behind snapshots and row locks. [MVCC and locks](mvcc-and-locks.md).
