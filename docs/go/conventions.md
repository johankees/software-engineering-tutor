# Conventions: the Go way

## Why care?

Open ten Python or Java projects from ten companies. You will find ten styles: different indentation, different naming, different ways to handle errors, different folder layouts. Every new project starts with a week of "how do they do things here?".

Open ten Go projects. They look **almost the same**. Same formatting, same naming, same error handling, same test style. A Go developer can join a new team and send a useful pull request on day one.

That is not luck. Go's community decided very early that **consistency across all Go code in the world** is worth more than any one person's taste. This page collects the conventions that make this work. For each one you get the **rule**, but more importantly the **why**. Once you understand the reasons, you won't need to memorise the rules: you'll find yourself arriving at them on your own, and you'll know what to do in cases no rule covers.

## Where the conventions come from

There is no single rulebook, but there are four sources that everyone in the Go world respects. In order of how often you'll see them quoted in code review:

| Source                                                            | What it is                                                                                                      |
| ----------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| [Effective Go](https://go.dev/doc/effective_go)                   | The original guide by the Go team (2009). The "why" behind the language's idioms. Some parts are dated.         |
| [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments) | The list of comments the Go team made most often in code reviews. Short, practical, widely used as a checklist. |
| [Google Go Style Guide](https://google.github.io/styleguide/go/)  | Google's internal guide, made public. Very detailed: _Guide_, _Decisions_ and _Best Practices_.                 |
| [Go Proverbs](https://go-proverbs.github.io/)                     | Rob Pike's short sayings (2015) that capture Go's philosophy in one line each.                                  |

The Google style guide gives a helpful **priority order** for when goals conflict. Readable code is, in this order:

1. **Clear**: the reader understands what it does and why.
2. **Simple**: it does it in the simplest way possible.
3. **Concise**: high signal, little noise.
4. **Maintainable**: easy to change correctly later.
5. **Consistent**: looks like the rest of the codebase.

Keep this list in mind. Almost every convention below is one of these five goals, made concrete.

## Formatting: `gofmt` decides, nobody argues

```sh
gofmt -d ugly.go   # show what would change
gofmt -w .         # rewrite all files in place
```

Here is a file written by someone who doesn't care about formatting:

```go
package order
import "fmt"
type Status string
func (s Status) Valid() bool{
  switch s {
  case "pending","paid": return true
  }
  return false
}
func Describe(s Status)string{return fmt.Sprintf("status=%s",s)}
```

And what `gofmt -d` says:

```text
diff ugly.go.orig ugly.go
--- ugly.go.orig
+++ ugly.go
@@ -1,10 +1,14 @@
 package order
+
 import "fmt"
+
 type Status string
-func (s Status) Valid() bool{
-  switch s {
-  case "pending","paid": return true
-  }
-  return false
+
+func (s Status) Valid() bool {
+	switch s {
+	case "pending", "paid":
+		return true
+	}
+	return false
 }
-func Describe(s Status)string{return fmt.Sprintf("status=%s",s)}
+func Describe(s Status) string { return fmt.Sprintf("status=%s", s) }
```

!!! abstract "Convention: all Go code is `gofmt`-formatted"

    **Rule:** Run `gofmt` (or `goimports`, which also fixes imports) on save, and check it in CI.
    There are no settings to choose: tabs for indentation, spaces for alignment, no line length limit.

    **Why:** Formatting debates are pure waste. Teams can spend hours arguing about brace style, and
    the result doesn't change what the program does. `gofmt` removes the debate by having **no
    options**. That has three deeper benefits:

    - **Diffs show only real changes.** If everyone formats the same way, a code review diff never
      contains "I reformatted this file". Merge conflicts from formatting disappear.
    - **Your eyes learn one shape.** After a few weeks, you read Go code faster because every file
      has the same layout. Unusual layout is a signal that something unusual is happening.
    - **Tools can rewrite code safely.** Because there is one canonical format, tools like `gofmt -r`,
      `go fix` and `gopls` refactorings can change code and print it back without making a mess.
      Large-scale automated refactoring across millions of lines was one of Go's original goals.

    As Rob Pike put it: _"Gofmt's style is no one's favourite, yet gofmt is everyone's favourite."_

    **Tool:** `gofmt`, `goimports`, the `gofmt`/`goimports` formatters in
    [golangci-lint](tooling-and-quality.md). Many teams also use `gofumpt`, a stricter superset.

!!! info "Why tabs?"

    Tabs for indentation means each reader can set the width they like (2, 4, 8) in their editor,
    without changing the file. Alignment (like the `=` in a `const` block) uses spaces, so it lines up
    for everyone. Go has no line length limit because breaking a line at an arbitrary column can hurt
    readability; break lines where it makes the code clearer, not at column 80.

## Imports

```go
import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/acme/orders/internal/order"
)
```

!!! abstract "Convention: group imports, standard library first"

    **Rule:** One `import ( ... )` block. Groups separated by a blank line: standard library, then
    third-party, then (optionally) your own module. `goimports` sorts within each group.
    Never use dot imports (`import . "pkg"`) outside of tests. Rename an import only to avoid a
    conflict (`jsonv1 "encoding/json"`).

    **Why:** The import block is a **summary of dependencies**. Reading it tells you what the file
    talks to: the network, a database, a third-party SDK. Grouping makes that summary scannable: a
    reviewer sees at once that a "pure domain" file just started importing an HTTP library. Dot
    imports and needless renames hide where a name comes from, so the reader can't tell `Get` from
    `http.Get`.

### Unused imports and variables are errors

You saw this in [Basics](basics.md#unused-things-are-errors):

```text
./main.go:5:2: "os" imported and not used
./main.go:9:2: declared and not used: total
```

!!! abstract "Convention (enforced by the compiler): nothing unused"

    **Rule:** Every import and every local variable must be used. Use `_` to ignore a value on
    purpose: `_, err := w.Write(p)` or `import _ "embed"` for side effects.

    **Why:** Two reasons, one practical and one about correctness.

    - **Build speed at scale.** Each import is a package the compiler must load and the linker must
      process. In a giant codebase, unused imports pile up over years, and builds get slower for no
      benefit. Making them errors means they can never accumulate. (Go also forbids **circular**
      imports, for the same reason: the dependency graph stays a clean tree that compiles fast and in
      parallel.)
    - **An unused variable is usually a bug.** You computed `total` and then returned something else.
      A warning would be ignored, like most warnings are. An error can't be ignored.

    Go's designers didn't want warnings at all: _either it's a problem, so fix it, or it's not, so
    don't bother me._ Things that are "maybe a problem" go to `go vet` and linters instead.

## Naming

Naming is where most Go code review comments land. The theme: **names should be as short as possible, and as long as necessary, given where they are used.**

### `MixedCaps`, never underscores

```go
maxRetries       // not max_retries, not MAX_RETRIES
OrderService     // not Order_Service
defaultTimeout   // constants too: not DEFAULT_TIMEOUT
```

!!! abstract "Convention: MixedCaps for everything"

    **Rule:** `camelCase` for unexported names, `PascalCase` for exported ones. No underscores, no
    `ALL_CAPS` constants. (Exceptions: test function names like `TestCreate_EmptyItems` may use
    underscores, and generated code is left alone.)

    **Why:** In Go, the **case of the first letter has meaning** (next section). `MAX_RETRIES` would be
    exported, just because it is a constant, which is not what the writer meant. With one naming
    style for all identifiers, the only thing that varies is the meaningful part: exported or not.

### Capital letter = exported

```go
type Order struct {
	ID     uuid.UUID // exported: other packages can read it
	status Status    // unexported: only package order can see it
}
```

There are no `public`, `private` or `protected` keywords. An identifier that starts with an **upper-case** letter is visible outside its package. Everything else is private to the package.

!!! abstract "Convention: export as little as possible"

    **Rule:** Start unexported. Export a name only when another package really needs it.

    **Why:** The capital-letter rule puts visibility **in the name itself**, so you see it at every
    place the name is used, not only where it is declared. When you read `o.status` anywhere, you
    know it is internal. That is a design choice for readers.

    Exporting is a **promise**: once other code uses an exported name, changing it breaks them. In a
    library with outside users, you can't take it back without a major version (see
    [Modules](modules.md)). Unexported names are free to change. Note that the unit of privacy is
    the **package**, not the type: all code in package `order` can see `status`. Keep packages
    focused so that this is safe.

### Short names for small scopes

```go
for i, o := range orders {      // i and o: the scope is three lines
	total += o.TotalCents()
}

func (s *Service) Create(ctx context.Context, o Order) error // ctx, s, o: universal
var ErrInsufficientStock = errors.New("order: insufficient stock") // package level: descriptive
```

!!! abstract "Convention: name length grows with scope"

    **Rule:** One or two letters for loop variables, receivers and very short functions (`i`, `r`,
    `w`, `b`, `ctx`, `err`). Descriptive names for package-level identifiers, struct fields and
    anything used far from its declaration. Don't put the type in the name (`ordersMap`,
    `nameString`).

    **Why:** A name is a pointer to its declaration. If the declaration is two lines up, the reader
    still has it in mind, and a long name only adds noise that hides the **shape** of the code. If the
    declaration is in another file, the name must carry all the meaning by itself. Common short names
    (`r` for a reader or request, `w` for a writer, `ctx`, `err`, `ok`) are a shared vocabulary in
    the whole Go world, so they cost the reader nothing. Type names in variables are redundant:
    the compiler and the editor already know the type, and they go stale when the type changes.

### Package names

```go
import "github.com/acme/orders/internal/order"     // good
import "github.com/acme/orders/internal/orderutils" // bad
import "github.com/acme/orders/internal/common"     // bad
import "github.com/acme/orders/internal/models"     // bad
```

!!! abstract "Convention: short, lower-case, single-word package names that say what it provides"

    **Rule:** Lower case, no underscores or `mixedCaps`, usually a singular noun: `order`, `http`,
    `json`, `memory`, `config`. The package name is the last part of the import path. Avoid
    meaningless names: `util`, `utils`, `common`, `helpers`, `misc`, `base`, `models`, `types`.

    **Why:** In Go, the package name is **part of every use** of its contents: callers write
    `order.Service`, `json.Marshal`, `http.Get`. So the package name is not a folder label, it is the
    first word of every identifier. A good one makes the call site read like a sentence. `util.Parse`
    tells you nothing; `money.Parse` tells you everything.

    Worse, `util`-style packages become **dumping grounds**. Because the name doesn't say what
    belongs there, everything ends up there. Soon every package imports `util`, and `util` imports
    half the codebase, and you get import cycles and slow builds. A package named after what it
    _provides_ has a natural boundary. "Is this about money? Then it goes in `money`."
    The `models`/`types` pattern (all types in one package, all logic elsewhere) comes from other
    languages and breaks Go's design, where a package holds a **type and the behaviour that goes
    with it**. More in [Project layout](project-layout.md).

### No stutter

Because the package name is always present at the call site, don't repeat it:

| Stutters                | Reads well                                                            |
| ----------------------- | --------------------------------------------------------------------- |
| `order.OrderService`    | `order.Service`                                                       |
| `order.NewOrderService` | `order.NewService`                                                    |
| `http.HTTPServer`       | `http.Server`                                                         |
| `config.LoadConfig`     | `config.Load`                                                         |
| `order.Order`           | `order.Order` (fine: the main type of the package may share its name) |

**Why:** `order.OrderService` says "order" twice. Callers read the package name every time, so the type name only needs to add what is new. Linters report this; you'll see it below.

### No `Get` prefix on getters

```go
func (o *Order) Customer() CustomerID        // not GetCustomer
func (o *Order) SetCustomer(c CustomerID)    // setters keep "Set"
```

**Why:** `o.Customer()` already reads as "the order's customer". `Get` adds no information. Go reserves `Get` for methods that actually _fetch_ something, like `http.Get` or `repo.Get(ctx, id)`, where it signals "this does real work, maybe I/O". So the prefix keeps its meaning instead of being noise. Source: [Effective Go: Getters](https://go.dev/doc/effective_go#Getters).

### Initialisms keep one case

```go
userID     // not userId
apiURL     // not apiUrl
ServeHTTP  // not ServeHttp
xmlHTTPRequest, parseJSON, OrderID
```

**Why:** `ID`, `URL`, `HTTP` are words written in capitals in normal English. Writing `Url` turns a familiar shape into an unfamiliar one, and mixed styles in one codebase (`UserId` here, `UserID` there) make names hard to guess and search. Keeping initialisms all-upper (or all-lower when unexported at the start: `url`, `id`) is one simple, checkable rule.

### Other naming patterns you will see everywhere

| Pattern                          | Example                                     | Why                                                                      |
| -------------------------------- | ------------------------------------------- | ------------------------------------------------------------------------ |
| Constructor `New` / `NewX`       | `order.NewService(repo)`, `bufio.NewReader` | If the package has one main type, `New` is enough: `ring.New`.           |
| One-method interfaces end in -er | `Reader`, `Notifier`, `Validator`           | Names the role. See [Methods and interfaces](methods-and-interfaces.md). |
| Error variables start with `Err` | `ErrNotFound`, `io.EOF` (old exception)     | Easy to find, easy to recognise in `errors.Is(err, ErrNotFound)`.        |
| Error types end in `Error`       | `ValidationError`, `*fs.PathError`          | Distinguishes the type from sentinel values.                             |
| Boolean names read as questions  | `isValid`, `hasStock`, `ok`                 | `if hasStock {` reads like English.                                      |
| `Must` functions panic           | `regexp.MustCompile`, `template.Must`       | Warns the reader: this panics on error. Only for init-time constants.    |

### Let the linter show you

Here is a file that breaks many naming rules at once:

```go
// Package order holds the domain model for customer orders.
package order

import (
	"errors"
	"fmt"
)

// OrderService handles orders.
type OrderService struct {
	customerId string
	apiUrl     string
}

func (this *OrderService) GetCustomerId() string { return this.customerId }

func Cancel(id string) error {
	if id == "" {
		return errors.New("Order ID is empty.")
	} else {
		return fmt.Errorf("cancel %s: not implemented", id)
	}
}
```

`golangci-lint run` with the `revive` linter enabled (configuration in [Tooling and code quality](tooling-and-quality.md)):

```text
order/order.go:10:6: exported: type name will be used as order.OrderService by other packages, and that stutters; consider calling this Service (revive)
order/order.go:11:2: var-naming: struct field customerId should be customerID (revive)
order/order.go:12:2: var-naming: struct field apiUrl should be apiURL (revive)
order/order.go:15:1: exported: exported method OrderService.GetCustomerId should have comment or be unexported (revive)
order/order.go:17:1: exported: exported function Cancel should have comment or be unexported (revive)
order/order.go:19:21: error-strings: error strings should not be capitalized or end with punctuation or a newline (revive)
order/order.go:20:9: indent-error-flow: if block ends with a return statement, so drop this else and outdent its block (revive)
```

Seven findings, each one a convention from this page. Notice what the linter did **not** report: the receiver named `this`, and the `Get` prefix. Linters can't catch everything; that is what code review (and this page) is for.

## Comments and documentation

Go documentation is generated from ordinary comments. There is no special syntax like Javadoc's `@param`.

```go
// Package order holds the domain model for customer orders:
// what an order is, which states it can be in, and the rules
// for moving between them.
package order

// ErrNotFound is returned when an order does not exist.
var ErrNotFound = errors.New("order: not found")

// Status is the life-cycle state of an [Order].
type Status string

// Cancel moves the order to [StatusCancelled].
// It returns an error if the order has already shipped.
func (o *Order) Cancel() error {
```

`go doc` turns these into documentation, in the terminal:

```text
$ go doc ./order
package order // import "github.com/acme/orders/order"

Package order holds the domain model for customer orders: what an order is,
which states it can be in, and the rules for moving between them.

var ErrNotFound = errors.New("order: not found")
type Order struct{ ... }
type Status string
    const StatusPending Status = "pending" ...

$ go doc ./order Order.Cancel
package order // import "github.com/acme/orders/order"

func (o *Order) Cancel() error
    Cancel moves the order to StatusCancelled. It returns an error if the order
    has already shipped.
```

The same comments become the web pages on [pkg.go.dev](https://pkg.go.dev) and the hover text in your editor. `[Order]` and `[StatusCancelled]` are **doc links**: on pkg.go.dev and in editors, they become clickable.

!!! abstract "Convention: doc comments are full sentences that start with the name"

    **Rule:** Every exported name has a comment directly above it. It is a complete sentence that
    starts with the name: `// Cancel moves the order ...`. Every package has one package comment
    (`// Package order ...`), in one file only (often `doc.go` for long ones). To deprecate, add a
    paragraph starting with `Deprecated:`.

    **Why:** Starting with the name makes the comment work in every place it is shown: in `go doc`
    lists, in search results (`grep "^// Cancel"`), and in editor pop-ups where you might see only the
    first line. Full sentences force the writer to say something real. `Deprecated:` is a convention
    that tools understand: `gopls` and `staticcheck` warn callers who still use the name.

!!! abstract "Convention: comments explain _why_, code explains _what_"

    **Rule:** Inside functions, don't write comments that repeat the code (`// increment i`). Write
    comments for things the code can't say: why this approach, why this odd-looking line is
    correct, what invariant must hold.

    **Why:** A "what" comment goes stale the moment someone changes the code without updating it, and
    then it lies. A "why" comment captures knowledge that is _not_ in the code, like "we retry only
    once because the payment provider is not idempotent". That is the information the next engineer
    really needs. If you need a "what" comment, the code is probably not clear enough: rename a
    variable or extract a function instead.

## Errors are values: style rules

Errors get a full page ([Errors](errors.md)). These are the **style** conventions:

```go
if err != nil {
	return fmt.Errorf("create order %s: %w", o.ID, err)
}
```

!!! abstract "Convention: error strings are lower case, no final punctuation, and add context"

    **Rule:** `errors.New("order not found")`, not `"Order not found."`. When you return an error from
    a call, wrap it with what **you** were doing: `"create order: %w"`. Don't start with "failed to"
    or "error:" (every error is a failure).

    **Why:** Errors are **chained**. By the time an error reaches a log, it looks like this:

    ```text
    handle POST /orders: create order 7f3a: insert row: connection refused
    ```

    Each layer added one lower-case phrase. If one layer had written `"Failed to insert row."`, the
    line becomes `create order 7f3a: Failed to insert row.: connection refused`. Capitals and periods
    in the middle of a sentence are wrong. And "failed to" in every layer is pure noise. The
    resulting chain reads like a stack trace written in English, and costs nothing at run time.

!!! abstract "Convention: handle an error once"

    **Rule:** Either **handle** the error (log it, return an HTTP status, retry, use a fallback) **or
    return** it (wrapped). Not both. Never `log.Printf(...)` and then `return err`.

    **Why:** If every layer logs _and_ returns, one failure produces five log lines at five levels,
    with partial context each. In production, that makes incidents harder to read, not easier. The
    layer at the top (the HTTP handler, the CLI's `main`) has the most context, so that is where the
    error gets logged, once, with the full chain.

## Control flow: keep the happy path on the left

Compare these two versions of the same function.

Nested (common in other languages):

```go
func (s *Service) Ship(ctx context.Context, id uuid.UUID) error {
	o, err := s.repo.Get(ctx, id)
	if err == nil {
		if o.Status == StatusPaid {
			o.Status = StatusShipped
			if err := s.repo.Update(ctx, o); err == nil {
				return nil
			} else {
				return fmt.Errorf("ship order %s: %w", id, err)
			}
		} else {
			return fmt.Errorf("ship order %s: status is %s, want paid: %w", id, o.Status, ErrInvalid)
		}
	} else {
		return fmt.Errorf("ship order %s: %w", id, err)
	}
}
```

Go style:

```go
func (s *Service) Ship(ctx context.Context, id uuid.UUID) error {
	o, err := s.repo.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("ship order %s: %w", id, err)
	}
	if o.Status != StatusPaid {
		return fmt.Errorf("ship order %s: status is %s, want paid: %w", id, o.Status, ErrInvalid)
	}

	o.Status = StatusShipped
	if err := s.repo.Update(ctx, o); err != nil {
		return fmt.Errorf("ship order %s: %w", id, err)
	}
	return nil
}
```

!!! abstract "Convention: return early; no `else` after `return`"

    **Rule:** Handle errors and special cases first, and return. The normal flow continues down the
    left margin without indentation. If an `if` block ends in `return`, don't write `else`.

    **Why:** This is called **line of sight**. In the Go version, you can read the happy path by
    scanning straight down the left edge: get, check, update, done. Every indented block is "an
    exception, and it leaves". In the nested version, you need to match each `else` with its `if`
    and keep a mental stack of conditions. Also, each error is handled right next to the call that
    caused it, so you can check at a glance that none is missed. The `indent-error-flow` lint rule
    above enforces the "no `else` after `return`" part.

## API design conventions

### Accept interfaces, return structs

```go
// Accepts the smallest interface it needs.
func WriteReport(w io.Writer, orders []Order) error

// Returns the concrete type.
func New() *Store // package memory
```

!!! abstract "Convention: accept interfaces, return concrete types"

    **Rule:** Function parameters should be the **smallest interface** that has the methods you call.
    Return values should be **concrete types** (usually a struct pointer), not interfaces. The main
    exception is `error`, and functions that may return different implementations on purpose.

    **Why:** Two directions, two reasons.

    - **Accepting an interface** makes your function work with more callers. `WriteReport(w
      io.Writer)` works with a file, an HTTP response, a buffer in a test, a gzip writer. If it took
      `*os.File`, all of those would need a temporary file.
    - **Returning a concrete type** gives the caller the full API, with its documentation, and lets
      you **add methods later** without breaking anyone. If `memory.New` returned an `order.Repository`
      interface, adding a method to the interface would break every other implementation, and
      callers couldn't reach anything that isn't in the interface. The caller can always assign your
      struct to _their own_ interface (that is what implicit interfaces are for).

    Source: [Go Code Review Comments: Interfaces](https://go.dev/wiki/CodeReviewComments#interfaces).
    See [Methods and interfaces](methods-and-interfaces.md) for how this works.

### Make the zero value useful

```go
var mu sync.Mutex        // ready, no constructor
var buf bytes.Buffer     // ready: buf.WriteString("x") works
var b strings.Builder    // ready
var wg sync.WaitGroup    // ready
```

!!! abstract "Convention: make the zero value useful"

    **Rule:** Where you can, design a type so that `var x T` (or a struct literal with only some
    fields) works correctly. Create internal maps lazily, use sensible defaults for zero fields
    (`if c.Timeout == 0 { timeout = 10 * time.Second }`). If the zero value _can't_ work, unexport the
    fields and provide `NewT`, so nobody can build a broken one.

    **Why:** Go has zero values whether you like it or not. Every `var x T`, every new struct field,
    every missing JSON field produces one. If your type's zero value is broken, it _will_ be used
    somewhere by accident. If it works, a whole class of "forgot to initialise" bugs is gone. It also
    composes: a struct made of useful-zero-value fields has a useful zero value itself, with no
    constructor chain. Effective Go calls this out explicitly; it is a Go proverb too: _"Make the
    zero value useful."_

### Exported fields are fine; getters and setters are not the default

```go
// Config holds the server settings. A zero field means "use the default".
type Config struct {
	Addr         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}
```

!!! abstract "Convention: no getters and setters by default"

    **Rule:** For plain data (configs, DTOs, events, options), use exported fields. Add methods only
    when they do something: keep an invariant, compute a value, hide a representation. When you do
    hide a field, the getter is `Name()` and the setter is `SetName()`.

    **Why:** In Java, getters and setters exist "in case we need to change the implementation later".
    In Go, that later change is cheap: you can unexport the field and add a method, and the compiler
    shows you every call site to fix (inside your module; for public libraries, see
    [Modules](modules.md) on compatibility). Writing getters up front costs reading time on every
    type and hides which fields are really plain data. But the opposite is true for types with
    **rules**: an `Order`'s status must only move `pending → paid → shipped`, so `status` is
    unexported and changed only through methods like `Pay()` and `Ship()`. Fields for data, methods
    for behaviour.

### A little copying is better than a little dependency

!!! abstract "Convention: don't add a dependency for a few lines"

    **Rule:** If you need a 10-line helper from a big library, copy (or rewrite) those 10 lines,
    with a test and a comment saying where they came from (and respecting the license). Prefer the
    standard library. Add a third-party module only when it gives you real, substantial value.

    **Why:** Every dependency is a long-term cost: it can break your build, bring in its own
    dependencies, have security vulnerabilities you must track (see `govulncheck` in
    [Tooling](tooling-and-quality.md)), change its API, or be abandoned. In an enterprise, each one
    may need a license review and a security scan. A few copied lines have none of those costs. Go's
    standard library is unusually complete (HTTP server and client, JSON, crypto, testing, logging,
    templates, SQL interface, UUIDs since 1.27) precisely so that this is practical. This proverb
    is why Go projects often have very short `go.mod` files.

### `context.Context` goes first

```go
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Order, error)
```

**Rule:** Any function that does I/O, can block, or calls something that does, takes `ctx context.Context` as its **first** parameter. Don't store a context in a struct. **Why:** a context carries the request's deadline and cancellation signal; making it the first parameter everywhere means it is never forgotten and always easy to spot. The full story is in [Context and cancellation](context.md).

### Let the caller decide about concurrency

**Rule:** Write functions that do their work **synchronously** and return when done. Don't start goroutines inside a library function and return before they finish, unless that is the function's whole purpose (like `http.Server.Serve`), and then give the caller a way to stop it. **Why:** The caller can always add concurrency (`go f()`) to a synchronous function. Removing hidden concurrency from an asynchronous function is impossible. Hidden goroutines also leak, race, and make tests flaky. See [Concurrency](concurrency.md).

## Things Go programmers avoid

### `panic` for normal errors

!!! abstract "Convention: don't panic"

    **Rule:** Return errors. Use `panic` only for **programmer bugs** that should never happen
    (an impossible `switch` default, a violated invariant) and during start-up for values that are
    constants in disguise (`regexp.MustCompile` of a literal pattern). A library must never let a
    panic escape to its callers as a way of reporting errors.

    **Why:** A panic unwinds the stack and, if nobody recovers, **kills the whole process**. In a
    server, that is every in-flight request, not just the bad one. Errors as values make failure
    visible in the function signature (`(Order, error)`) so every caller must decide what to do. A
    panic is invisible in the signature, so callers can't know they need to handle it. That is the
    same reason Go has no exceptions. (`net/http` does recover panics in handlers to keep the server
    alive, but treats them as bugs and logs a stack trace. Don't rely on it.)

### `init()` functions

```go
func init() {
	db = mustConnect(os.Getenv("DATABASE_URL")) // don't
}
```

!!! abstract "Convention: avoid `init()`; prefer explicit setup in `main`"

    **Rule:** Don't use `init()` for anything that can fail, does I/O, reads the environment or
    flags, or depends on order. Set things up explicitly in `main` (or a `run` function) and pass
    them down. Acceptable uses: registering a driver or codec that the package itself defines, or
    computing a lookup table from constants.

    **Why:** `init` runs **automatically when the package is imported**, before `main`, in an order
    the reader can't see. It can't return an error, so failures become panics or, worse, are ignored.
    It runs in **tests** too: importing your package in a unit test suddenly tries to connect to a
    database. And it makes the package impossible to use twice with different settings. Explicit
    setup in `main` is boring, visible and testable, which is exactly what you want.

### Mutable global state

```go
var defaultRepo = memory.New() // package-level, mutable: avoid

func CreateOrder(o Order) error { return defaultRepo.Create(context.Background(), o) }
```

!!! abstract "Convention: no mutable package-level variables; pass dependencies explicitly"

    **Rule:** Package-level variables are fine for constants in disguise (`var ErrNotFound = ...`,
    compiled regexps, lookup tables). Everything that holds **state or configuration** (a logger, a
    DB pool, an HTTP client, a feature flag) lives in a struct and is passed in, usually through a
    constructor: `order.NewService(repo, logger)`.

    **Why:** Global state is the opposite of everything Go optimises for.

    - **Hidden dependencies.** `CreateOrder(o)` looks pure, but it secretly talks to storage. The
      signature lies. With `svc.Create(ctx, o)`, you can see what it needs.
    - **Tests interfere.** Two tests that change the same global can't run with `t.Parallel()`, and
      a test that forgets to reset it breaks other tests, in random order.
    - **No second instance.** You can't run two services with different configs in one process (or
      one test).
    - **Data races.** A global that is written after start-up must be protected by a lock, and
      somebody will forget.

    The standard library itself has old examples of this (`http.DefaultClient`, which has **no
    timeout**, `flag.CommandLine`, the default `log` and `slog` loggers). They are convenient for
    small scripts. In services, create your own and pass them in. See how `main` wires everything in
    [Building a service](services.md).

## Go Proverbs

Rob Pike's [Go Proverbs](https://go-proverbs.github.io/) are short, so they are easy to quote in a code review. Here are the ones you'll hear most, with what they mean in practice:

| Proverb                                                                   | What it means                                                                                                                         |
| ------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| _Don't communicate by sharing memory, share memory by communicating._     | Prefer passing data between goroutines over channels to having many goroutines lock the same data. See [Concurrency](concurrency.md). |
| _Concurrency is not parallelism._                                         | Concurrency is how you **structure** a program (independent tasks); parallelism is running them at the same time on many CPUs.        |
| _The bigger the interface, the weaker the abstraction._                   | Small interfaces (`io.Reader`) are satisfied by many types and are hugely reusable.                                                   |
| _Make the zero value useful._                                             | See above. `var x T` should just work.                                                                                                |
| _`interface{}` says nothing._                                             | A parameter of type `any` gives the reader and the compiler no information. Use real types, or generics.                              |
| _Gofmt's style is no one's favourite, yet gofmt is everyone's favourite._ | Consistency beats personal taste.                                                                                                     |
| _A little copying is better than a little dependency._                    | See above.                                                                                                                            |
| _Clear is better than clever._                                            | Write code for the reader. Tricks save you a minute and cost every reader ten.                                                        |
| _Errors are values._                                                      | Errors are ordinary data you can store, compare, wrap and program with, not special control flow.                                     |
| _Don't just check errors, handle them gracefully._                        | Add context, decide what to do. `if err != nil { return err }` everywhere is not handling.                                            |
| _Don't panic._                                                            | See above.                                                                                                                            |
| _Documentation is for users._                                             | Doc comments say what a function does and how to use it, not how it works inside.                                                     |
| _Reflection is never clear._                                              | Use `reflect` only in libraries that truly need it (encoders, ORMs). It is slow and loses type safety.                                |
| _Cgo is not Go._                                                          | Calling C gives up fast builds, easy cross-compiling, memory safety and cheap goroutine calls.                                        |

## Which tool enforces what

Conventions that a machine can check should be checked by a machine, so humans can review design instead of style.

| Convention                                                                              | Enforced by                                              |
| --------------------------------------------------------------------------------------- | -------------------------------------------------------- |
| Formatting                                                                              | `gofmt` / `goimports` / `gofumpt`                        |
| Unused imports and variables, import cycles                                             | The compiler                                             |
| Unkeyed struct literals, copied mutexes, `Printf` mistakes                              | `go vet`                                                 |
| Naming, stutter, initialisms, doc comments, error strings, `else` after `return`        | `revive`, `staticcheck` (`ST*` checks) in golangci-lint  |
| Unchecked errors                                                                        | `errcheck` in golangci-lint                              |
| Outdated idioms (for example manual loops that `slices` replaces)                       | `go fix` modernizers (Go 1.26), the `modernize` analyzer |
| `Get` prefix, receiver names, package naming, interface placement, global state, `init` | Code review (you!)                                       |

[Tooling and code quality](tooling-and-quality.md) shows the configuration used in the reference project.

## Check yourself

??? question "1. Why does Go use the case of the first letter for visibility instead of a `public` keyword?"

    So that visibility is visible **at every use**, not only at the declaration. When you read
    `o.status` anywhere in the code, you know it is package-internal. It also removes keywords and
    makes the language smaller.

??? question "2. A colleague creates `internal/utils` with `FormatMoney`, `ParseSKU` and `RetryHTTP`. What do you suggest, and why?"

    Split it by what each thing provides: `money.Format`, `sku.Parse`, and a retry helper next to the
    HTTP client that uses it. `utils` says nothing at the call site and becomes a dumping ground that
    everything imports, which leads to import cycles and unclear ownership.

??? question "3. Rewrite the names: `order.OrderRepository`, `GetStatus()`, `customerId`, `MAX_ITEMS`."

    `order.Repository`, `Status()`, `customerID`, `maxItems` (or `MaxItems` if exported).

??? question "4. Why should error strings be lower case without a final period?"

    Because errors are wrapped into one chain: `create order: insert row: connection refused`.
    Capitals and periods would appear in the middle of that sentence. Lower-case fragments compose
    into a readable line.

??? question "5. Why should `memory.New` return `*memory.Store` and not an `order.Repository` interface?"

    A concrete return type gives callers the full API and lets you add methods without breaking
    anyone. Callers that want an interface define their own (implicit satisfaction). Returning an
    interface forces one abstraction on everyone and makes adding methods a breaking change.

??? question "6. What's wrong with connecting to the database in `init()`?"

    It runs on import (including in unit tests), in an order you can't see, can't return an error,
    reads hidden configuration, and creates global state that blocks parallel tests and second
    instances. Do it in `main`/`run` and pass the pool down.

## Recap

- Conventions exist so that **all Go code looks the same**. Priority: clear, simple, concise, maintainable, consistent.
- `gofmt` formats everything. No options, no debates, clean diffs, safe automated refactoring.
- Imports grouped; nothing unused, enforced by the compiler.
- Names: `MixedCaps`; capital = exported; short for small scopes; package names short and meaningful; no stutter; no `Get`; initialisms in one case.
- Doc comments are full sentences starting with the name. Inline comments explain **why**.
- Error strings: lower case, no period, add context with `%w`. Handle each error once.
- Happy path on the left; return early.
- Accept interfaces, return structs. Make the zero value useful. Fields for data, methods for behaviour.
- Avoid: `panic` for errors, `init()` with side effects, mutable globals, needless dependencies.
- Let tools check what tools can check. Review the rest.

Next: [Methods and interfaces](methods-and-interfaces.md), where "accept interfaces, return structs" turns into code.
