# Building a CLI

## Why care?

Your team writes a small tool to create orders from the terminal. It works on your laptop. Then:

- Someone puts it in a shell script: `orderctl list | grep pending`. The script breaks, because the tool prints a friendly "Fetching orders..." line into the same output as the data.
- The CI pipeline runs it, the server is slow, and the job hangs for six hours. The tool had no timeout.
- A script checks `if orderctl get $ID; then ...`. The tool exits with `0` even when the order does not exist.
- Nobody can test it, because all the logic is inside `main()`, which calls `os.Exit` in ten places.

None of these are hard problems. But each one makes a tool that people **cannot trust**. This page builds `orderctl`, the command-line client of the orders project, and explains every decision so your next CLI is right from the start.

All code on this page is real. It lives in `examples/orders/` in this repository, and CI builds, lints and tests it.

## What we are building

`orderctl` talks to the `ordersd` service (see [Building a service](services.md)) over HTTP:

```text
$ orderctl create -customer c-42 -item BOOK-1:2:1250 -item MUG:1:899
ID                                    CUSTOMER  STATUS   ITEMS  TOTAL  CREATED
01a216c6-d393-716c-832e-f6ca26fc5788  c-42      pending  2      33.99  2026-11-26 07:34:18

$ orderctl list
ID                                    CUSTOMER  STATUS   ITEMS  TOTAL  CREATED
01a216c6-d393-716c-832e-f6ca26fc5788  c-42      pending  2      33.99  2026-11-26 07:34:18
01a216c6-d398-75c5-aa97-31ff13d9a54d  c-7       pending  1      15.00  2026-11-26 07:34:18
```

The code is split like this:

```text
examples/orders/
├── cmd/orderctl/
│   ├── main.go        ← flags, commands, output, exit codes
│   └── main_test.go   ← runs the whole CLI in-process
└── internal/
    ├── client/        ← reusable Go client for the HTTP API
    ├── buildinfo/     ← "which version is this binary?"
    └── order/         ← domain types, shared with the service
```

!!! abstract "Convention: `cmd/<name>/` holds only the program's entry point"

    **Rule:** each binary gets a folder under `cmd/` with a small `package main`. Real logic lives in packages under `internal/`.

    **Why:** `package main` cannot be imported by anything. Code there can only be reused by copy-paste. When the HTTP client lives in `internal/client`, the CLI, tests and any future tool can all use it. See [Project layout](project-layout.md) for the full picture.

## The shape of a Go CLI: a tiny `main`, a testable `run`

Here is the complete `main` function of `orderctl`:

```go
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	// Check before stop(): stop also cancels ctx.
	if err != nil && ctx.Err() != nil {
		fmt.Fprintln(os.Stderr, "orderctl: interrupted")
		os.Exit(exitInterrupted)
	}
	stop()
	if code := exitCode(err); code != exitOK {
		if !alreadyReported(err) {
			fmt.Fprintln(os.Stderr, "orderctl:", err)
		}
		os.Exit(code)
	}
}
```

It does only three things:

1. Collects **everything from the outside world**: signals, arguments, environment, output streams.
2. Hands them to `run`.
3. Turns the result into an **exit code**.

All the real work happens in `run`:

```go
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) error {
```

!!! abstract "Convention: `main` calls `run(...) error`"

    **Rule:** keep `main` a few lines long. Put the program in a `run` function that receives its inputs as parameters and returns an `error`.

    **Why:**

    - **Testable.** A test can call `run` with fake arguments, a fake environment and a `strings.Builder` for output. No subprocesses, no real environment variables, and tests can run in parallel.
    - **One exit point.** `os.Exit` stops the program _immediately_. Deferred functions do **not** run: open files are not flushed, temp files are not removed. When only `main` calls `os.Exit`, every `defer` inside `run` is safe.
    - **No globals.** `os.Args` and `os.Getenv` are global state. Passing them in makes every dependency visible in one function signature.

### Under the hood: why `os.Exit` skips `defer`

The compiler arranges for deferred calls to run when the **function returns**. `os.Exit` never returns: it makes the `exit_group` system call (see [Processes, threads and system calls](../fundamentals/processes-and-threads.md)) and the kernel destroys the process on the spot. There is no "return" for the deferred calls to hook into.

