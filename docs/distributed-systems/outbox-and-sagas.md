# Outbox and sagas

## Why care?

Three tickets, all about one order:

- "Order 1 is in the database, but payments never heard of it. No error in the logs, the pod was just restarted."
- "Payments charged a customer for an order that does not exist. The insert had failed."
- "Shipping failed for order 42, so we cancelled it. The customer was still charged."

The first two are the **dual-write problem**: one request changes **two** systems (the database and the broker), and there is no transaction that covers both. The third is a business process that spans **three** services, each with its own database: no transaction covers that either.

This page shows both bugs with real runs, then fixes them with three patterns that every event-driven system ends up using: the **transactional outbox**, the **inbox**, and the **saga**. It builds on [Messaging](messaging.md) (at-least-once delivery) and on [Transactions and isolation](../databases/transactions.md) (what `COMMIT` promises).

## The idea: write the note on the receipt

A shop clerk sells a sofa. Two things must happen: the sale goes into the **cash book**, and the **warehouse** must be told to deliver.

- **Dual write**: the clerk writes the sale in the book, then walks to the phone to call the warehouse. If the clerk faints on the way, the sale is booked and nobody delivers. If the clerk calls first and the card is then declined, the warehouse delivers a sofa nobody paid for.
- **Outbox**: the cash book has a column "notes for the warehouse". The clerk writes the sale **and** the note on the same line, in one go. A **runner** comes by every few seconds, takes every note that is not ticked yet, phones the warehouse, and ticks the note. If the runner faints, the next runner finds the unticked note and phones again.
- **Inbox**: the warehouse keeps a list of note numbers it has already handled. When the runner phones twice about note 17, the warehouse says "17? done already" and does nothing.

The trick: the only thing that must be atomic is a write to **one** database, which databases are very good at. Everything after that is at-least-once with retries, and duplicates are made harmless.

```mermaid
flowchart LR
    subgraph O ["orders service"]
        H[handler] -->|"one transaction"| DB[("orders + outbox")]
        R[relay] -->|"1. SELECT ... FOR UPDATE SKIP LOCKED"| DB
        R -->|"3. UPDATE published_at, COMMIT"| DB
    end
    R -->|"2. publish (event ID)"| MQ[[broker]]
    subgraph P ["payments service"]
        C[consumer] -->|"one transaction: inbox + charge"| PDB[("inbox + charges")]
    end
    MQ -->|"at-least-once"| C
```

## Under the hood

### The dual-write problem

Two writes, two systems, two orders to do them in. Both are broken:

| Order                                           | What fails                                       | Result                              |
| ----------------------------------------------- | ------------------------------------------------ | ----------------------------------- |
| Commit, then publish                            | Crash, deploy, broker down, timeout              | Order exists, **event lost**        |
| Publish, then commit                            | Constraint, deadlock, serialization error, crash | Event out, **order does not exist** |
| Publish inside the transaction, before `COMMIT` | `COMMIT` fails                                   | Same as publish first               |

Retrying does not save you. If the process died, there is nobody left to retry. If the publish timed out, you don't know whether it happened (the run below shows a timeout where the message **was** stored).

What about a distributed transaction (two-phase commit, XA) over the database and the broker? Kafka and NATS don't take part in XA, it holds locks while the slowest participant answers, and a coordinator crash leaves participants stuck "in doubt". Almost nobody does it for this. The outbox gets the same result with one local transaction.

### Transactional outbox

1. In the **same** database transaction as the business change, insert a row into an `outbox` table: event ID, subject (or topic), payload.
2. `COMMIT`. Now the order and the event exist together, or neither does.
3. A separate **relay** reads unpublished outbox rows, publishes them, and marks them published.

The relay is **at-least-once**: it can publish and then crash before it marks the row. So every event carries a unique **event ID**, and the receiving side must dedupe on it (broker dedupe helps, the inbox is the real guarantee).

There are two ways to build the relay:

- **Polling** (this page): `SELECT ... FOR UPDATE SKIP LOCKED` every few hundred milliseconds. Simple, works on any SQL database. Costs: some latency, and a busy table that needs cleaning (delete published rows, or partition the table by day).
- **Log tailing / CDC**: read the database's change stream (PostgreSQL logical replication, see [Replication and scaling](../databases/replication.md#logical-replication-briefly)), for example with Debezium. Lower latency, no polling. But you now run more infrastructure, and you inherit the replication-slot risks from that page: a stuck reader keeps WAL forever.

### `FOR UPDATE SKIP LOCKED`

