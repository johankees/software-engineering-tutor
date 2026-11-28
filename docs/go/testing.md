# Testing

## Why care?

Here is a common failure. A developer changes how order totals are rounded. All 600 tests pass. A few days later, finance reports that every invoice since the change is wrong by a few cents.

You look at the tests. The service tests use a mocking library: they check that `repo.Create` was "called once with any order". None of them checks the **total**. They test _which functions were called_, not _what the code does_. They passed because they could not fail.

Good tests let you change code safely. Go has strong opinions about what "good" means, and a complete toolkit built into `go test`: unit tests, benchmarks, fuzzing, coverage, race detection, fake clocks and in-memory HTTP servers. No framework needed.

All output on this page comes from a small version of the `orders` project, run with Go 1.27.

## The idea: tests are plain Go code

A test in Go is:

- a function named `TestXxx(t *testing.T)`,
- in a file ending in `_test.go`,
- run by `go test`.

```go
func TestOrderTotal(t *testing.T) {
	o := order.Order{Items: []order.Item{{SKU: "PEN-3", Quantity: 10, PriceCents: 150}}}
	if got, want := o.Total(), int64(1500); got != want {
		t.Errorf("Total() = %d, want %d", got, want)
	}
}
```

There is no `assertEquals`, no `expect(...).toBe(...)`. Just an `if` and a message. That is on purpose.