The same is true for `log.Fatal`, which is just `log.Print` plus `os.Exit(1)`. That is why enterprise Go code avoids `log.Fatal` outside `main`.

## Flags with the standard library

Go ships a `flag` package. It is small, but enough for most tools. `orderctl` has **global flags** (shared by every command) and **subcommands** with their own flags:

```text
orderctl [global flags] <command> [command flags]
```

The global part of `run`:

```go
	var g globals
	fs := flag.NewFlagSet("orderctl", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usageText)
		fs.PrintDefaults()
	}
	fs.StringVar(&g.server, "server", cmp.Or(getenv("ORDERS_SERVER"), "http://localhost:8080"), "orders service `URL` (env ORDERS_SERVER)")
	fs.DurationVar(&g.timeout, "timeout", 10*time.Second, "timeout for the whole command")
	fs.StringVar(&g.output, "o", "table", "output `format`: table or json")
	if err := fs.Parse(args); err != nil {
		return wrapFlagErr(err)
	}
```

Line by line:

- **`flag.NewFlagSet` instead of the global `flag.String(...)`.** The package-level functions use one hidden global `flag.CommandLine`. A test that parses flags twice would crash with "flag redefined". A `FlagSet` is a normal value: every call to `run` makes a fresh one.
- **`flag.ContinueOnError`.** The global flag set uses `ExitOnError`: on a bad flag it calls `os.Exit(2)` _for you_, deep inside a library. With `ContinueOnError`, `Parse` returns an error and **we** decide what happens.
- **`fs.SetOutput(stderr)`.** Usage text goes to the writer we were given. In tests, that is a buffer we can check.
- **Backticks in the help text.** In ``"orders service `URL` (env ORDERS_SERVER)"`` the word between backticks becomes the placeholder name, so `PrintDefaults` shows `-server URL` instead of the generic `-server string`.
- **`cmp.Or(a, b)`** returns the first value that is not the zero value. Here: the environment variable if set, else the default. It replaces an `if` statement.
- **`time.Duration` flags.** `fs.DurationVar` parses `10s`, `1m30s`, `250ms`. Never take a timeout as a bare integer: is `-timeout 10` seconds or milliseconds?

The result, from a real run:

```text
$ orderctl -h
Usage: orderctl [global flags] <command> [command flags]

Commands:
  create   create an order: create -customer ID -item SKU:QTY:PRICE_CENTS [-item ...]
  get      show one order: get ORDER_ID
  list     list all orders
  version  print version information

Global flags:
  -o format
    	output format: table or json (default "table")
  -server URL
    	orders service URL (env ORDERS_SERVER) (default "http://localhost:8080")
  -timeout duration
    	timeout for the whole command (default 10s)
```

### Subcommands are more flag sets

After the global flags, `fs.Arg(0)` is the command name and the rest belongs to it:

```go
	cmd, cmdArgs := fs.Arg(0), fs.Args()[1:]
	// ...
	switch cmd {
	case "create":
		return runCreate(ctx, c, out, cmdArgs, stderr)
	case "get":
		return runGet(ctx, c, out, cmdArgs)
	case "list":
		return runList(ctx, c, out, cmdArgs)
	default:
		return usagef("unknown command %q (run orderctl -h)", cmd)
	}
```

`runCreate` makes its own `FlagSet` for `-customer` and `-item`. Each command is a plain function with plain parameters. No framework, no registration, no reflection.

!!! abstract "Convention: start with `flag`; reach for a framework only when you need it"

    **Rule:** use the standard `flag` package until you really need more. Popular frameworks like `cobra` are fine for big tools with dozens of nested commands, shell completion and generated docs.

    **Why:** every dependency is code you must update, audit for security, and teach to new people. "A little copying is better than a little dependency" is a Go proverb. `flag` + a `switch` handles a tool like `orderctl` in a few dozen lines that anyone can read in five minutes.