You want two or more relays (for availability), and they must not publish the same row at the same time. `FOR UPDATE` locks the rows a relay selected; `SKIP LOCKED` makes the other relay **skip** them instead of waiting. The details, with two `psql` sessions, are in [MVCC and locks](../databases/mvcc-and-locks.md#nowait-and-skip-locked-a-job-queue). The cost: relays now publish in parallel, so **events can leave in a different order** than they were written. The run below shows it.

### Inbox (idempotent consumer)

The consumer writes the event ID into an `inbox` table with a primary key, **in the same transaction** as its own change:

```sql
INSERT INTO payments.inbox (event_id) VALUES ($1) ON CONFLICT DO NOTHING;
-- 0 rows inserted: seen before. Roll back, ack, done.
-- 1 row inserted:  do the work in the same transaction, COMMIT, then ack.
```

If the consumer crashes after `COMMIT` but before the ack, the broker redelivers. The second time, the insert finds the ID and the work is skipped. If it crashes before `COMMIT`, both the inbox row and the work are rolled back, and the redelivery does the work for the first time. Either way: effectively once.

This only works when the side effect is **in the same database** as the inbox. If the side effect is a call to a payment provider, send them an **idempotency key** instead (see [Retries and idempotency](retries-and-idempotency.md)).

### Sagas

A **saga** is a business transaction made of local transactions in several services. Each step commits on its own. If a later step fails, you can't roll back the earlier ones: they are committed, maybe already seen by the customer. Instead each step has a **compensation**: a new action that semantically undoes it.

| Step           | Compensation                 |
| -------------- | ---------------------------- |
| Reserve stock  | Release the reservation      |
| Charge payment | Refund (not "delete charge") |
| Book shipping  | Cancel the shipping label    |

A refund is not a rollback. The customer's statement shows a charge **and** a refund. Other services may have acted on the charge in between. That is why sagas give you **ACD without the I**: no isolation. Other requests can see the half-done state ("paid, not shipped").

Two ways to coordinate a saga:

- **Orchestration**: one component (the orchestrator, often the `orders` service) tells each service what to do, waits for the answer, and runs compensations on failure. The flow is in one place, easy to read and to monitor. The orchestrator must persist its state (which step, which compensations are due), or a crash in the middle loses track.
- **Choreography**: no central brain. Each service reacts to events (`order.placed` → payments charges → `payment.charged` → shipping books → ...), and on failure publishes an event that earlier services react to with their compensation. Loose coupling, but the flow is spread over many services, and cycles and "who undoes what" get hard to follow beyond three or four steps.

```mermaid
sequenceDiagram
    participant O as orders (orchestrator)
    participant I as inventory
    participant P as payments
    participant S as shipping
    O->>I: reserve stock
    I-->>O: reserved
    O->>P: charge 19.99
    P-->>O: charged
    O->>S: book shipping
    S-->>O: FAILED: no courier
    O->>P: refund 19.99
    P-->>O: refunded
    O->>I: release stock
    I-->>O: released
    Note over O: order cancelled
```

Rules for compensations:

- A compensation **must eventually succeed**. Retry it, with backoff, for as long as it takes. If it truly can't (refund API removed), it becomes a ticket for a human, not a skipped step.
- Every step and every compensation must be **idempotent**: the orchestrator retries them too, and messages arrive more than once.
- Put steps that **can't be undone** (send the parcel, send an e-mail) **last**, after every step that may still fail. The last step that can still fail is called the **pivot**.

Each saga message is published with the outbox, and each step is a consumer with an inbox. The three patterns work together.

## Try it

Two containers: PostgreSQL 18 and NATS with JetStream (from [Messaging](messaging.md)).

```console
$ docker run -d --name tutor-pg -p 15432:5432 -e POSTGRES_PASSWORD=tutor postgres:18
$ docker run -d --name tutor-nats -p 14222:4222 nats:2 -js
$ docker exec tutor-pg psql -U postgres -Atc "SELECT version()"
PostgreSQL 18.7 (Debian 18.7-1.pgdg13+5) on x86_64-pc-linux-gnu, compiled by gcc (Debian 14.2.0-19) 14.2.0, 64-bit
```

The schema: orders and their outbox, and the payments service's inbox and charges in a separate schema (in real life: a separate database):

```sql
CREATE TABLE orders (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    customer    text   NOT NULL,
    total_cents bigint NOT NULL CHECK (total_cents > 0),
    status      text   NOT NULL DEFAULT 'pending'
);

CREATE TABLE outbox (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id     uuid   NOT NULL UNIQUE DEFAULT uuidv7(),
    subject      text   NOT NULL,
    payload      jsonb  NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz
);
-- The relay only looks for rows that are not published yet.
CREATE INDEX outbox_unpublished ON outbox (id) WHERE published_at IS NULL;

-- The payments service has its own schema (in real life: its own database).
CREATE SCHEMA payments;
CREATE TABLE payments.inbox (
    event_id    uuid PRIMARY KEY,
    received_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE payments.charges (
    order_id     bigint PRIMARY KEY,
    amount_cents bigint NOT NULL
);
```

```console
$ docker exec -i tutor-pg psql -U postgres -v ON_ERROR_STOP=1 < schema.sql
CREATE TABLE
CREATE TABLE
CREATE INDEX
CREATE SCHEMA
CREATE TABLE
CREATE TABLE
```

`uuidv7()` is new in PostgreSQL 18: a UUID whose first 48 bits are a millisecond timestamp, so IDs made later sort later and the index stays compact. A good default for event IDs.

The Go programs share a scratch module:

```bash
mkdir ~/ds-outbox && cd ~/ds-outbox && go mod init example.com/outbox
go get github.com/nats-io/nats.go@v1.56.0 github.com/jackc/pgx/v5@v5.13.0
```

### The dual-write bug, three ways

`dualwrite` does what most first versions do: insert the order, then publish the event (or the other way round):

```go
// Command dualwrite saves an order and publishes "order placed" the naive
// way: two separate writes to two systems.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func main() {
	order := flag.String("order", "commit-first", "commit-first or publish-first")
	total := flag.Int64("total", 1999, "order total in cents")
	work := flag.Duration("work", 0, "other work between the two writes")
	flag.Parse()
	ctx := context.Background()

	db, err := pgx.Connect(ctx, "postgres://postgres:tutor@localhost:15432/postgres")
	if err != nil {
		log.Fatal(err)
	}
	nc, err := nats.Connect("nats://localhost:14222")
	if err != nil {
		log.Fatal(err)
	}
	js, _ := jetstream.New(nc)
	js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{Name: "EVENTS", Subjects: []string{"events.>"}})

	save := func() (int64, error) {
		var id int64
		err := db.QueryRow(ctx,
			"INSERT INTO orders (customer, total_cents) VALUES ('alice', $1) RETURNING id", *total).Scan(&id)
		return id, err
	}
	publish := func(what string) error {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		_, err := js.Publish(ctx, "events.order.placed", []byte(what))
		return err
	}

	switch *order {
	case "commit-first":
		id, err := save()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("1. order", id, "committed")
		time.Sleep(*work)
		if err := publish(fmt.Sprintf(`{"order_id":%d}`, id)); err != nil {
			fmt.Println("2. publish failed:", err)
			return
		}
		fmt.Println("2. event published")
	case "publish-first":
		if err := publish(`{"customer":"alice"}`); err != nil {
			log.Fatal(err)
		}
		fmt.Println("1. event published")
		if _, err := save(); err != nil {
			fmt.Println("2. insert failed:", err)
			return
		}
		fmt.Println("2. order committed")
	}
}
```

**Way 1: the process dies between the two writes.** `-work 5s` stands for "the handler does something else first", and `kill -9` stands for an OOM kill or a node failure:

```console
$ go build -o bin/ ./dualwrite
$ sh -c './bin/dualwrite -work 5s & sleep 1; kill -9 $!; wait $!; echo "exit $?"'
1. order 1 committed
Killed
exit 137
```

**Way 2: the broker stops answering.** `docker pause` freezes the NATS server while the program waits between the writes:

```console
$ ./bin/dualwrite -order commit-first -work 2s & sleep 1; docker pause tutor-nats; wait; docker unpause tutor-nats
1. order 2 committed
tutor-nats
2. publish failed: context deadline exceeded
tutor-nats
```

**Way 3: publish first, then the insert fails.** A negative total breaks the `CHECK` constraint, like any validation, deadlock or serialization error would:

```console
$ ./bin/dualwrite -order publish-first -total -500
1. event published
2. insert failed: ERROR: new row for relation "orders" violates check constraint "orders_total_cents_check" (SQLSTATE 23514)
```

Now compare the two systems. `peek` is a 30-line program in the same module that prints every message in the `EVENTS` stream (with `Stream.GetMsg`):

```console
$ go run ./peek
EVENTS stores 2 messages
  seq 1 events.order.placed  {"order_id":2}
  seq 2 events.order.placed  {"customer":"alice"}
$ docker exec tutor-pg psql -U postgres -c "SELECT * FROM orders"
 id | customer | total_cents | status
----+----------+-------------+---------
  1 | alice    |        1999 | pending
  2 | alice    |        1999 | pending
(2 rows)
```

All three are wrong, and one is a surprise:

- **Order 1** has no event. Payments will never charge it. Nothing logged an error: the process that could have logged it was dead.
- **Order 2** has an event, even though the program printed `publish failed`. The publish was sitting in the paused server's socket buffer. When the server was unpaused, it stored the message, but the client had already given up. A timeout means **"I don't know"**, not "it failed". If the code had retried, there would be two events; if it had "undone" the order, the event would point to nothing.
- **Message 2** describes an order that was never saved. Payments would charge for nothing.

### The outbox: one transaction, two rows

Start clean. `reset` (in the same module) deletes the `EVENTS` stream:

```console
$ go run ./reset
<nil>
$ docker exec tutor-pg psql -U postgres -qc "TRUNCATE orders, outbox RESTART IDENTITY"
```

`placeorder` writes the order and its event in **one** transaction. `pgx.BeginFunc` commits if the function returns `nil` and rolls back otherwise:

```go
// Command placeorder saves orders and their "order placed" events in one
// database transaction: the transactional outbox.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
)

func main() {
	n := flag.Int("n", 1, "how many orders")
	flag.Parse()
	ctx := context.Background()
	db, err := pgx.Connect(ctx, "postgres://postgres:tutor@localhost:15432/postgres")
	if err != nil {
		log.Fatal(err)
	}
	for i := range *n {
		total := int64(1000 + 100*i)
		err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
			var id int64
			if err := tx.QueryRow(ctx,
				"INSERT INTO orders (customer, total_cents) VALUES ('alice', $1) RETURNING id",
				total).Scan(&id); err != nil {
				return err
			}
			// Same transaction: both rows commit, or neither does.
			_, err := tx.Exec(ctx,
				"INSERT INTO outbox (subject, payload) VALUES ('events.order.placed', $1)",
				map[string]int64{"order_id": id, "total_cents": total})
			if err == nil {
				fmt.Println("order", id, "and its event committed")
			}
			return err
		})
		if err != nil {
			log.Fatal(err)
		}
	}
}
```

```console
$ go build -o bin/ ./placeorder ./relay ./payments
$ ./bin/placeorder -n 6
order 1 and its event committed
order 2 and its event committed
order 3 and its event committed
order 4 and its event committed
order 5 and its event committed
order 6 and its event committed
$ docker exec tutor-pg psql -U postgres -c "SELECT id, event_id, payload, published_at FROM outbox"
 id |               event_id               |               payload                | published_at
----+--------------------------------------+--------------------------------------+--------------
  1 | 01a21bac-dfa0-72db-b916-75566b955dfc | {"order_id": 1, "total_cents": 1000} |
  2 | 01a21bac-dfa3-7cb6-ac7f-ae1a4144fa24 | {"order_id": 2, "total_cents": 1100} |
  3 | 01a21bac-dfa6-7f39-bf18-2745e80aea02 | {"order_id": 3, "total_cents": 1200} |
  4 | 01a21bac-dfaa-70fc-8f6c-f77118f53118 | {"order_id": 4, "total_cents": 1300} |
  5 | 01a21bac-dfad-7582-86c1-3791d3038ffd | {"order_id": 5, "total_cents": 1400} |
  6 | 01a21bac-dfb0-7565-87a1-31ce84bcd11d | {"order_id": 6, "total_cents": 1500} |
(6 rows)
```

The `event_id` values are UUIDv7: see how they start with the same time prefix and count up. Nothing is published yet, and that's fine. The events are **safe**: they are committed, and they can't exist without their order.

### The relay, twice, with `SKIP LOCKED`

The relay takes up to `-batch` rows in a transaction, publishes each one with the event ID as the JetStream **message ID** (`Nats-Msg-Id` header), marks them published and commits. `-slow` adds a delay per publish so two relays overlap:

```go
// relayBatch locks up to batch unpublished rows, publishes them, marks them
// published and commits. Rows locked by another relay are skipped.
func relayBatch(ctx context.Context, db *pgx.Conn, js jetstream.JetStream,
	name string, batch int, slow time.Duration, crash bool) (int, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT id, event_id::text, subject, payload::text FROM outbox
		WHERE published_at IS NULL
		ORDER BY id LIMIT $1
		FOR UPDATE SKIP LOCKED`, batch)
	if err != nil {
		return 0, err
	}
	type event struct {
		ID                        int64
		EventID, Subject, Payload string
	}
	events, err := pgx.CollectRows(rows, pgx.RowToStructByPos[event])
	if err != nil {
		return 0, err
	}

	var ids []int64
	for _, e := range events {
		time.Sleep(slow)
		ack, err := js.Publish(ctx, e.Subject, []byte(e.Payload), jetstream.WithMsgID(e.EventID))
		if err != nil {
			return 0, err // rollback: the rows stay unpublished, we try again later
		}
		fmt.Printf("%s outbox id %d -> stream seq %d duplicate=%v\n", name, e.ID, ack.Sequence, ack.Duplicate)
		if crash {
			fmt.Println(name, "crashes before commit")
			os.Exit(1)
		}
		ids = append(ids, e.ID)
	}
	if _, err := tx.Exec(ctx, "UPDATE outbox SET published_at = now() WHERE id = ANY($1)", ids); err != nil {
		return 0, err
	}
	return len(events), tx.Commit(ctx)
}
```

`main` connects, creates the stream, and calls `relayBatch` in a loop until it returns 0 rows (a real relay would sleep and poll again instead of exiting). Two relays at once:

```console
$ sh -c './bin/relay -name r1 -slow 300ms & ./bin/relay -name r2 -slow 300ms & wait'
r2 outbox id 1 -> stream seq 1 duplicate=false
r1 outbox id 3 -> stream seq 2 duplicate=false
r1 outbox id 4 -> stream seq 3 duplicate=false
r2 outbox id 2 -> stream seq 4 duplicate=false
r1 outbox empty, done
r2 outbox id 5 -> stream seq 5 duplicate=false
r2 outbox id 6 -> stream seq 6 duplicate=false
r2 outbox empty, done
$ go run ./peek
EVENTS stores 6 messages
  seq 1 events.order.placed 01a21bac-dfa0-72db-b916-75566b955dfc {"order_id": 1, "total_cents": 1000}
  seq 2 events.order.placed 01a21bac-dfa6-7f39-bf18-2745e80aea02 {"order_id": 3, "total_cents": 1200}
  seq 3 events.order.placed 01a21bac-dfaa-70fc-8f6c-f77118f53118 {"order_id": 4, "total_cents": 1300}
  seq 4 events.order.placed 01a21bac-dfa3-7cb6-ac7f-ae1a4144fa24 {"order_id": 2, "total_cents": 1100}
  seq 5 events.order.placed 01a21bac-dfad-7582-86c1-3791d3038ffd {"order_id": 5, "total_cents": 1400}
  seq 6 events.order.placed 01a21bac-dfb0-7565-87a1-31ce84bcd11d {"order_id": 6, "total_cents": 1500}
