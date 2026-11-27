# How data is stored

## Why care?

Three tickets that all trace back to this page:

- "We changed one status column on one row, and the database wrote 30 kB."
- "The first query after a restart is slow. The same query a second later is fast."
- "Disk usage keeps growing, but we never add rows. We only update them."

A database is not magic. It is a program that keeps your rows in files, in fixed-size blocks, and writes a diary of every change before it touches those blocks. Once you can see the blocks and the diary, most "strange" database behaviour stops being strange.

On this page you look inside a real PostgreSQL table, byte by byte, and inside its write-ahead log. At the end you compare PostgreSQL's design (B-trees, update in place) with the other big family (LSM trees, used by RocksDB and Cassandra).

## The idea: a ring binder and a diary

Think of a big ring binder in an office.

- Every **page** in the binder has the same size. You never write half a row on one page and half on the next. You take out a whole page, change it, and put it back.
- Each page has a small **table of contents** at the top: "row 1 starts at line 40, row 2 at line 32...". Rows are written from the bottom of the page up. The table of contents grows from the top down. When they meet, the page is full.
- Next to the binder lies a **diary**. Before anybody changes a page, they first write one line in the diary: "page 500, row 1: status is now `cancelled`." The diary is only ever written at the end, never in the middle, so writing to it is fast.
- Once in a while, somebody copies all changed pages back into the archive cabinet. That is a **checkpoint**. After it, the old diary lines are not needed anymore.

If the office burns down (a crash), you take the archive pages and replay the diary from the last checkpoint. Nothing is lost.

```mermaid
flowchart LR
    C[Client: UPDATE] --> B["Shared buffers<br/>(8 kB pages in RAM)"]
    C --> W["WAL buffers"]
    W -->|"COMMIT: write + fsync"| WAL[("pg_wal/<br/>16 MB segment files")]
    B -->|"checkpoint, background writer<br/>(later, in bulk)"| HEAP[("base/&lt;db&gt;/&lt;table&gt;<br/>table files")]
    WAL -.->|"after a crash: replay"| HEAP
```

The important arrow is the one with `fsync`. A commit is durable when its diary line is on disk. The table pages can follow minutes later.

## Setup: your own PostgreSQL

All pages in this chapter use one PostgreSQL 18 container called `tutor-pg`. Start it once:

```console
$ docker run -d --name tutor-pg -e POSTGRES_PASSWORD=tutor \
    -p 127.0.0.1:55432:5432 postgres:18 \
    -c wal_level=logical -c shared_preload_libraries=pg_stat_statements
1650a00de17bb2a4b3fef441e5188c4584bb4fa3d932d164bdec027f81b4b8b4
$ docker exec tutor-pg psql -U postgres -c 'SELECT version();'
                                                      version
--------------------------------------------------------------------------------------------------------------------
 PostgreSQL 18.7 (Debian 18.7-1.pgdg13+5) on x86_64-pc-linux-gnu, compiled by gcc (Debian 14.2.0-19) 14.2.0, 64-bit
(1 row)
```

- `-p 127.0.0.1:55432:5432` publishes the port only on loopback, and not on 5432, so it does not clash with a PostgreSQL you may already have.
- The two `-c` options are server settings. [Replication](replication.md) needs `wal_level=logical`, and [Query planning](query-planning.md) needs `pg_stat_statements`. They do nothing on this page.
- Give it a second after `docker run`. The image first creates the database cluster, then restarts the server. `docker logs tutor-pg` ends with `database system is ready to accept connections` when it is done.

You don't need `psql` on your machine. It is inside the image. Each page gets its own database:

```console
$ docker exec tutor-pg createdb -U postgres tutor_storage
$ docker exec -it tutor-pg psql -U postgres -d tutor_storage
tutor_storage=#
```

From Go, the address is `postgres://postgres:tutor@127.0.0.1:55432/tutor_storage`. [Go and SQL](go-and-sql.md) uses it. When you are done with the chapter, `docker rm -f tutor-pg` removes everything.

The examples below are typed at that `tutor_storage=#` prompt. Some inspection functions come from extensions that ship with PostgreSQL. Turn them on in your database:

```sql
CREATE EXTENSION pageinspect;    -- read raw pages
CREATE EXTENSION pg_walinspect;  -- read the WAL from SQL
CREATE EXTENSION pg_buffercache; -- see what is in shared buffers
```

They read raw internals, so only a superuser may use them. Never install them for your application's user in production.

## Under the hood: pages, tuples, and the heap

Create a small `orders` table, like the one in the [orders service](../go/index.md):