!!! abstract "Convention: plain `if` checks with \"got, want\" messages"

    **Rule:** compare with normal Go code, and write failure messages in the form `Func(args) = got, want want`.

    **Why:** a failing test is read by someone who did **not** write it, maybe a year later, maybe in a CI log.
    `Total() = 150, want 1500` tells them exactly what was called, what came out, and what should have come out,
    without opening the test file. Plain Go also means no second language to learn: tests use the same
    `if`, loops and helpers as the code. (Many teams do use `testify`'s `assert`/`require`; it is popular and fine.
    The standard library and the Go team don't, and the "got, want" message habit matters more than the tool.)

### `t.Error` or `t.Fatal`?

- `t.Errorf` marks the test as failed and **continues**. Use it when the next checks still make sense: you see all problems in one run.
- `t.Fatalf` marks it failed and **stops** this test right now. Use it when continuing makes no sense: `order, err := ...; if err != nil { t.Fatalf(...) }` — there is no order to check.

### Where tests live

Tests sit **next to the code** they test: `order.go` and `order_test.go` in the same folder. You choose the package name of the test file:

| Test file says       | Kind           | Can see                     | Use for                                        |
| -------------------- | -------------- | --------------------------- | ---------------------------------------------- |
| `package order`      | white-box test | everything, also unexported | testing tricky internal helpers                |
| `package order_test` | black-box test | only the exported API       | most tests: use the package like a caller does |

!!! abstract "Convention: prefer `package xxx_test`"

    **Rule:** write tests in the external `_test` package by default.

    **Why:** the test then uses your package exactly like real callers. If the API is awkward to test, it is awkward
    to use, and you find out early. Tests that only use the public API also survive refactoring: you can rename
    every unexported function without touching a test.

## Table-driven tests

This is the most recognisable pattern in Go testing. You describe the **cases as data**, then write the checking logic **once**:

```go
func TestOrderTotal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		items []order.Item
		want  int64
	}{
		{name: "no items", items: nil, want: 0},
		{name: "one item", items: []order.Item{{SKU: "BOOK-1", Quantity: 1, PriceCents: 1999}}, want: 1999},
		{name: "quantity multiplies", items: []order.Item{{SKU: "PEN-3", Quantity: 10, PriceCents: 150}}, want: 1500},
		{
			name: "several items",
			items: []order.Item{
				{SKU: "BOOK-1", Quantity: 2, PriceCents: 1999},
				{SKU: "PEN-3", Quantity: 3, PriceCents: 150},
			},
			want: 4448,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := order.Order{Items: tc.items}.Total()
			if got != tc.want {
				t.Errorf("Total() = %d, want %d", got, tc.want)
			}
		})
	}
}
```

Now break `Total` on purpose (forget to multiply by `Quantity`) and run `go test`:

```text
--- FAIL: TestOrderTotal (0.00s)
    --- FAIL: TestOrderTotal/several_items (0.00s)
        order_test.go:36: Total() = 2149, want 4448
    --- FAIL: TestOrderTotal/quantity_multiplies (0.00s)
        order_test.go:36: Total() = 150, want 1500
```

You see **which cases** fail and which pass, and the message says what is wrong.

!!! abstract "Convention: table-driven tests with `t.Run`"

    **Rule:** when you test one function with several inputs, use a slice of named cases and one loop with `t.Run`.

    **Why:**

    - **Adding a case is one line.** So people actually add the edge case they just found in production.
    - **The interesting part is visible.** The table _is_ the specification: you read the inputs and outputs side by
      side, without the noise of setup code repeated 10 times.
    - **Each case is its own subtest**, with its own name in the output. You can run one: `go test -run 'TestOrderTotal/quantity'`.
    - **The checking logic exists once**, so it is the same for every case and can't drift.

### `t.Parallel()`

`t.Parallel()` lets tests (and subtests) run at the same time. It makes the suite faster, and, just as valuable, it **finds hidden shared state**: if two tests break when run together, they share something they shouldn't (a global, a fixed file name, a port).

Since Go 1.22 every loop iteration has its own `tc` variable, so the old `tc := tc` line is no longer needed. The `forvar` fixer in `go fix` removes it from old code.

!!! warning "Parallel subtests must not share mutable data"

    In a validation table, each case changes a "valid order" a bit. If cases share the `Items` slice, parallel
    cases overwrite each other's data (slices share their backing array, see [Types and data](types-and-data.md)).
    Copy first: `o.Items = append([]order.Item(nil), valid.Items...)`.

### Useful helpers on `t`

| Helper                   | What it does                                                                          |
| ------------------------ | ------------------------------------------------------------------------------------- |
| `t.Context()` (1.24)     | a context that is cancelled when the test ends. Use instead of `context.Background()` |
| `t.Cleanup(f)`           | run `f` after the test (and its subtests). Like `defer`, but works from helpers       |
| `t.TempDir()`            | a fresh directory, deleted after the test                                             |
| `t.Setenv(k, v)`         | set an env variable for this test, restored afterwards (not with `t.Parallel`)        |
| `t.Chdir(dir)` (1.24)    | change the working directory for this test                                            |
| `t.Helper()`             | mark a helper function, so failures point at the caller's line                        |
| `t.ArtifactDir()` (1.26) | a directory for output files worth keeping (see below)                                |

`t.ArtifactDir()` is handy in CI: write the failing HTTP response, a screenshot or a diff there. Normally it is a temp directory that is deleted. With `go test -artifacts -outputdir out` (the `out` folder must exist), the files are kept:

```text
out/_artifacts/TestArtifact/1309881514/response.json
```

## Test doubles: fakes, not mocks

Your `order.Service` needs a `Repository`. In a unit test you don't want a real PostgreSQL. So you give it a **test double**: something that stands in for the real thing. There are several kinds, and Go culture strongly prefers one of them.

| Kind     | What it is                                                         | Checks                          |
| -------- | ------------------------------------------------------------------ | ------------------------------- |
| **Stub** | returns fixed answers                                              | nothing                         |
| **Fake** | a real, simple, working implementation (e.g. a map instead of SQL) | the **result**: state after     |
| **Mock** | records calls and verifies "was `Create` called once with X?"      | the **interaction**: calls made |

Here is a fake repository. It is about 40 lines and works like a tiny database:

```go
// fakeRepo is a working, in-memory Repository for tests.
type fakeRepo struct {
	mu        sync.Mutex
	orders    map[uuid.UUID]order.Order
	createErr error // if set, Create fails with it
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{orders: make(map[uuid.UUID]order.Order)}
}

func (r *fakeRepo) Get(_ context.Context, id uuid.UUID) (order.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[id]
	if !ok {
		return order.Order{}, order.ErrNotFound
	}
	return o, nil
}

func (r *fakeRepo) Create(_ context.Context, o order.Order) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return r.createErr
	}
	r.orders[o.ID] = o
	return nil
}

// List omitted here; it filters by customer and sorts by CreatedAt.
```

And a test that uses it:

```go
var fixedNow = func() time.Time { return time.Date(2026, 11, 26, 9, 30, 0, 0, time.UTC) }

func TestServicePlace(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	svc := order.NewService(repo, fixedNow)

	placed, err := svc.Place(t.Context(), "cus-1", []order.Item{{SKU: "BOOK-1", Quantity: 1, PriceCents: 1999}})
	if err != nil {
		t.Fatalf("Place() error = %v", err)
	}

	// Check the outcome (state), not which methods were called.
	got, err := repo.Get(t.Context(), placed.ID)
	if err != nil {
		t.Fatalf("order was not stored: %v", err)
	}
	if got.Status != order.StatusPending {
		t.Errorf("Status = %q, want %q", got.Status, order.StatusPending)
	}
	if !got.CreatedAt.Equal(fixedNow()) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, fixedNow())
	}
}
```

Notice `now func() time.Time` in the service. Time is a dependency like the database. Inject it, and tests become exact.

!!! abstract "Convention: prefer fakes over mocks"

    **Rule:** test against small, working fake implementations, and check the resulting **state**. Use mocks (call
    expectations) only when the interaction itself is the behaviour, e.g. "we must send exactly one email".

    **Why:**

    - **Mocks test the implementation, fakes test the behaviour.** A mock test says "Place calls Create once". If
      you refactor Place to call `CreateBatch`, the behaviour is the same but every mock test breaks. A fake test
      only cares that the order is stored. That is the problem in the example at the top of this page.
    - **Fakes are reusable.** One `fakeRepo` serves hundreds of tests. Mock setups are copied and tweaked in every test.
    - **Go makes fakes cheap.** Interfaces are implicit and small (see [Methods and interfaces](methods-and-interfaces.md)),
      so a fake is a plain struct with a few methods. No generator, no framework.
    - **Fakes can be tested too.** Run the same contract test suite against the fake and the real PostgreSQL
      repository. Then you _know_ the fake behaves like the real thing.

!!! tip "Small interfaces make fakes easy"

    If your fake needs 25 methods, the interface is too big. Define the interface **where it is used**, with only
    the methods that code needs. A `Notifier` with one `Notify` method is a 5-line fake.

## Testing HTTP: `httptest`

Two cases come up in every service.

**Testing a handler** (your server side): use `httptest.NewRecorder()` as the `ResponseWriter` and call `ServeHTTP` directly. No network at all. You saw this in [Errors](errors.md#a-whole-service-error-strategy).

**Testing a client** (code that calls another service): start a real HTTP server inside the test. Since Go 1.27, `httptest.NewTestServer` runs it on an **in-memory network**: no port, no "address already in use", and it shuts down by itself when the test ends.

```go
func TestClientGet(t *testing.T) {
	t.Parallel()
	known := uuid.MustParse("0195d0f8-5f6b-7c2a-9d3e-1a2b3c4d5e6f")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != known.String() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.MarshalWrite(w, order.Order{ID: known, Status: order.StatusPaid}); err != nil {
			t.Errorf("encode: %v", err)
		}
	})
	srv := httptest.NewTestServer(t, mux) // in-memory network, closed automatically

	base, err := url.Parse("http://orders.internal")
	if err != nil {
		t.Fatal(err)
	}
	c := client.New(base, srv.Client())

	t.Run("found", func(t *testing.T) {
		got, err := c.Get(t.Context(), known)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got.Status != order.StatusPaid {
			t.Errorf("Status = %q, want %q", got.Status, order.StatusPaid)
		}
	})
	t.Run("not found", func(t *testing.T) {
		_, err := c.Get(t.Context(), uuid.NewV7())
		if !errors.Is(err, order.ErrNotFound) {
			t.Errorf("Get() error = %v, want ErrNotFound", err)
		}
	})
}
```

```text
--- PASS: TestClientGet (0.00s)
    --- PASS: TestClientGet/found (0.00s)
    --- PASS: TestClientGet/not_found (0.00s)
```

Two things make this work:

- `srv.Client()` returns an `*http.Client` wired to the in-memory network. **Every** request it sends goes to the test server, whatever the host name. So the client under test can use its real base URL.
- The client takes its `*http.Client` as a **parameter** (`client.New(base, hc)`). That is dependency injection, and it is what makes the code testable. A client that uses `http.DefaultClient` inside can't be tested this way.

`NewTestServer` also fails the test if the handler panics. (The older `httptest.NewServer` listens on a real loopback port and needs `defer srv.Close()`. You'll see it in a lot of existing code.)

## Golden files: testing big outputs

Some outputs are too big for a `want` string in the code: a rendered report, generated code, a long JSON document. Store the expected output in a file, `testdata/<TestName>.golden`, and compare:

```go
var update = flag.Bool("update", false, "rewrite golden files in testdata/")

func TestDaily(t *testing.T) {
	t.Parallel()
	orders := []order.Order{
		{CustomerID: "cus-1", Status: order.StatusPaid, Items: []order.Item{{SKU: "BOOK-1", Quantity: 2, PriceCents: 1999}}},
		{CustomerID: "cus-22", Status: order.StatusPending, Items: []order.Item{{SKU: "PEN-3", Quantity: 10, PriceCents: 150}}},
	}

	var buf bytes.Buffer
	if err := report.Daily(&buf, orders); err != nil {
		t.Fatal(err)
	}

	golden := filepath.Join("testdata", t.Name()+".golden")
	if *update {
		if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden file (run with -update to create it): %v", err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("Daily() output differs from %s\ngot:\n%s\nwant:\n%s", golden, buf.Bytes(), want)
	}
}
```

The first run fails, with a helpful message:

```text
--- FAIL: TestDaily (0.00s)
    report_test.go:36: read golden file (run with -update to create it): open testdata/TestDaily.golden: no such file or directory
```

`go test ./internal/report/ -update` writes `testdata/TestDaily.golden`:

```text
  customer   status  items   total
     cus-1     paid      1  €39.98
    cus-22  pending      1  €15.00
                            €54.98
```

**You review that file** like code, and commit it. From now on, any change to the report shows up as a diff in code review: the reviewer sees exactly how the output changed.

!!! info "`testdata` is special"

    The `go` tool ignores directories named `testdata` (and names starting with `.` or `_`) when it looks for
    packages. Put fixtures, golden files and sample inputs there. Tests run with the package directory as working
    directory, so the relative path `testdata/...` always works.

## Examples: documentation that is tested

A function named `ExampleXxx` is both **documentation** (it shows up on pkg.go.dev and in `go doc`) and a **test**: `go test` runs it and compares what it prints with the `// Output:` comment.

```go
func ExampleOrder_Total() {
	o := order.Order{Items: []order.Item{
		{SKU: "BOOK-1", Quantity: 2, PriceCents: 1999},
		{SKU: "PEN-3", Quantity: 1, PriceCents: 150},
	}}
	fmt.Println(o.Total())
	// Output: 4148
}
```

When the code changes and the example goes stale, the test fails:

```text
--- FAIL: ExampleOrder_Total (0.00s)
got:
2149
want:
4148
```

So the documentation can't go out of date without a failing test. The name tells `go doc` where to attach it: `ExampleOrder_Total` belongs to method `Total` on type `Order`.

## Benchmarks

A benchmark is a function `BenchmarkXxx(b *testing.B)`. Since Go 1.24 the body is a `for b.Loop()` loop:

```go
func BenchmarkOrderTotal(b *testing.B) {
	o := order.Order{Items: make([]order.Item, 100)}
	for i := range o.Items {
		o.Items[i] = order.Item{SKU: "BOOK-1", Quantity: i, PriceCents: 1999}
	}
	for b.Loop() {
		o.Total()
	}
}

func BenchmarkOrderValidate(b *testing.B) {
	o := order.Order{CustomerID: "cus-1", Items: []order.Item{{SKU: "BOOK-1", Quantity: 0, PriceCents: 1999}}}
	b.ReportAllocs()
	for b.Loop() {
		_ = o.Validate()
	}
}
```

Run only benchmarks (`-run '^$'` skips the tests) and show memory:

```sh
go test ./internal/order/ -run '^$' -bench . -benchmem
```

```text
cpu: AMD Ryzen 7 5800H with Radeon Graphics
BenchmarkOrderTotal-16       	24579723	        49.40 ns/op	       0 B/op	       0 allocs/op
BenchmarkOrderValidate-16    	 2258193	       529.1 ns/op	     280 B/op	       9 allocs/op
```

Read it as: `Total` over 100 items takes about 49 ns and allocates nothing. `Validate` on an invalid order takes 529 ns and makes 9 heap allocations (the error values and their messages).

Why `b.Loop()` instead of the old `for i := 0; i < b.N; i++`?

- Setup **before** the loop is not timed. No more `b.ResetTimer()`.
- The compiler is not allowed to optimize away the call in the loop body, even when the result is unused. With `b.N` loops, benchmarks sometimes measured nothing at all.

The `-16` is `GOMAXPROCS`. One benchmark run is noisy. To compare two versions, run each with `-count=10` and compare with `benchstat` (see [Performance](performance.md)).

## Fuzzing: generated inputs find edge cases

You write tests for the inputs you **thought of**. Bugs are often in the inputs you didn't think of. A **fuzz test** generates millions of inputs, guided by code coverage, and checks a **property** that must always hold.

Here is a parser for SKUs like `BOOK-0042`. It has a bug that many experienced developers would not see in review:

```go
// ParseSKU parses "CATEGORY-NUMBER". The category is upper-cased.
func ParseSKU(s string) (SKU, error) {
	cat, num, ok := strings.Cut(s, "-")
	if !ok || cat == "" {
		return SKU{}, fmt.Errorf("parse sku %q: want CATEGORY-NUMBER", s)
	}
	for i := 0; i < len(cat); i++ {
		if !unicode.IsLetter(rune(cat[i])) { // BUG: cat[i] is a byte, not a character
			return SKU{}, fmt.Errorf("parse sku %q: category must be letters", s)
		}
	}
	n, err := strconv.Atoi(num)
	if err != nil || n < 0 {
		return SKU{}, fmt.Errorf("parse sku %q: number must be a non-negative integer", s)
	}
	return SKU{Category: strings.ToUpper(cat), Number: n}, nil
}
```

What property should always hold? **Round trip**: if a string parses, then formatting the SKU and parsing it again must give the same SKU.

```go
func FuzzParseSKU(f *testing.F) {
	// Seed corpus: normal and tricky inputs. These also run in every normal `go test`.
	for _, s := range []string{"BOOK-0042", "pen-7", "-1", "BOOK-", "BOOK--1", "ÄÖ-1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		sku, err := order.ParseSKU(s)
		if err != nil {
			return // rejecting input is fine; crashing or lying is not
		}
		// Property: formatting and parsing again gives the same SKU.
		again, err := order.ParseSKU(sku.String())
		if err != nil {
			t.Fatalf("ParseSKU(%q) ok, but ParseSKU(%q) failed: %v", s, sku.String(), err)
		}
		if again != sku {
			t.Fatalf("round trip changed the SKU: %+v -> %q -> %+v", sku, sku.String(), again)
		}
	})
}
```

Start fuzzing:

```sh
go test ./internal/order/ -run '^$' -fuzz FuzzParseSKU -fuzztime 2m
```

```text
fuzz: elapsed: 0s, gathering baseline coverage: 0/6 completed
fuzz: elapsed: 0s, gathering baseline coverage: 6/6 completed, now fuzzing with 16 workers
fuzz: minimizing 35-byte failing input file
fuzz: elapsed: 0s, minimizing
--- FAIL: FuzzParseSKU (0.20s)
    --- FAIL: FuzzParseSKU (0.00s)
        sku_test.go:22: ParseSKU("\xe0-0") ok, but ParseSKU("�-0000") failed: parse sku "�-0000": category must be letters

    Failing input written to testdata/fuzz/FuzzParseSKU/44f75adfbfdceb25
    To re-run:
    go test -run=FuzzParseSKU/44f75adfbfdceb25
```

The fuzzer needed **0.2 seconds** to find the input `"\xe0-0"`: one byte `0xE0` that is **not valid UTF-8**. The loop looks at bytes, and `rune(0xE0)` is `à`, a letter, so the parser accepts it. Then `strings.ToUpper` replaces the broken byte with `�` (U+FFFD), which is not a letter, and the second parse fails. Accepting garbage, then producing something you can't read back: in production, that is an order with a SKU nobody can look up.

This is exactly the bytes-vs-characters problem from [Encodings](../fundamentals/encodings.md). The fix is to range over the **string**, which decodes UTF-8 into runes (and turns invalid bytes into `U+FFFD`, which is not a letter):

```go
for _, r := range cat { // range over a string decodes UTF-8 runes
	if !unicode.IsLetter(r) {
		return SKU{}, fmt.Errorf("parse sku %q: category must be letters", s)
	}
}
```

Two details make fuzzing part of normal work, not a special project:

1. The failing input was **saved** to `testdata/fuzz/FuzzParseSKU/`. Commit it. From now on, plain `go test` runs it as a regression test, with no fuzzing engine.
2. The seeds from `f.Add` also run in every normal `go test`. So a fuzz test also works as a table test.

!!! tip "Good fuzz targets"

    Anything that parses untrusted input: request bodies, query parameters, file formats, IDs, money amounts.
    Good properties: "never panics", "round trip is stable", "output is always valid JSON", "fast path and slow
    path agree". Run fuzzing for a few minutes in a nightly CI job, not in every PR.

## The race detector

A **data race** is two goroutines touching the same memory at the same time, with at least one writing, and no synchronization (see [Concurrency](concurrency.md)). Races cause bugs that appear once a week under load and never on your laptop.

Go has a race detector built into the toolchain. Add `-race`:

```go
// Counter counts orders per status. BUG: not safe for concurrent use.
type Counter struct {
	byStatus map[string]int
}

func (c *Counter) Inc(status string) { c.byStatus[status]++ }
```

```go
func TestCounterConcurrent(t *testing.T) {
	t.Parallel()
	c := stats.NewCounter()
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { c.Inc("paid") })
	}
	wg.Wait()
	if got := c.Get("paid"); got != 2 {
		t.Errorf("Get(paid) = %d, want 2", got)
	}
}
```

Without `-race` the test **passes**: `ok github.com/acme/orders/internal/stats`. With it (paths shortened):

```text
==================
WARNING: DATA RACE
Read at 0x00c0000ea6c0 by goroutine 9:
  runtime.mapaccess2_faststr()
      .../internal/runtime/maps/runtime_faststr.go:120 +0x0

Previous write at 0x00c0000ea6c0 by goroutine 10:
  runtime.mapassign_faststr()
      .../internal/runtime/maps/runtime_faststr.go:261 +0x0
  github.com/acme/orders/internal/stats.(*Counter).Inc()
      .../internal/stats/stats.go:13 +0x8e
  github.com/acme/orders/internal/stats_test.TestCounterConcurrent.func1()
      .../internal/stats/stats_test.go:15 +0x12
...
--- FAIL: TestCounterConcurrent (0.00s)
    testing.go:1867: race detected during execution of test
FAIL
```

It shows both accesses with full stack traces. Fix: a `sync.Mutex` in `Counter`.

How it works: the compiler adds a check around **every** memory access, and the runtime tracks which goroutine touched what, and in which order. That makes the program about 2–20× slower and use 5–10× more memory. So you don't ship with it, but:

!!! abstract "Convention: always run tests with `-race` in CI"

    **Rule:** CI runs `go test -race ./...`.

    **Why:** the race detector has **no false positives**: every report is a real bug. But it only finds races in
    code that actually **runs** during the test. So it needs tests that exercise concurrency, and it needs to be on
    every time, not "when someone remembers". (It needs cgo, i.e. a C compiler, on most platforms; CI images have one.)

## Testing time: `testing/synctest`

The order service cancels unpaid orders after 15 minutes:

```go
// ExpireUnpaid cancels the order if it is still pending after ttl.
// It blocks until the order is cancelled, ctx is done, or isPaid reports true.
func ExpireUnpaid(ctx context.Context, ttl time.Duration, isPaid func() bool, cancel func()) {
	timer := time.NewTimer(ttl)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
		if !isPaid() {
			cancel()
		}
	}
}
```

How do you test that? A 15-minute test is useless. A test with `ttl = 10*time.Millisecond` and `time.Sleep(20*time.Millisecond)` is **flaky**: on a busy CI machine, the goroutine might not run in time.

`testing/synctest` (Go 1.25) solves this. Inside `synctest.Test`, goroutines run in a **bubble** with a **fake clock**. The clock only moves forward when every goroutine in the bubble is blocked. Then it jumps straight to the next timer.

```go
func TestExpireUnpaid(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var cancelled atomic.Bool
		go order.ExpireUnpaid(t.Context(), 15*time.Minute,
			func() bool { return false },     // never paid
			func() { cancelled.Store(true) }, // record the cancel
		)

		synctest.Sleep(14*time.Minute + 59*time.Second) // fake clock: returns at once
		if cancelled.Load() {
			t.Fatal("cancelled too early")
		}

		synctest.Sleep(time.Second)
		if !cancelled.Load() {
			t.Fatal("not cancelled after 15 minutes")
		}
	})
}
```

```text
--- PASS: TestExpireUnpaid (0.00s)
```

The test covers fifteen minutes of logic in **0.00 seconds**. It is deterministic and uses the real `time` package. No fake clock interface to inject.

`synctest.Sleep(d)` (Go 1.27) is `time.Sleep(d)` followed by `synctest.Wait()`: advance the clock, then wait until all other goroutines in the bubble are blocked again, so their work is done before you check. `t.Context()` inside the bubble belongs to the bubble too. Also note: `httptest.NewTestServer`'s in-memory network works inside a bubble, so you can test HTTP timeouts and retries the same way.

## Integration tests

Unit tests with fakes are fast. But at some point you must know that your SQL really works on PostgreSQL. That is an **integration test**. Two common ways to keep them apart from unit tests:

**1. A build tag.** The file is only compiled when you ask for it:

```go
//go:build integration

package postgres_test

func TestRepositoryCreateGet(t *testing.T) {
	dsn := os.Getenv("ORDERS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ORDERS_TEST_DATABASE_URL not set")
	}
	db := openTestDB(t, dsn) // helper: connects, runs migrations, t.Cleanup drops the schema
	repo := postgres.New(db)
	// ... same checks as the fake-repository contract tests
}
```

```sh
go test ./...                          # unit tests only: fast, no Docker
go test -tags integration -race ./...  # also integration tests: CI, with a database
```

**2. `testing.Short()`.** Run `go test -short` for the fast set; long tests call `if testing.Short() { t.Skip(...) }`.

In CI, the database usually comes from a service container, or from the `testcontainers-go` library, which starts a throwaway PostgreSQL in Docker from inside the test. (That is a third-party dependency; it is widely used and maintained.)

!!! abstract "Convention: one contract test suite for fake and real"

    **Rule:** write the repository tests once, as a function that takes an `order.Repository`, and run it against
    both the fake (unit) and PostgreSQL (integration).

    **Why:** this proves the fake behaves like the real store. Without it, fakes slowly drift from reality, and
    unit tests start to pass against behaviour production doesn't have.

## Coverage

```sh
go test -cover ./...
```

```text
ok  	github.com/acme/orders/internal/client	0.005s	coverage: 70.6% of statements
ok  	github.com/acme/orders/internal/order	0.005s	coverage: 97.5% of statements
ok  	github.com/acme/orders/internal/report	0.004s	coverage: 100.0% of statements
```

Per function, and as an HTML page with covered lines in green:

```sh
go test -coverprofile=cover.out ./internal/order/
go tool cover -func=cover.out
go tool cover -html=cover.out
```

```text
github.com/acme/orders/internal/order/expiry.go:10:	ExpireUnpaid	100.0%
github.com/acme/orders/internal/order/order.go:47:	Total		100.0%
github.com/acme/orders/internal/order/order.go:56:	Validate	100.0%
github.com/acme/orders/internal/order/service.go:24:	NewService	100.0%
github.com/acme/orders/internal/order/service.go:29:	Place		83.3%
github.com/acme/orders/internal/order/sku.go:17:	ParseSKU	100.0%
github.com/acme/orders/internal/order/sku.go:35:	String		100.0%
total:							(statements)	97.5%
```

Coverage tells you which code **no test runs**. It does not tell you that the tested code is **checked**. The mock-based suite from the top of this page probably had 90% coverage. Use coverage to find blind spots, not as a target. A hard "minimum 80%" rule in CI tends to produce tests that run code without checking it.

## Everyday `go test` flags

| Command                                   | What it does                                                         |
| ----------------------------------------- | -------------------------------------------------------------------- |
| `go test ./...`                           | all packages in the module                                           |
| `go test -run 'TestOrderTotal/several' .` | only matching tests (regex, `/` separates subtest levels)            |
| `go test -count=1 ./...`                  | ignore the test cache (results are cached when nothing changed)      |
| `go test -shuffle=on ./...`               | random test order: finds tests that depend on each other             |
| `go test -race ./...`                     | with the race detector                                               |
| `go test -v ./...`                        | verbose: every test name and `t.Log` output                          |
| `go test -failfast ./...`                 | stop after the first failure                                         |
| `go test -timeout 2m ./...`               | panic with all goroutine stacks if a test hangs (default 10 minutes) |

## Traps

!!! warning "Trap 1: testing the mock, not the code"

    A test where every dependency is a mock with expectations, and the only check is "the mocks were called",
    can't catch a wrong result. Ask of every test: _"which bug would make this fail?"_ If you can't name one, the
    test is noise.

!!! warning "Trap 2: `time.Sleep` to wait for goroutines"

    `go worker(); time.Sleep(100 * time.Millisecond); check()` is flaky on slow CI machines and slow on fast ones.
    Wait for a signal (channel, `WaitGroup`), or use `synctest`.

!!! warning "Trap 3: tests that depend on map order or the real clock"

    A test passes 9 times out of 10 because it prints map contents unsorted, or because it compares `time.Now()`
    values. Sort before comparing. Inject `now func() time.Time`, or use `synctest`.

!!! warning "Trap 4: `t.Fatal` from another goroutine"

    `t.Fatal` and `t.FailNow` must be called from the test's own goroutine. From a goroutine you started, use
    `t.Error` (safe) and return. `go vet`'s `testinggoroutine` check catches the common case.

!!! warning "Trap 5: shared fixtures between parallel tests"

    A package-level `var testOrder = order.Order{...}` that tests modify. With `t.Parallel()` they corrupt each other.
    Build fixtures inside each test, or with a helper function that returns a fresh value.

## Check yourself

??? question "1. Why does Go prefer `Total() = 150, want 1500` over `assert.Equal(t, 1500, total)`?"

    The message must be useful to someone who did not write the test. "got, want" with the call shows what was
    called, the actual and the expected value. Plain `if` also needs no second "assertion language". (Assertion
    libraries can produce good messages too; the habit matters more than the tool.)

??? question "2. You refactor `Service.Place` to call `repo.CreateBatch` instead of `repo.Create`. Behaviour is the same. 40 tests fail. What does that tell you about those tests?"

    They are mock-based interaction tests: they test _how_ the code works, not _what_ it does. Fake-based tests
    that check the stored state would still pass.

??? question "3. A fuzz test fails and writes a file to `testdata/fuzz/`. What do you do with that file?"

    Fix the bug, then commit the file. Plain `go test` runs every file in that folder as a regression case.

??? question "4. Your test passes, and also passes with `-race`. Does that prove the code has no data races?"

    No. The race detector only sees races in code paths that ran during the test, with the timing that happened.
    It has no false positives, but it can have false negatives. Tests must actually exercise the concurrent paths.

??? question "5. How does `synctest` let a test cover a 15-minute timeout in zero seconds, without changing the code under test?"

    Goroutines inside the bubble use a fake clock. When every goroutine is blocked, the clock jumps to the next
    timer. The code still uses the normal `time` package.

## Recap

- Tests are plain Go: `TestXxx(t *testing.T)` in `_test.go` files. Messages say `Func(args) = got, want want`.
- Prefer black-box tests (`package xxx_test`). Use table-driven tests with `t.Run` and `t.Parallel`.
- Prefer **fakes** (small working implementations) over mocks. Check state, not calls. Small interfaces make fakes cheap.
- `httptest.NewRecorder` for handlers; `httptest.NewTestServer` (in-memory) for clients. Inject `*http.Client`.
- Golden files in `testdata/` with an `-update` flag. `ExampleXxx` functions are tested documentation.
- Benchmarks: `for b.Loop()`, `-benchmem`, compare with `benchstat`.
- Fuzzing finds inputs you didn't think of. Commit the failing inputs.
- Always `-race` in CI. Use `synctest` for anything with timers. Keep integration tests behind a build tag.
- Coverage finds blind spots; it does not prove quality.