```

- **No row was published twice.** r2 locked rows 1 and 2; r1's query skipped them and took 3 and 4. Without `SKIP LOCKED`, r1 would have waited for r2's commit, then found nothing, so the second relay would add availability but no speed.
- **The order changed.** Order 2 is stream sequence 4, after orders 3 and 4. Each relay is in order; together they are not. For "order placed" events of different orders that's usually fine. For several events of **one** order (`placed`, `cancelled`), it is not: see Traps.
- r1 said "outbox empty" while rows 5 and 6 were still there. They were locked by r2, so for r1 they did not exist. A real relay just polls again.

### The relay crashes after publishing

One more order. This time the relay publishes and dies before its `UPDATE ... COMMIT`:

```console
$ ./bin/placeorder
order 7 and its event committed
$ ./bin/relay -crash-after-publish; echo "exit $?"
r1 outbox id 7 -> stream seq 7 duplicate=false
r1 crashes before commit
exit 1
$ docker exec tutor-pg psql -U postgres -c "SELECT id, published_at FROM outbox WHERE id = 7"
 id | published_at
----+--------------
  7 |
(1 row)

$ ./bin/relay
r1 outbox id 7 -> stream seq 7 duplicate=true
r1 outbox empty, done
$ go run ./peek | head -1
EVENTS stores 7 messages
```

The transaction rolled back, so row 7 was still unpublished, and the next relay published it **again**. That's the at-least-once part of the outbox, and you can't avoid it. Here JetStream caught it: same `Nats-Msg-Id` within the stream's **duplicate window** (2 minutes by default, `StreamConfig.Duplicates`), so it answered `duplicate=true` with the original sequence 7 and stored nothing new.

Don't rely on that alone. A relay that is down for 3 minutes is outside the window. Kafka's idempotent producer only dedupes retries of **one** producer session, not a new process. The consumer needs its own protection.

### The inbox: a redelivered event does nothing

The payments consumer: durable consumer `payments`, `AckWait: 3s`. For each event it inserts the event ID into `payments.inbox` and the charge in one transaction, then acks. `-crash-before-ack 3` makes it exit after committing order 3, before the ack:

```go
	for {
		m, err := c.Next(jetstream.FetchMaxWait(5 * time.Second))
		if errors.Is(err, nats.ErrTimeout) {
			fmt.Println("no more events")
			return
		} else if err != nil {
			log.Fatal(err)
		}
		md, _ := m.Metadata()
		eventID := m.Headers().Get(jetstream.MsgIDHeader)
		var ev struct {
			OrderID    int64 `json:"order_id"`
			TotalCents int64 `json:"total_cents"`
		}
		json.Unmarshal(m.Data(), &ev)

		err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx,
				"INSERT INTO payments.inbox (event_id) VALUES ($1) ON CONFLICT DO NOTHING", eventID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return errDuplicate // seen before: rollback, do nothing
			}
			_, err = tx.Exec(ctx,
				"INSERT INTO payments.charges (order_id, amount_cents) VALUES ($1, $2)",
				ev.OrderID, ev.TotalCents)
			return err
		})
		switch {
		case errors.Is(err, errDuplicate):
			fmt.Printf("order %d: delivery %d, already in inbox, skip\n", ev.OrderID, md.NumDelivered)
		case err != nil:
			log.Fatal(err) // no ack: the event comes back later
		default:
			fmt.Printf("order %d: delivery %d, charged %d cents\n", ev.OrderID, md.NumDelivered, ev.TotalCents)
		}
		if ev.OrderID == *crashOn {
			fmt.Println("crash before ack")
			os.Exit(1)
		}
		m.DoubleAck(ctx)
	}