!!! warning "Trap: Go's `flag` stops at the first non-flag argument"

    `orderctl list -o json` does **not** set `-o`. Parsing stops at `list`; `-o json` becomes arguments of `list`, which then fails with "list: takes no arguments". Global flags must come **before** the command: `orderctl -o json list`. This is different from GNU tools, where flags can appear anywhere. Say it clearly in your usage text.

### Custom flag types: a repeatable `-item`

An order has many lines, so `-item` may be given many times. Any type with `String()` and `Set(string) error` methods satisfies the `flag.Value` interface:

```go
// itemList is a repeatable -item flag. Any type with String and Set
// methods satisfies flag.Value.
type itemList []order.Item

func (l *itemList) String() string { return fmt.Sprint(len(*l), " items") }

func (l *itemList) Set(s string) error {
	sku, rest, ok := strings.Cut(s, ":")
	qty, price, ok2 := strings.Cut(rest, ":")
	if !ok || !ok2 {
		return fmt.Errorf("want SKU:QTY:PRICE_CENTS, got %q", s)
	}
	q, err := strconv.Atoi(qty)
	if err != nil {
		return fmt.Errorf("quantity %q: not a whole number", qty)
	}
	p, err := strconv.ParseInt(price, 10, 64)
	if err != nil {
		return fmt.Errorf("price %q: not a whole number of cents", price)
	}
	*l = append(*l, order.Item{SKU: sku, Quantity: q, PriceCents: p})
	return nil
}
```

The flag package calls `Set` once per `-item`. Each call appends. This is [implicit interface satisfaction](methods-and-interfaces.md) at work: `itemList` never says "implements flag.Value", it just has the right methods.

Notice the CLI does **not** check "quantity must be at least 1". It only checks that the text has the right _shape_. The business rule lives in one place, the `order` package, and the server enforces it. If the CLI also checked it, the two copies would one day disagree.

!!! info "Why prices are in cents"

    `1250` means 12.50. Money is always an integer count of the smallest unit. A `float64` cannot store 0.10 exactly (see [Bits, bytes and numbers](../fundamentals/bits-and-bytes.md)), and tiny rounding errors in money become real bugs in invoices.

## Configuration: flags, then environment, then defaults

`orderctl` finds its server in three places, strongest first:

1. The `-server` flag.
2. The `ORDERS_SERVER` environment variable.
3. The built-in default `http://localhost:8080`.

The mechanism is simple: the environment value becomes the flag's **default**. If the user passes `-server`, it overwrites that default. The service uses the same rule with more settings; the [service page](services.md) shows its `config` package.

!!! abstract "Convention: config precedence is flag > env > file > default"

    **Rule:** the more _specific_ and _temporary_ a source is, the stronger it is.

    **Why:** a container image or CI job sets environment variables once, for everyone. A human debugging a problem types a flag once, for one run. The person typing must always win, or they cannot override anything without editing the environment.

## stdout is for data, stderr is for people

Look at where `orderctl` writes things:

- Results (tables, JSON) go to **stdout** through the `printer` type.
- Error messages and usage text go to **stderr**.

That is what makes this work:

```sh
orderctl -o json list | jq '.[] | select(.status == "pending") | .id'
```

If an error message went to stdout, `jq` would receive `orderctl: connection refused` and fail with a confusing parse error. With stderr, the human sees the message in the terminal and `jq` gets nothing.

!!! abstract "Convention: data on stdout, diagnostics on stderr"

    **Rule:** only the program's _result_ goes to stdout. Progress, warnings and errors go to stderr.

    **Why:** Unix pipes connect stdout of one program to stdin of the next. stderr is not piped, so it still reaches the human. Mixing them breaks every script that uses your tool.

### Two output formats, one of them a contract

People want a table. Scripts want JSON. `orderctl` supports both with `-o`:

```text
$ orderctl -o json list
[
  {
    "id": "01a216c6-d393-716c-832e-f6ca26fc5788",
    "customer_id": "c-42",
    "status": "pending",
    "item_count": 2,
    "total_cents": 3399,
    "created_at": "2026-11-26T07:34:18.771091084Z"
  },
  {
    "id": "01a216c6-d398-75c5-aa97-31ff13d9a54d",
    "customer_id": "c-7",
    "status": "pending",
    "item_count": 1,
    "total_cents": 1500,
    "created_at": "2026-11-26T07:34:18.776362825Z"
  }
]
```