```console
tutor_storage=# CREATE TABLE orders (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    customer_id text NOT NULL,
    status      text NOT NULL DEFAULT 'pending',
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE
tutor_storage=# INSERT INTO orders (customer_id) VALUES ('alice'), ('bob'), ('carol');
INSERT 0 3
tutor_storage=# SELECT ctid, customer_id, status FROM orders;
 ctid  | customer_id | status
-------+-------------+---------
 (0,1) | alice       | pending
 (0,2) | bob         | pending
 (0,3) | carol       | pending
(3 rows)
```

`uuidv7()` is new in PostgreSQL 18. Version 7 UUIDs start with a timestamp, so new IDs are close to each other. That matters for indexes, see [Indexes](indexes.md).

`ctid` is a hidden column on every table: the **physical address** of the row. `(0,2)` means "page 0, item 2". It is the "line number" the indexes use to find a row.

### The table is a file

```console
tutor_storage=# SELECT pg_relation_filepath('orders');
 pg_relation_filepath
----------------------
 base/16389/16464
(1 row)
```

That path is relative to the data directory (`/var/lib/postgresql/18/docker` in this image). `16389` is the database, `16464` is the table's **file node**. Look at it from outside PostgreSQL:

```console
$ docker exec tutor-pg ls -l /var/lib/postgresql/18/docker/base/16389/ | grep 16464
-rw------- 1 postgres postgres   8192 Nov 26 22:19 16464
```

Exactly 8192 bytes: one page. A table file always grows by whole pages. PostgreSQL calls this file the **heap**: rows are put wherever there is space, in no particular order. (Not the Go heap, and not the heap data structure. Same word, different idea.) When the file reaches 1 GB, PostgreSQL starts a second file, `16464.1`. Next to it you will later see `16464_fsm` (the free space map: which pages have room) and `16464_vm` (the visibility map, see [MVCC and locks](mvcc-and-locks.md)).

### Inside one page

`pageinspect` reads a page and decodes it. First the 24-byte page header:

```console
tutor_storage=# SELECT * FROM page_header(get_raw_page('orders', 0));
    lsn    | checksum | flags | lower | upper | special | pagesize | version | prune_xid
-----------+----------+-------+-------+-------+---------+----------+---------+-----------
 0/2DD5090 |        0 |     0 |    36 |  8000 |    8192 |     8192 |       4 |         0
(1 row)
```

- **`lsn`**: the position in the WAL of the last change to this page. Remember it, it comes back below.
- **`lower`**: where the line pointer array ends. 24 bytes of header plus 3 line pointers of 4 bytes = 36.
- **`upper`**: where the row data starts. Rows grow down from the end, so the free space is the gap between 36 and 8000.
- **`special`** = 8192: tables have no "special space". B-tree pages use it, see [Indexes](indexes.md).
- **`checksum`** is 0 here because this is the copy in memory. PostgreSQL computes the checksum when it writes the page to disk. (Since PostgreSQL 18, new clusters have data checksums on by default.)

Then the line pointers and the tuple headers. A **tuple** is PostgreSQL's word for one version of a row:

```console
tutor_storage=# SELECT lp, lp_off, lp_len, t_xmin, t_xmax, t_ctid
FROM heap_page_items(get_raw_page('orders', 0));
 lp | lp_off | lp_len | t_xmin | t_xmax | t_ctid
----+--------+--------+--------+--------+--------
  1 |   8128 |     64 |    773 |      0 | (0,1)
  2 |   8064 |     64 |    773 |      0 | (0,2)
  3 |   8000 |     64 |    773 |      0 | (0,3)
(3 rows)
```

Each row takes 64 bytes: a 24-byte tuple header (23 bytes, padded), 16 bytes of UUID, the two short texts (1 length byte plus the letters), and 8 bytes of timestamp, with some padding so the timestamp starts at a multiple of 8. `t_xmin` is the transaction that created this version (all three came from one `INSERT`, transaction 773).

```mermaid
flowchart TB
    subgraph PAGE ["One 8 kB page"]
        direction TB
        H["Page header: 24 bytes (lsn, checksum, lower, upper...)"]
        LP["Line pointers: lp 1 → 8128, lp 2 → 8064, lp 3 → 8000 (4 bytes each, grow down)"]
        FREE["free space"]
        T3["tuple 3 (carol) at 8000"]
        T2["tuple 2 (bob) at 8064"]
        T1["tuple 1 (alice) at 8128"]
        H --- LP --- FREE --- T3 --- T2 --- T1
    end
```

Why the extra line pointer step? An index stores `(0,2)`, not "byte 8064". PostgreSQL may move tuples around inside the page to remove holes, and only has to change the line pointer. Every index entry stays correct.

### An UPDATE writes a new row

Now change bob's order:

```console
tutor_storage=# UPDATE orders SET status = 'paid' WHERE customer_id = 'bob';
UPDATE 1
tutor_storage=# SELECT ctid, customer_id, status FROM orders;
 ctid  | customer_id | status
-------+-------------+---------
 (0,1) | alice       | pending
 (0,3) | carol       | pending
 (0,4) | bob         | paid
(3 rows)
tutor_storage=# SELECT lp, lp_off, lp_len, t_xmin, t_xmax, t_ctid
FROM heap_page_items(get_raw_page('orders', 0));
 lp | lp_off | lp_len | t_xmin | t_xmax | t_ctid
----+--------+--------+--------+--------+--------
  1 |   8128 |     64 |    773 |      0 | (0,1)
  2 |   8064 |     64 |    773 |    774 | (0,4)
  3 |   8000 |     64 |    773 |      0 | (0,3)
  4 |   7936 |     64 |    774 |      0 | (0,4)
(4 rows)
```

Bob moved from `(0,2)` to `(0,4)`. PostgreSQL did not overwrite the old row. It wrote a **new version** (created by transaction 774), and marked the old one as "deleted by 774" (`t_xmax`), with a forward pointer `t_ctid = (0,4)`. The old version stays on the page until `VACUUM` removes it. Other transactions that started before 774 may still need to see "pending". That is **MVCC**, and it gets its own page: [MVCC and locks](mvcc-and-locks.md).

The order of rows in `SELECT` changed too. Without `ORDER BY`, a table has no order. You get the rows in the order they happen to lie in the file.

Don't trust `pageinspect`? Ask the disk. After a `CHECKPOINT` (more on that below), the page is in the file, and `od` shows both bobs:

```console
$ docker exec tutor-pg sh -c 'od -A d -t x2 -N 24 /var/lib/postgresql/18/docker/base/16389/16464'
0000000 0000 0000 5268 02dd d0c2 0000 0028 1f00
0000016 2000 2004 0306 0000
0000024
$ docker exec tutor-pg sh -c 'od -A d -c -j 7936 /var/lib/postgresql/18/docker/base/16389/16464 | head -12'
...
0007968 223  \v 305 217 235   ( 226   9  \t   b   o   b  \v   p   a   i
0007984   d  \0  \0  \0  \0  \0  \0  \0   v   &   e 033   4 004 003  \0
...
0008096 223  \v 305 217 235   ( 226   9  \t   b   o   b 021   p   e   n
0008112   d   i   n   g  \0  \0  \0  \0   v   &   e 033   4 004 003  \0
```

(`...` marks lines left out.) Read the header in 2-byte little-endian words: bytes 0-7 are the LSN, `d0c2` at byte 8 is the checksum (not 0 anymore, because this is the disk copy), `0028` is `lower` = 40 (four line pointers now), `1f00` is `upper` = 7936. Further down: the same UUID bytes twice, once with `bob paid`, once with `bob pending`. Disk space for a deleted row version is only given back when `VACUUM` runs. That is the answer to "we only update, and the disk grows".

### Many rows

Add 100,000 orders and look at the size:

```console
tutor_storage=# INSERT INTO orders (customer_id, status, created_at)
SELECT 'customer-' || (i % 5000), (ARRAY['pending','paid','shipped','cancelled'])[1 + i % 4],
       now() - i * interval '1 minute'
FROM generate_series(1, 100000) AS i;
INSERT 0 100000
tutor_storage=# VACUUM ANALYZE orders;
VACUUM
tutor_storage=# SELECT relpages, reltuples, pg_size_pretty(pg_relation_size('orders')) AS heap,
       pg_size_pretty(pg_indexes_size('orders')) AS indexes
FROM pg_class WHERE relname = 'orders';
 relpages | reltuples |  heap   | indexes
----------+-----------+---------+---------
      935 |    100003 | 7480 kB | 3104 kB
(1 row)
```

About 107 rows per page. The primary key index is already 3 MB, almost half the size of the table. Keep that in mind on the next page.

### TOAST: when a value does not fit

A row must fit in one page. So what happens with a 10 kB text? Add a `note` column:

```console
tutor_storage=# ALTER TABLE orders ADD COLUMN note text;
ALTER TABLE
tutor_storage=# SELECT reltoastrelid::regclass FROM pg_class WHERE relname = 'orders';
      reltoastrelid
-------------------------
 pg_toast.pg_toast_16464
(1 row)
```

Every table with a variable-length column has a hidden **TOAST table** ("The Oversized-Attribute Storage Technique"). When a row gets bigger than about 2 kB, PostgreSQL first tries to compress the big values, and if that is not enough, moves them out to the TOAST table in chunks. The row keeps only a small pointer.