```

```console
$ ./bin/payments -crash-before-ack 3; echo "exit $?"
order 1: delivery 1, charged 1000 cents
order 3: delivery 1, charged 1200 cents
crash before ack
exit 1
$ ./bin/payments
order 4: delivery 1, charged 1300 cents
order 2: delivery 1, charged 1100 cents
order 5: delivery 1, charged 1400 cents
order 6: delivery 1, charged 1500 cents
order 7: delivery 1, charged 1000 cents
order 3: delivery 2, already in inbox, skip
no more events
$ docker exec tutor-pg psql -U postgres -c "SELECT order_id, amount_cents FROM payments.charges ORDER BY order_id" -c "SELECT count(*) FROM payments.inbox"
 order_id | amount_cents
----------+--------------
        1 |         1000
        2 |         1100
        3 |         1200
        4 |         1300
        5 |         1400
        6 |         1500
        7 |         1000
(7 rows)

 count
-------
     7
(1 row)
```

The charge for order 3 was committed, the ack was not. JetStream redelivered it after `AckWait` (`delivery 2`), **after** the newer events, because the unacked message waited for its timer. The inbox saw the ID and skipped it. Seven orders, seven charges, seven inbox rows. And the stream order (3 before 2, then 3 again at the end) shows once more that consumers must not assume order.

(Here `charges.order_id` is also a primary key, so a second charge would fail anyway. Real tables often don't have such a natural key: a customer can be charged twice legitimately. The inbox works for every kind of event.)

### A saga with compensation

The saga is simulated in one Go program, so you can see the logic without three services. Each step has `do` and `undo`. The orchestrator runs the steps in order; on failure it runs the `undo` of every step that already succeeded, newest first, and retries each one until it works:

```go
// Command saga runs the "place order" saga with an orchestrator. Each step
// has a compensation that undoes it with a new, forward action.
package main