The JSON comes from its own struct, not from the domain type:

```go
// orderOutput is the CLI's JSON output contract. Scripts parse it, so it is
// defined here and changes only on purpose.
type orderOutput struct {
	ID         string    `json:"id"`
	CustomerID string    `json:"customer_id"`
	Status     string    `json:"status"`
	Items      int       `json:"item_count"`
	TotalCents int64     `json:"total_cents"`
	CreatedAt  time.Time `json:"created_at"`
}
```

Somebody's script depends on `total_cents`. If the CLI printed `order.Order` directly, renaming a field in the domain would silently break that script. A separate struct makes the contract **visible**: if you change this struct, you know you are changing a public API.

The JSON is written with `encoding/json/v2` (the new JSON package, stable since Go 1.27; see [Standard library essentials](stdlib-essentials.md)) and pretty-printed with an option:

```go
		b, err := json.Marshal(out, jsontext.WithIndent("  "))
```

The table uses `text/tabwriter`, which lines up columns separated by `\t`:

```go
	tw := tabwriter.NewWriter(p.w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tCUSTOMER\tSTATUS\tITEMS\tTOTAL\tCREATED")
	for _, o := range orders {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n",
			o.ID, o.CustomerID, o.Status, len(o.Items), formatCents(o.TotalCents()),
			o.CreatedAt.Format(time.DateTime))
	}
	return tw.Flush()
```

!!! warning "Trap: forgetting `Flush`"

    `tabwriter` must see _all_ rows before it knows how wide each column is, so it buffers everything. Without `tw.Flush()` nothing is printed. The same is true for `bufio.Writer`. Always return the `Flush` error: it is where write errors (like a closed pipe) finally appear.

## Exit codes are part of your API

Scripts do not read your error messages. They read the **exit code**. `orderctl` uses four:

```go
// Exit codes. Scripts depend on them, so they are part of the CLI's API.
const (
	exitOK    = 0
	exitError = 1 // the command ran and failed
	exitUsage = 2 // the command line was wrong (same code the flag package uses)
	// exitInterrupted is 128 + SIGINT(2), what shells report for Ctrl-C.
	exitInterrupted = 130
)
```

The difference between `1` and `2` matters. Exit `2` says "**you** called me wrong, retrying will not help". Exit `1` says "I tried and it failed; maybe retry later".

How does `main` know which one? Errors carry the information. Command-line mistakes are wrapped in a small type:

```go
// usageError marks mistakes in the command line, so main can exit with 2.
// reported is true when the flag package already printed the problem.
type usageError struct {
	err      error
	reported bool
}

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

func exitCode(err error) int {
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	if _, ok := errors.AsType[usageError](err); ok {
		return exitUsage
	}
	return exitError
}
```

`errors.AsType` (Go 1.26) walks the chain of wrapped errors and finds a `usageError` anywhere in it. So `runGet` can return `usagef("get: %q is not a valid order ID", ...)` and `main` still exits with `2`. The [Errors](errors.md) page explains wrapping in depth.

The `reported` flag solves a small problem: the `flag` package already prints "flag provided but not defined: -verbose" before returning the error. Without `reported`, the user would see the same line twice.

Real runs:

```text
$ orderctl get 019ac0de-0000-7000-8000-000000000001; echo "exit=$?"
orderctl: get order: orders API: 404 Not Found: order not found (request 01a216c6-d3cb-7401-b10a-c48f733f68f4)
exit=1

$ orderctl get 42; echo "exit=$?"
orderctl: get: "42" is not a valid order ID
exit=2

$ orderctl frobnicate; echo "exit=$?"
orderctl: unknown command "frobnicate" (run orderctl -h)
exit=2

$ orderctl -h > /dev/null; echo "exit=$?"
exit=0
```