```console
tutor_storage=# UPDATE orders SET note = repeat('gift wrap please. ', 500) WHERE customer_id = 'alice';
UPDATE 1
tutor_storage=# UPDATE orders SET note = (SELECT string_agg(md5(i::text), '') FROM generate_series(1, 300) i)
WHERE customer_id = 'carol';
UPDATE 1
tutor_storage=# SELECT customer_id, length(note) AS chars, pg_column_size(note) AS stored_bytes,
       pg_column_compression(note) AS compression
FROM orders WHERE customer_id IN ('alice', 'carol');
 customer_id | chars | stored_bytes | compression
-------------+-------+--------------+-------------
 carol       |  9600 |         9600 |
 alice       |  9000 |          132 | pglz
(2 rows)
tutor_storage=# SELECT chunk_id, chunk_seq, length(chunk_data) FROM pg_toast.pg_toast_16464 ORDER BY chunk_seq;
 chunk_id | chunk_seq | length
----------+-----------+--------
    16555 |         0 |   1996
    16555 |         1 |   1996
    16555 |         2 |   1996
    16555 |         3 |   1996
    16555 |         4 |   1616
(5 rows)
```

- Alice's note repeats itself, so `pglz` squeezed 9000 characters into 132 bytes. It stays in the row.
- Carol's note is random hex. It doesn't compress well, so it went to the TOAST table in five chunks of about 2 kB.

The practical point: `SELECT *` on a table with big JSON or text columns reads the TOAST table too, for every row. Select only the columns you need. And a query that doesn't touch `note` never pays for it.

## Under the hood: shared buffers

PostgreSQL never reads a row from the file directly. It reads the whole **page** into **shared buffers**, a big array of 8 kB slots in shared memory (128 MB by default, `shared_buffers`), and works on the page there. `pg_buffercache` shows what is in it:

```console
tutor_storage=# SELECT count(*) AS buffers, pg_size_pretty(count(*) * 8192) AS size
FROM pg_buffercache WHERE relfilenode = pg_relation_filenode('orders');
 buffers |  size
---------+---------
     937 | 7496 kB
(1 row)
```

The whole table is cached (935 heap pages, plus a free space map and a visibility map page). PostgreSQL 18 can throw a relation out of the cache on purpose. Do that, and count pages with `EXPLAIN (ANALYZE, BUFFERS)`:

```console
tutor_storage=# SELECT * FROM pg_buffercache_evict_relation('orders');
 buffers_evicted | buffers_flushed | buffers_skipped
-----------------+-----------------+-----------------
             937 |             336 |               0
(1 row)
tutor_storage=# EXPLAIN (ANALYZE, BUFFERS, COSTS OFF) SELECT count(*) FROM orders;
                                 QUERY PLAN
----------------------------------------------------------------------------
 Aggregate (actual time=10.908..10.909 rows=1.00 loops=1)
   Buffers: shared read=935
   ->  Seq Scan on orders (actual time=0.149..6.936 rows=100003.00 loops=1)
         Buffers: shared read=935
 Planning:
   Buffers: shared hit=53 read=5
 Planning Time: 0.318 ms
 Execution Time: 10.931 ms
(8 rows)
tutor_storage=# EXPLAIN (ANALYZE, BUFFERS, COSTS OFF) SELECT count(*) FROM orders;
                                 QUERY PLAN
----------------------------------------------------------------------------
 Aggregate (actual time=8.449..8.450 rows=1.00 loops=1)
   Buffers: shared hit=935
   ->  Seq Scan on orders (actual time=0.003..4.147 rows=100003.00 loops=1)
         Buffers: shared hit=935
 Planning Time: 0.029 ms
 Execution Time: 8.459 ms
(6 rows)
```

- **`read=935`**: 935 pages were not in shared buffers and had to be asked from the kernel.
- **`hit=935`**: all pages were found in shared buffers.
- **`buffers_flushed = 336`**: 336 of the cached pages were **dirty** (changed in memory, not yet in the file). Evicting them meant writing them out first.