import (
	"errors"
	"flag"
	"fmt"
)

type step struct {
	name     string
	do, undo func() error
}

// run executes steps in order. When one fails, it runs the compensations of
// the steps that already succeeded, newest first.
func run(steps []step) error {
	for i, s := range steps {
		if err := s.do(); err != nil {
			fmt.Printf("  %-16s FAILED: %v\n", s.name, err)
			for j := i - 1; j >= 0; j-- {
				// A compensation must succeed eventually: retry it (with backoff in
				// real life), never skip it.
				for attempt := 1; ; attempt++ {
					err := steps[j].undo()
					if err == nil {
						break
					}
					fmt.Printf("  undo %s, attempt %d: %v\n", steps[j].name, attempt, err)
				}
			}
			return fmt.Errorf("saga aborted at %s: %w", s.name, err)
		}
	}
	return nil
}

func main() {
	fail := flag.String("fail", "", "step that fails: payment or shipping")
	flakyRefund := flag.Int("flaky-refund", 0, "refund fails this many times first")
	flag.Parse()

	stock, balance := 5, 10_000 // the other services' state, in memory
	var ledger []string

	steps := []step{
		{"reserve stock",
			func() error { stock--; fmt.Println("  reserve stock    stock now", stock); return nil },
			func() error { stock++; fmt.Println("  undo reserve     release stock, now", stock); return nil }},
		{"charge payment",
			func() error {
				if *fail == "payment" {
					return errors.New("card declined")
				}
				balance -= 1999
				ledger = append(ledger, "charge -1999")
				fmt.Println("  charge payment   balance now", balance)
				return nil
			},
			func() error {
				if *flakyRefund > 0 {
					*flakyRefund--
					return errors.New("payments service unavailable")
				}
				balance += 1999
				ledger = append(ledger, "refund +1999")
				fmt.Println("  undo charge      refund, balance now", balance)
				return nil
			}},
		{"book shipping",
			func() error {
				if *fail == "shipping" {
					return errors.New("no courier for this address")
				}
				fmt.Println("  book shipping    label SHIP-42")
				return nil
			},
			func() error { fmt.Println("  undo shipping    cancel label"); return nil }},
	}

	fmt.Println("order 42: pending")
	if err := run(steps); err != nil {
		fmt.Println("order 42: cancelled,", err)
	} else {
		fmt.Println("order 42: confirmed")
	}
	fmt.Println("stock", stock, "balance", balance, "ledger", ledger)
}
```

The happy path, then a failure at each step:

```console
$ go build -o bin/ ./saga
$ ./bin/saga
order 42: pending
  reserve stock    stock now 4
  charge payment   balance now 8001
  book shipping    label SHIP-42