!!! abstract "Convention: `-h` exits 0, mistakes exit 2, failures exit 1"

    **Rule:** asking for help is success. A wrong command line is `2` (the code the `flag` package and most Unix tools use). A runtime failure is `1`. A signal is `128 + signal number`.

    **Why:** `if orderctl get "$ID"; then ...` must only enter the `then` branch when the order really exists. CI systems, `make`, `xargs` and shell scripts all make decisions from exit codes. A tool that exits `0` on failure gives all of them wrong information.

## Error messages that help

Read this real error from left to right:

```text
orderctl: list orders: send request: Get "http://127.0.0.1:9999/v1/orders": dial tcp 127.0.0.1:9999: connect: connection refused
```

Each layer added a few words with `fmt.Errorf("...: %w", err)`:

| Piece                                    | Added by                       |
| ---------------------------------------- | ------------------------------ |
| `orderctl:`                              | `main`                         |
| `list orders:`                           | `runList`                      |
| `send request:`                          | `client.do`                    |
| `Get "http://127.0.0.1:9999/v1/orders":` | `net/http` (a `*url.Error`)    |
| `dial tcp ...: connection refused`       | the `net` package, from the OS |

Together they say _what_ the user wanted, _where_ it failed, and _why_. No stack trace needed.

!!! abstract "Convention: error strings are lowercase, without punctuation at the end"

    **Rule:** write `fmt.Errorf("list orders: %w", err)`, not `"Failed to list orders!"`.

    **Why:** errors get **chained**. A capital letter or a `.` in the middle of `a: B.: c` looks broken. And do not start with "failed to" or "error": everything in an error chain has failed, so those words add nothing. Linters (`revive`'s `error-strings` rule, `staticcheck` ST1005) check this.

Server errors carry even more: the API returns a request ID, and the client puts it in the message (`(request 01a216c6-...)`). A user can paste that line into a support ticket, and an engineer can find the exact request in the server logs.

## Timeouts and Ctrl-C

If a CLI can hang forever, it will eventually do so, for example in CI. `orderctl` sets one deadline for the whole command:

```go
	// One deadline for the whole command, and Ctrl-C cancels it too.
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
```

The parent `ctx` comes from `signal.NotifyContext` in `main`, so the context is cancelled by **either** the timeout **or** Ctrl-C. Every HTTP request is made with this context. See [Context and cancellation](context.md) for how this tree of contexts works.

```text
$ orderctl -timeout 1ns list
orderctl: list orders: send request: Get "http://localhost:8080/v1/orders": context deadline exceeded
```

Pressing Ctrl-C while the server does not answer (here simulated with `timeout -s INT 1`):

```text
$ timeout --preserve-status -s INT 1 orderctl -server http://127.0.0.1:18083 list; echo "exit=$?"
orderctl: interrupted
exit=130
```

Notice a subtle detail in `main`: it checks `ctx.Err()` **before** calling `stop()`. `stop()` also cancels the context, so after it, `ctx.Err()` would always be non-nil and every error would look like an interrupt.

!!! warning "Trap: `http.DefaultClient` has no timeout"

    `http.Get(url)` uses `http.DefaultClient`, whose `Timeout` is zero, which means **no limit**. A server that accepts the connection but never answers blocks the caller forever. In `orderctl` the context bounds every request. The `client` package itself uses `&http.Client{Timeout: 10 * time.Second}` when the caller passes `nil`, so it is safe by default.

## The client package

`internal/client` turns HTTP calls into normal Go method calls:

```go
c, err := client.New("http://localhost:8080", "orderctl/v1.2.0", nil)
o, err := c.GetOrder(ctx, id)
```

The core is one private method that every call goes through:

```go
	req, err := http.NewRequestWithContext(ctx, method, c.base.JoinPath(path).String(), body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// err is a *url.Error, which already names the method and URL.
		return fmt.Errorf("send request: %w", err)
	}
	// Read to EOF and close, so the TCP connection goes back to the pool.
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		_ = resp.Body.Close()
	}()
	limited := io.LimitReader(resp.Body, maxResponseBytes)
```

Every line protects against a real production problem:

- **`NewRequestWithContext`** connects the request to the deadline and to Ctrl-C. `http.NewRequest` (without context) cannot be cancelled. The `noctx` linter flags it.
- **`User-Agent`** with the version lets server operators see which client versions are still in use before they remove an old API.
- **Drain and close the body.** Go reuses TCP connections (keep-alive) only if you read the response body to the end _and_ close it. Forget it, and every request opens a new connection: slow, and under load you run out of ports or file descriptors. The `bodyclose` linter catches a missing `Close`.
- **`io.LimitReader`.** A broken or hostile server could send gigabytes. The limit caps what we read.

### Domain errors across the network

When the server answers `404`, the client returns an `*APIError`. Its `Unwrap` method maps HTTP statuses back to the **domain** errors:

```go
func (e *APIError) Unwrap() error {
	switch e.StatusCode {
	case http.StatusNotFound:
		return order.ErrNotFound
	case http.StatusBadRequest:
		return order.ErrInvalid
	default:
		return nil
	}
}
```

Now code using the client writes `errors.Is(err, order.ErrNotFound)`, exactly like code that calls the service directly. The network becomes an implementation detail. The test `TestNotFoundIsDomainError` in `cmd/orderctl` checks this end to end.

`decodeAPIError` also handles bodies that are not JSON at all. A load balancer in front of the service might return an HTML "502 Bad Gateway" page. The client still reports the status instead of a confusing JSON parse error.

### A deliberate copy of the wire types

The client defines its own `orderJSON` struct, even though the server has an almost identical one. That looks like duplication, and it is, on purpose:

!!! abstract "Convention: a little copying is better than a little dependency"

    **Rule:** when two packages share a small shape, copying it is often better than creating a dependency between them.

    **Why:** if the client imported the server's `httpapi` package, every server refactor would ripple into the client, and the CLI binary would contain server code. The risk of copying is that the copies drift apart. Here, that risk is covered by a **contract test**: the client tests run against the _real_ server handler (next section). If the copies ever disagree, the build fails.

## Version information

When a user reports a bug, the first question is "which version?". `orderctl version` answers it:

```text
$ orderctl version
orderctl aea3486-dirty (go1.27.4, commit aea3486d8a844022a3d22d541bfaf71213e422ec-dirty, committed 2026-11-26T07:06:41Z)
```

Where does this come from? When you run `go build` inside a Git repository, the toolchain **embeds** build information in the binary: the Go version, the module version, the commit hash, the commit time, and whether there were uncommitted changes (`dirty`). You can see it with `go version -m`:

```text
$ go version -m bin/ordersd
bin/ordersd: go1.27.4
	path	github.com/acme/orders/cmd/ordersd
	mod	github.com/acme/orders	(devel)
	build	-buildmode=exe
	build	-compiler=gc
	build	-trimpath=true
	build	CGO_ENABLED=0
	build	GOARCH=amd64
	build	GOOS=linux
	build	GOAMD64=v1
	build	vcs=git
	build	vcs.revision=aea3486d8a844022a3d22d541bfaf71213e422ec
	build	vcs.time=2026-11-26T07:06:41Z
	build	vcs.modified=true
```

The program reads the same data at run time with `runtime/debug.ReadBuildInfo()`. The `internal/buildinfo` package wraps it:

```go
func fromBuildInfo(bi *debug.BuildInfo, override string) Info {
	info := Info{
		Version:   orDefault(override, bi.Main.Version),
		GoVersion: bi.GoVersion,
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			info.Revision = s.Value
		case "vcs.time":
			info.Time = s.Value
		case "vcs.modified":
			info.Modified = s.Value == "true"
		}
	}
	info.Version = orDefault(info.Version, "(devel)")
	return info
}
```

The `override` comes from a package variable set at **link time**:

```sh
go build -ldflags="-X github.com/acme/orders/internal/buildinfo.version=v1.2.0" ./cmd/orderctl
```

`-X` overwrites the value of a string variable in the final binary. The Makefile uses `git describe` to fill it in. A Docker build has no `.git` folder, so there the version is passed as a build argument (see [Building a service](services.md)).

!!! tip "Why split `Read` and `fromBuildInfo`?"

    `debug.ReadBuildInfo` reads _this_ binary, so a test cannot control its output. `fromBuildInfo` takes the data as a parameter, so `buildinfo_test.go` can feed it any combination and check the result. This "pure core, thin impure shell" split is the same idea as `main` and `run`.

## Testing the CLI

Because `run` takes all its inputs as parameters, a test can drive the **whole CLI** in-process against a **real server**:

```go
func newCLI(t *testing.T) cli {
	h := httpapi.NewHandler(order.NewService(memory.New()), slog.New(slog.DiscardHandler), func() bool { return true })
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return cli{t: t, server: srv.URL}
}

func (c cli) run(args ...string) (stdout, stderr string, err error) {
	c.t.Helper()
	var out, errOut strings.Builder
	getenv := func(k string) string {
		if k == "ORDERS_SERVER" {
			return c.server
		}
		return ""
	}
	err = run(c.t.Context(), args, getenv, &out, &errOut)
	return out.String(), errOut.String(), err
}
```

`httptest.NewServer` starts the real HTTP handler on a random local port. `getenv` is a tiny fake: the test never touches the real environment, so tests can run in parallel safely.

Then exit codes become a simple table test (see [Testing](testing.md)):

```go
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"help", []string{"-h"}, exitOK},
		{"version", []string{"version"}, exitOK},
		{"no command", nil, exitUsage},
		{"unknown command", []string{"delete"}, exitUsage},
		{"unknown flag", []string{"-verbose", "list"}, exitUsage},
		{"bad output format", []string{"-o", "yaml", "list"}, exitUsage},
		{"bad item flag", []string{"create", "-item", "BOOK"}, exitUsage},
		{"bad id", []string{"get", "42"}, exitUsage},
		{"not found", []string{"get", "019ac0de-0000-7000-8000-000000000001"}, exitError},
		{"server rejects order", []string{"create", "-customer", "c-1"}, exitError},
		{"server down", []string{"-server", "http://127.0.0.1:1", "list"}, exitError},
	}
```

The client package goes one step further with `httptest.NewTestServer` (new in Go 1.27). It runs the server on an **in-memory network**, so no real port is opened at all. You must use `srv.Client()` to reach it:

```go
	h := httpapi.NewHandler(order.NewService(memory.New()), slog.New(slog.DiscardHandler), func() bool { return true })
	srv := httptest.NewTestServer(t, h)
	c, err := New(srv.URL, "orderctl-test", srv.Client())
```

This is the **contract test** mentioned earlier: real client, real server handler, real JSON in between.

Coverage for the whole project, from a real run:

```text
$ go test -cover ./...
ok  	github.com/acme/orders/cmd/orderctl	0.088s	coverage: 85.7% of statements
ok  	github.com/acme/orders/cmd/ordersd	0.059s	coverage: 70.2% of statements
ok  	github.com/acme/orders/internal/buildinfo	0.004s	coverage: 95.7% of statements
ok  	github.com/acme/orders/internal/client	0.060s	coverage: 85.3% of statements
ok  	github.com/acme/orders/internal/config	0.006s	coverage: 98.0% of statements
ok  	github.com/acme/orders/internal/httpapi	0.012s	coverage: 91.9% of statements
ok  	github.com/acme/orders/internal/order	0.005s	coverage: 86.8% of statements
ok  	github.com/acme/orders/internal/storage/memory	0.006s	coverage: 93.1% of statements
```

The untested lines are mostly in `main()` itself: signal handling and `os.Exit`. That is the point of keeping `main` tiny. What is left there is too simple to break.

## Shipping the binary

The Makefile target:

```make
build: ## Build both binaries into ./bin
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN)/ ./cmd/...
```

- **`CGO_ENABLED=0`** builds a fully static binary that does not need the system's C library. It runs on any Linux of the same CPU type, including tiny containers.
- **`-trimpath`** removes your local folder names (like `/home/you/src/...`) from the binary. Builds become reproducible, and you do not leak your file system layout.
- **`-ldflags="-s -w"`** drops the symbol table and debug info: smaller binaries (stack traces still work, because Go keeps its own runtime tables).
- **`./cmd/...`** builds every program under `cmd/` in one command.

Cross-compiling is two environment variables, no extra tools (see [The Go toolchain](toolchain.md)):

```sh
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o bin/orderctl-darwin-arm64 ./cmd/orderctl
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o bin/orderctl.exe ./cmd/orderctl
```

For real releases, teams often use **GoReleaser**: one config file that cross-compiles, creates archives and checksums, signs them, and publishes a GitHub release and a Homebrew formula.

## Traps

| Trap                                              | What goes wrong                                                  | Fix                                             |
| ------------------------------------------------- | ---------------------------------------------------------------- | ----------------------------------------------- |
| Logic in `main()`                                 | Untestable; `os.Exit` skips `defer`                              | `main` calls `run(...) error`                   |
| Global `flag.String(...)`                         | "flag redefined" in tests; `ExitOnError` exits from inside a lib | `flag.NewFlagSet(..., flag.ContinueOnError)`    |
| Messages on stdout                                | Breaks pipes and `jq`                                            | Results on stdout, everything else on stderr    |
| Exit 0 on failure                                 | Scripts and CI continue after errors                             | Map errors to exit codes in one place           |
| No timeout                                        | Tool hangs forever when the server stalls                        | `context.WithTimeout` for the whole command     |
| Not draining `resp.Body`                          | No connection reuse; port exhaustion under load                  | Read to EOF and `Close`, in a `defer`           |
| Printing domain structs as JSON                   | Internal renames silently break users' scripts                   | A dedicated output struct = explicit contract   |
| Validating business rules in the CLI too          | Two copies of the rules drift apart                              | CLI checks shape; the server checks rules       |
| Flags after the command (`orderctl list -o json`) | Go's `flag` stops at the first non-flag                          | Document "global flags first" in the usage text |

## Check yourself

??? question "1. Why does `orderctl` pass `os.Getenv` into `run` instead of calling `os.Getenv` inside it?"

    So tests can pass a fake `getenv`. Real environment variables are global to the process: a test that
    sets one (`os.Setenv`) affects every other test running in parallel. A function parameter has no such
    problem, and the signature of `run` now lists every input the program has.

??? question "2. A colleague writes `log.Fatal(err)` inside `runCreate`. What can go wrong?"

    `log.Fatal` calls `os.Exit(1)` immediately. Deferred calls in `run` and `runCreate` (like `cancel()`
    or flushing an output buffer) do not run. The exit code is always 1, even for usage errors that should
    be 2. And the test calling `run` would kill the whole test binary. Return the error instead.

??? question "3. `orderctl list` works, but `orderctl list | head -1` sometimes prints an error. Which stream should that error go to, and why?"

    stderr. `head -1` closes the pipe after one line, so the next write fails with "broken pipe". The
    error message must not go into the pipe (which is closed anyway) but to the terminal. Since `orderctl`
    writes all messages to stderr, this works.

??? question "4. Why does the client map HTTP 404 back to `order.ErrNotFound` in `Unwrap`?"

    So code that uses the client can check `errors.Is(err, order.ErrNotFound)`, exactly as if it used the
    service directly. Callers do not need to know about HTTP status codes, and code can be moved between
    "in-process" and "over the network" without changing its error handling.

??? question "5. Your CLI's JSON output is produced by `json.Marshal(order)` on the domain type. What risk does that create?"

    The output format is now coupled to internal code. Renaming a field, adding a secret field, or
    changing a type silently changes what users' scripts receive. A dedicated output struct makes the
    contract explicit and turns every change into a deliberate decision.

## Recap

- `main` is tiny: collect signals, args, env and streams, call `run`, map the error to an exit code.
- Use `flag.NewFlagSet` with `ContinueOnError`. Subcommands are more flag sets and a `switch`.
- Precedence: flag > environment > default. The human typing always wins.
- Data to stdout, messages to stderr. JSON output is a contract: give it its own struct.
- Exit codes are API: `0` success, `1` failure, `2` usage, `130` interrupted.
- One deadline for the whole command; Ctrl-C cancels it. Never use a client without a timeout.
- Drain and close response bodies. Map HTTP errors back to domain errors.
- Embed version information; the toolchain does most of it for you.
- Test the whole CLI in-process against a real handler.

Next: the other half of the project, [Building a service](services.md).