Why is the cold run only 2 ms slower? Because "read" means a `read()` system call, and the kernel had those pages in its own **page cache** (see [Filesystems](../linux/filesystems.md#under-the-hood-the-page-cache)). PostgreSQL caches twice: in shared buffers, and in the kernel's page cache below it. After a server restart, or on a new replica, both are cold and the first queries really go to disk. That is the "slow after restart" ticket. `BUFFERS` numbers are the stable thing to compare; milliseconds depend on what the kernel happens to cache.

## Under the hood: the write-ahead log

The rule is in the name: **write the log ahead** of the data. Before a changed page may be written to the table file, the WAL record that describes the change must be on disk. On `COMMIT`, PostgreSQL writes the WAL up to the commit record and calls `fsync` (here `fdatasync`, see `wal_sync_method`). It does **not** write the table pages. They stay dirty in shared buffers.

Every byte in the WAL has an address, the **LSN** (log sequence number), written like `0/1996EA20`. It only grows. Watch one `INSERT`:

```console
tutor_storage=# SELECT pg_current_wal_lsn() AS before \gset
tutor_storage=# INSERT INTO orders (customer_id) VALUES ('dave');
INSERT 0 1
tutor_storage=# SELECT pg_current_wal_lsn() AS after \gset
tutor_storage=# SELECT :'before' AS before, :'after' AS after, pg_wal_lsn_diff(:'after', :'before') AS bytes;
   before   |   after    | bytes
------------+------------+-------
 0/1996EA20 | 0/1996EAF8 |   216
(1 row)
tutor_storage=# SELECT start_lsn, resource_manager AS rmgr, record_type, record_length AS len,
       fpi_length AS fpi, description
FROM pg_get_wal_records_info(:'before', :'after');
 start_lsn  |    rmgr     | record_type | len | fpi |          description
------------+-------------+-------------+-----+-----+-------------------------------
 0/1996EA20 | Heap        | INSERT      |  95 |   0 | off: 109, flags: 0x08
 0/1996EA80 | Btree       | INSERT_LEAF |  72 |   0 | off: 41
 0/1996EAC8 | Transaction | COMMIT      |  46 |   0 | 2026-11-26 22:20:27.072575+00
(3 rows)
```

(`\gset` stores a query result in a `psql` variable, `:'before'` uses it.) One row, three records: the new tuple in the heap, the new entry in the primary key index, and the commit. 216 bytes in total. The WAL is a list of **physical** changes: "put these bytes at item 109 of this page", not "INSERT INTO ...".

The WAL lives in `pg_wal/` as 16 MB segment files. The file name encodes the LSN:

```console
tutor_storage=# SELECT pg_walfile_name(:'after');
     pg_walfile_name
--------------------------
 000000010000000000000019
(1 row)
```

### Why the WAL makes commits fast

Writing to the WAL is **sequential**: always at the end of one file. Writing table pages is **random**: page 500 of one table, page 12 of an index, page 934 of another. The WAL turns many random writes into one sequential write plus one `fsync`. Many concurrent commits share one `fsync` (group commit).

The `fsync` is still the expensive part. PostgreSQL ships a tool to measure it on your disk:

```console
$ docker exec -w /tmp tutor-pg /usr/lib/postgresql/18/bin/pg_test_fsync -s 1 -f /tmp/tutor-fsync-test
1 second per test
O_DIRECT supported on this platform for open_datasync and open_sync.

Compare file sync methods using one 8kB write:
(in "wal_sync_method" preference order, except fdatasync is Linux's default)
        open_datasync                       776.998 ops/sec    1287 usecs/op
        fdatasync                           774.766 ops/sec    1291 usecs/op
        fsync                               650.377 ops/sec    1538 usecs/op
        fsync_writethrough                              n/a
        open_sync                           622.214 ops/sec    1607 usecs/op
...
Non-sync'ed 8kB writes:
        write                            662168.606 ops/sec       2 usecs/op
```

1.3 ms per `fdatasync`, 2 µs per plain `write`. A factor of 600. So one client committing one row at a time can do at most about 770 commits per second on this laptop. Check it with `pgbench`, which also ships with PostgreSQL (`-i` creates its test tables, `-N` runs a simple insert-and-update transaction, `-T 5` for 5 seconds):

```console
$ docker exec tutor-pg pgbench -U postgres -i -q -s 1 tutor_storage
...
$ docker exec tutor-pg pgbench -U postgres -c 1 -T 5 -N tutor_storage
...
latency average = 1.955 ms
tps = 511.458306 (without initial connection time)
$ docker exec -e PGOPTIONS='-c synchronous_commit=off' tutor-pg pgbench -U postgres -c 1 -T 5 -N tutor_storage
...
latency average = 0.552 ms
tps = 1812.095206 (without initial connection time)
```

`synchronous_commit=off` makes `COMMIT` return **before** the `fsync`. Three times faster, and a crash can lose the last few hundred milliseconds of commits that the client was told had succeeded. The database stays consistent; it just "forgets" those transactions. That can be fine for a click counter, never for a payment. You can set it per transaction (`SET LOCAL synchronous_commit = off`), so you don't need to choose for the whole server.

!!! warning "fsync must tell the truth"

    All of this assumes that when `fsync` returns, the data survives a power cut. Some disks and
    virtual disks lie: they report success while the data is still in a volatile cache. Then
    the WAL protects you from nothing. Cloud block storage and enterprise SSDs with power-loss
    protection are fine. `fsync = off` in `postgresql.conf` is never fine, outside of a test that
    you are happy to throw away. See also the "fsyncgate" note on [Filesystems](../linux/filesystems.md#under-the-hood-fsync-and-the-atomic-rename).

### Checkpoints

If dirty pages stay in memory, the WAL grows forever, and crash recovery has to replay all of it. A **checkpoint** writes all dirty pages to the table files, `fsync`s them, and then records "everything before this LSN is in the files". Recovery starts from there. WAL segments older than that point can be recycled.

A checkpoint runs every `checkpoint_timeout` (5 min by default) or when `max_wal_size` (1 GB) of WAL has been written since the last one, whichever is first. `checkpoint_completion_target = 0.9` spreads the writes over 90% of the interval, so the disk is not flooded. `pg_controldata` shows the last one:

```console
$ docker exec tutor-pg /usr/lib/postgresql/18/bin/pg_controldata /var/lib/postgresql/18/docker | grep -E 'checkpoint location|REDO location|state|checksum'
Database cluster state:               in production
Latest checkpoint location:           0/1C5BC158
Latest checkpoint's REDO location:    0/1C5BC100
Data page checksum version:           1
```

`pg_stat_checkpointer` counts them. `num_requested` growing fast in production means `max_wal_size` is too small for your write rate: checkpoints are forced early, and each one causes the cost you see next.

### Full-page writes: the 30 kB status change

The OS writes in 4 kB units, PostgreSQL in 8 kB pages. If the power fails in the middle of writing a page, the file can hold half an old page and half a new one: a **torn page**. A WAL record that says "change byte 300 of page 500" can't repair a page that is half garbage.

So, the **first time** a page is changed after a checkpoint, PostgreSQL puts a full copy of the page into the WAL (`full_page_writes = on`). Recovery restores that copy first, then applies later records on top. Watch it. Find a row on page 500, run a checkpoint, and change one column, twice:

```console
tutor_storage=# SELECT id AS oid FROM orders WHERE ctid = '(500,1)' \gset
tutor_storage=# CHECKPOINT;
CHECKPOINT
tutor_storage=# SELECT pg_current_wal_lsn() AS before \gset
tutor_storage=# UPDATE orders SET status = 'cancelled' WHERE id = :'oid';
UPDATE 1
tutor_storage=# SELECT pg_current_wal_lsn() AS after \gset
tutor_storage=# SELECT resource_manager AS rmgr, record_type, record_length AS len, fpi_length AS fpi
FROM pg_get_wal_records_info(:'before', :'after');
    rmgr     | record_type |  len  |  fpi
-------------+-------------+-------+-------
 Heap        | LOCK        |  8215 |  8156
 Heap        | UPDATE      | 14104 | 13964
 Btree       | INSERT_LEAF |  7457 |  7404
 Transaction | COMMIT      |    46 |     0
(4 rows)
```

Then the same thing again, right away:

```console
tutor_storage=# SELECT pg_current_wal_lsn() AS before \gset
tutor_storage=# UPDATE orders SET status = 'paid' WHERE id = :'oid';
UPDATE 1
tutor_storage=# SELECT pg_current_wal_lsn() AS after \gset
tutor_storage=# SELECT resource_manager AS rmgr, record_type, record_length AS len, fpi_length AS fpi
FROM pg_get_wal_records_info(:'before', :'after');
    rmgr     |   record_type   | len | fpi
-------------+-----------------+-----+-----
 Heap2       | PRUNE_ON_ACCESS |  56 |   0
 Heap        | HOT_UPDATE      | 114 |   0
 Transaction | COMMIT          |  46 |   0
(3 rows)
```

The first update wrote 29,904 bytes of WAL. The second wrote 216. Same row, same column. `pg_waldump` (the command-line WAL reader) shows which pages were copied, marked `FPW`:

```console
$ docker exec tutor-pg /usr/lib/postgresql/18/bin/pg_waldump -p /var/lib/postgresql/18/docker/pg_wal -s 0/1C5BC1D0 -e 0/1C5C36A0
rmgr: Heap        len (rec/tot):     59/  8215, tx:        801, lsn: 0/1C5BC1D0, prev 0/1C5BC158, desc: LOCK xmax: 801, off: 1, infobits: [LOCK_ONLY, EXCL_LOCK], flags: 0x00, blkref #0: rel 1663/16389/16464 blk 500 FPW
rmgr: Heap        len (rec/tot):    140/ 14104, tx:        801, lsn: 0/1C5BE200, prev 0/1C5BC1D0, desc: UPDATE old_xmax: 801, old_off: 1, old_infobits: [], flags: 0x11, new_xmax: 0, new_off: 69, blkref #0: rel 1663/16389/16464 blk 934 FPW, blkref #1: rel 1663/16389/16464 blk 500, blkref #3: rel 1663/16389/16464 fork vm blk 0 FPW
rmgr: Btree       len (rec/tot):     53/  7457, tx:        801, lsn: 0/1C5C1930, prev 0/1C5BE200, desc: INSERT_LEAF off: 259, blkref #0: rel 1663/16389/16476 blk 206 FPW
rmgr: Transaction len (rec/tot):     46/    46, tx:        801, lsn: 0/1C5C3670, prev 0/1C5C1930, desc: COMMIT 2026-11-26 22:20:43.102603 UTC
```

Read it as a story:

1. Page 500 was full (the bulk insert packed it). The new row version had to go to another page, 934. Before PostgreSQL lets go of page 500 to go and find room, it marks the old row as locked (`LOCK`, with a copy of page 500).
2. `UPDATE` writes the new version on page 934 (a copy of it, and of the visibility map page).
3. The row moved, so its `ctid` changed, so the primary key index needs a new entry pointing to `(934,70)` (`INSERT_LEAF`, with a copy of index page 206).
4. The second update found room on page 934 itself: a **HOT** update ("heap-only tuple") that needs no new index entry. And page 934 was already copied since the checkpoint. 216 bytes.

That is **write amplification**: the database writes far more bytes than you changed. More WAL means more disk, more network to replicas, and bigger backups. Things that make it worse: frequent checkpoints, many indexes, and updates that can't stay on the same page. [Indexes](indexes.md) shows how to make HOT updates more likely.

## B-trees and LSM trees

PostgreSQL, MySQL (InnoDB), SQL Server and Oracle all use the same basic design: pages, **updated in place** (or next to the old version, in PostgreSQL), found through **B-tree** indexes, protected by a WAL. A second family of storage engines turns this around. RocksDB, LevelDB, Cassandra, ScyllaDB and the storage under CockroachDB and TiKV use a **log-structured merge tree** (LSM tree).

```mermaid
flowchart TB
    subgraph BT ["B-tree engine (PostgreSQL)"]
        direction TB
        W1[write] --> WAL1[(WAL)]
        W1 --> P1["find the page,<br/>change it in memory"]
        P1 -->|checkpoint| F1[("table + index files<br/>pages changed in place")]
    end
    subgraph LSM ["LSM engine (RocksDB, Cassandra)"]
        direction TB
        W2[write] --> WAL2[(WAL)]
        W2 --> M["memtable (sorted, in RAM)"]
        M -->|full: flush| L0[("SSTable files<br/>sorted, never changed")]
        L0 -->|"compaction: merge,<br/>drop old versions"| L1[("bigger SSTables")]
    end
```

An LSM tree never changes a file once written:

1. A write goes into the WAL and into a sorted in-memory table (the **memtable**).
2. When the memtable is full, it is written out as one sorted, immutable file (an **SSTable**). That is one big sequential write.
3. A read must check the memtable and then several SSTables, newest first. **Bloom filters** (a small "definitely not here" test per file) skip most files.
4. In the background, **compaction** merges SSTables into bigger ones, keeping only the newest version of each key. A delete is a special marker (a **tombstone**) that disappears during compaction.

| Question            | B-tree (PostgreSQL)                                  | LSM tree (RocksDB, Cassandra)                                 |
| ------------------- | ---------------------------------------------------- | ------------------------------------------------------------- |
| Write pattern       | Random page writes (smoothed by WAL and checkpoints) | Sequential writes only                                        |
| Write amplification | Full-page writes, index updates, `VACUUM`            | Compaction rewrites the same data many times                  |
| Read of one key     | One path down one tree                               | Memtable + several files (Bloom filters help)                 |
| Read amplification  | Low                                                  | Higher, and it grows when compaction falls behind             |
| Space               | Dead row versions until `VACUUM`, half-empty pages   | Old versions and tombstones until compaction                  |
| Background work     | Checkpoints, `VACUUM`                                | Compaction (CPU and disk I/O that competes with your queries) |
| Good for            | Mixed reads and writes, transactions, ad-hoc queries | Very high write rates, time series, key-value at large scale  |

No design wins everywhere. You trade **write amplification** (bytes written per byte changed), **read amplification** (pages read per lookup) and **space amplification** (bytes on disk per byte of live data). Making one smaller usually makes another bigger. When a vendor says "10× faster writes", ask which of the three they paid with.

## Try it

1. In `tutor_storage`, create a table with a `bigint` and a `text` column, insert one row, and predict `lp_len` before you check it with `heap_page_items`.
2. Update that row five times. Look at `heap_page_items` again. How many tuples are on the page? Which ones have `t_xmax` set? Run `VACUUM` on the table and look once more.
3. Run `CHECKPOINT`, then an `UPDATE` of one row by primary key, and measure the WAL bytes with `pg_current_wal_lsn()` before and after. Do it again without the checkpoint. Explain the difference.
4. Run the `pgbench` command above with `-c 8` instead of `-c 1`. Does `tps` grow by 8×? Why is it more than one client can do, if every commit needs an `fsync`? (Hint: group commit.)

Clean up when you are done:

```sh
docker exec tutor-pg dropdb -U postgres tutor_storage
```

## Traps

- **"Without `ORDER BY` the rows come back in insert order."** They come back in the order they lie in the heap, and an `UPDATE` moves rows. Always write `ORDER BY` when order matters.
- **Using `ctid` as an ID.** It changes on every update that moves a row, and after `VACUUM FULL`. It is an address, not a key.
- **"We only update, so the table can't grow."** Every update writes a new row version. Without `VACUUM` keeping up, the table bloats.
- **`SELECT *` on wide tables.** Large values are in TOAST, and reading them costs extra page reads per row.
- **Turning `fsync` off "for speed".** One crash and the cluster can be corrupt, not just behind. Use `synchronous_commit = off` per transaction when you can accept losing recent commits.
- **Huge `shared_buffers`.** PostgreSQL also relies on the kernel's page cache. The usual starting point is about 25% of RAM, not 80%.
- **Comparing query times on a warm laptop.** Warm caches hide I/O. Compare `BUFFERS` counts, not only milliseconds.
- **Too-small `max_wal_size`.** Checkpoints come too often, and every page changed after each checkpoint costs a full 8 kB copy in the WAL.

## Check yourself

??? question "1. A page header says `lower = 52` and `upper = 7680`. How many line pointers does the page have, and how much free space?"

    `(52 - 24) / 4 = 7` line pointers. Free space is the gap: `7680 - 52 = 7628` bytes.

??? question "2. You update one column of one row. Name three reasons why the database may write much more than the size of that column."

    PostgreSQL writes a whole new row version, not one column. If the page has no room, the new
    version goes to another page and every index needs a new entry. And the first change to each
    page after a checkpoint puts a full 8 kB copy of the page in the WAL (full-page writes).

??? question "3. `COMMIT` returned. Which parts of the change are guaranteed to be on disk, and which are not?"

    The WAL records up to and including the commit record are on disk (`fsync`ed), with the default
    `synchronous_commit = on`. The changed table and index pages are probably still only in shared
    buffers. That is fine: after a crash, recovery replays the WAL from the last checkpoint.

??? question "4. `EXPLAIN (ANALYZE, BUFFERS)` shows `shared read=935`, yet the query took only 10 ms. Did it go to the disk?"

    Not necessarily. "read" means PostgreSQL did not find the page in shared buffers and asked the
    kernel with `read()`. The kernel's page cache may have had it in RAM. You only know it hit the
    disk if you also see the I/O there (for example with `track_io_timing` or `iostat`).

??? question "5. Your team wants to store 200,000 sensor readings per second, almost never update them, and read them by time range. B-tree or LSM, and what do you pay?"

    That is the classic LSM use case: append-heavy writes become sequential flushes. You pay with
    background compaction (CPU and I/O, which can hurt latency when it falls behind) and with reads
    that may check several files. PostgreSQL can also do it with partitioning and BRIN indexes
    (see [Indexes](indexes.md) and [Replication and scaling](replication.md)), but at that rate the
    WAL and index maintenance become the limit.

## Recap

- PostgreSQL stores a table as a **heap file** of 8 kB **pages**. Each page has a header, **line pointers** growing down, and **tuples** growing up from the end.
- **`ctid`** = (page, item) is a row version's physical address. Indexes point to it.
- An **UPDATE writes a new tuple** and marks the old one with `xmax`. The space comes back only after `VACUUM`.
- Values bigger than about 2 kB are compressed or moved to the **TOAST** table.
- Pages are read into **shared buffers**. Below that, the kernel's page cache caches them again.
- Every change goes to the **WAL** first. `COMMIT` waits for the WAL `fsync`, not for the table pages. That `fsync` (1.3 ms here) limits single-client commit speed. `synchronous_commit = off` trades recent commits for speed.
- **Checkpoints** write dirty pages and let old WAL go. The first change to a page after a checkpoint writes a **full-page image** to the WAL.
- **B-trees** update pages in place and read cheaply. **LSM trees** write sequentially and pay with compaction and more expensive reads. Every engine trades write, read and space amplification.

Next: the B-tree in detail, and how to make the database find one row among millions. [Indexes](indexes.md).