order 42: confirmed
stock 4 balance 8001 ledger [charge -1999]
$ ./bin/saga -fail payment
order 42: pending
  reserve stock    stock now 4
  charge payment   FAILED: card declined
  undo reserve     release stock, now 5
order 42: cancelled, saga aborted at charge payment: card declined
stock 5 balance 10000 ledger []
$ ./bin/saga -fail shipping
order 42: pending
  reserve stock    stock now 4
  charge payment   balance now 8001
  book shipping    FAILED: no courier for this address
  undo charge      refund, balance now 10000
  undo reserve     release stock, now 5
order 42: cancelled, saga aborted at book shipping: no courier for this address
stock 5 balance 10000 ledger [charge -1999 refund +1999]
```

Ticket 3 is fixed: shipping failed, so the orchestrator refunded and released the stock, newest step first. Look at the **ledger**: the balance is back to 10,000, but the history has two entries, a charge and a refund. The customer saw the charge. That's a compensation, not a rollback.

Now the refund itself fails twice (the payments service is restarting):

```console
$ ./bin/saga -fail shipping -flaky-refund 2
order 42: pending
  reserve stock    stock now 4
  charge payment   balance now 8001
  book shipping    FAILED: no courier for this address
  undo charge payment, attempt 1: payments service unavailable
  undo charge payment, attempt 2: payments service unavailable
  undo charge      refund, balance now 10000
  undo reserve     release stock, now 5
order 42: cancelled, saga aborted at book shipping: no courier for this address
stock 5 balance 10000 ledger [charge -1999 refund +1999]
```

The orchestrator kept trying, and the saga still ended clean. In a real system this loop is not in memory: the saga state ("step 3 failed, compensations 2 and 1 due") is a row in the orchestrator's database, each command goes out through the outbox, and a crash of the orchestrator resumes from that row. Workflow engines (Temporal, for example) do exactly this bookkeeping for you.

Clean up:

```bash
docker rm -f tutor-pg tutor-nats
```

## Traps

- **Publishing inside the transaction "to be safe".** If `COMMIT` then fails, the event is out and the data is not. It's the publish-first bug.
- **Treating a publish timeout as "not published".** The run above stored a message whose publish "failed". Timeouts are unknown outcomes. Only idempotency makes a retry safe.
- **No event ID, or a new ID on every retry.** The consumer can't dedupe. Make the ID when you write the outbox row, never in the relay.
- **Inbox in a different transaction than the work.** Insert-ID-then-crash marks the event as done without doing it; work-then-crash does it twice. Same transaction, or it is not an inbox.
- **Several relays and events that must stay in order.** `SKIP LOCKED` relays reorder. For per-entity order: one relay (with a lock or leader election for failover), or partition the work by entity (`WHERE order_id % 4 = $1`), and use a per-entity key or subject at the broker.
- **An outbox table that only grows.** Delete published rows (or drop daily partitions). A huge outbox makes every relay poll slower and bloats the table (see [MVCC and locks](../databases/mvcc-and-locks.md)).
- **Log-tailing relays with abandoned replication slots.** WAL piles up on the primary until the disk is full. Monitor slot lag (see [Replication and scaling](../databases/replication.md)).
- **Compensations that can fail for good.** "Refund" must be retried until it works, and be idempotent. If you can't build that, add a human queue for it, not a log line.
- **Irreversible steps early in the saga.** Sending the parcel before charging the card means a declined card has no compensation. Order the steps so that the irreversible ones come after the pivot.
- **Forgetting that sagas are not isolated.** Between "charged" and "refunded", other services see a paid order. Use status fields (`pending`, `confirmed`, `cancelled`) and don't let other flows act on `pending` as if it were final.

## Check yourself

??? question "1. Why can't you fix the commit-then-publish bug by retrying the publish 5 times?"

    The bug includes the case where the process dies after `COMMIT`. Then nobody is left to retry,
    and the event is lost silently. Retries also don't help when a timeout hides a publish that did
    happen: you get duplicates. The outbox stores the "intent to publish" durably with the data.

??? question "2. The relay published outbox row 42, then the database connection broke before `COMMIT`. What happens next, and which component makes it harmless?"

    The transaction rolls back, so row 42 is still unpublished. The next relay run publishes it
    again: a duplicate. The broker may drop it (JetStream message-ID dedupe within the window), but
    the guarantee comes from the consumer's inbox, which sees the same event ID and skips the work.

??? question "3. Two relays run `SELECT ... FOR UPDATE SKIP LOCKED LIMIT 10`. What would change with plain `FOR UPDATE`? And without `FOR UPDATE` at all?"

    Plain `FOR UPDATE`: the second relay waits for the first one's locks, then sees the rows as
    published (or re-checks them) and does nothing useful: no parallelism. Without `FOR UPDATE`:
    both relays read the same rows and publish every event twice.

??? question "4. A payments consumer first calls the card provider, then inserts the event ID into its inbox and commits. What goes wrong, and how do you fix it?"

    The provider call is not in the database transaction. A crash after the call and before
    `COMMIT` leaves no inbox row, so the redelivery charges again. Fix: send the event ID (or an
    ID derived from it) to the provider as its idempotency key, so the provider dedupes the
    second call. The inbox alone protects only side effects in the same database.

??? question "5. In the saga, the shipping step fails. In which order do compensations run, and why that order?"

    Newest first: refund the payment, then release the stock. It's the reverse of the steps, like
    unwinding a stack. Later steps may depend on earlier ones (the charge was made because stock was
    reserved; a shipping label points to a reservation), so you undo the dependent step before the
    step it depends on.

??? question "6. Orchestration or choreography for a 6-step order flow with 4 teams? Give one reason each way."

    Orchestration: the whole flow and its compensations are in one place, easy to see, test and
    monitor ("which orders are stuck at step 4?"). Choreography: no central service that every
    team depends on, and services stay loosely coupled. With 6 steps, most teams choose
    orchestration, because a choreographed failure path across 6 services is hard to follow.

## Recap

- **Dual write** (database + broker, no shared transaction) breaks either way round: commit-then-publish loses events, publish-then-commit invents them. Timeouts hide what really happened.
- **Transactional outbox**: write the event to an `outbox` table in the **same** transaction as the data. A **relay** publishes and marks rows. It is at-least-once, so every event has a stable **event ID**.
- Several relays share the work with `FOR UPDATE SKIP LOCKED`. They **reorder** events across rows.
- The broker may dedupe (JetStream `Nats-Msg-Id`, 2-minute window), but the real guarantee is the consumer's **inbox**: insert the event ID with `ON CONFLICT DO NOTHING` in the same transaction as the work.
- A **saga** is a chain of local transactions. On failure, run **compensations** (new actions like refunds) in reverse order, retry them until they succeed, and make every step idempotent. Sagas have no isolation.
- **Orchestration** keeps the flow in one place; **choreography** spreads it over events. Put irreversible steps after the pivot.

Next: how a group of servers agrees on one value, even when some of them crash. [Consensus and Raft](consensus-and-raft.md).
