# Basics

## Why care?

You join a team. On day one you open a pull request with this:

```go
var total int = 0
var names []string = []string{}
for i := 0; i < len(items); i = i + 1 {
	total = total + items[i].Quantity
}
```

It compiles. It works. And the reviewer leaves five comments. Not because it is wrong, but because it is **not how Go is written**. Every line says "this person learned another language first".

Go is a small language. You can learn all of its syntax in a weekend. But "knowing the syntax" and "writing Go like a Go programmer" are two different things. This page teaches you both at once: every piece of syntax comes with the way it is _used_ in real code, and why.

## The smallest real program

```go
package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("missing name argument")
	}
	fmt.Println("hello,", args[0])
	return nil
}
```

Already a lot to see:

- **`package main`** says "this package builds into a program". Every other package name builds into a library that other code imports.
- **`import`** lists the packages we use. Only what we use: an unused import is a compile error.
- **`func main()`** is the entry point. It takes no arguments and returns nothing.
- We do the real work in **`run`**, which returns an `error`. `main` only decides how to exit. This "small `main`, testable `run`" shape is the standard in production code. You will build a full version in [Building a CLI](cli.md).

## Variables

Go has two ways to declare a variable:

```go
var count int          // long form: name, type, no value → zero value
var name = "orders"    // long form with a value: type is inferred (string)
port := 8080           // short form: only inside functions, type inferred
```

The `:=` is called a **short variable declaration**. It declares _and_ assigns. It is what you will use most of the time inside functions.

!!! abstract "Convention: `:=` inside functions, `var` when the zero value is the point"

    **Rule:** Use `x := value` when you have a starting value. Use `var x T` when you want the zero
    value on purpose (for example `var buf bytes.Buffer`, `var mu sync.Mutex`, `var orders []Order`).
    Don't write `var x int = 0` or `x := []string{}` without a reason.

    **Why:** Each form _tells the reader something_. `var buf bytes.Buffer` says "I start empty, and
    empty is a valid state". `n := len(items)` says "here is a value I computed". If you mix them
    randomly, the reader loses that signal. `var total int = 0` says the same thing twice, and the
    reader stops to wonder if the `= 0` is important.

### Zero values: there is no "uninitialised"

In C, a new variable holds whatever garbage was in that memory. In Java, a field can be `null` and crash later. In Go, **every variable always has a value**. If you don't give one, it gets the **zero value** of its type.

| Type                                                | Zero value                |
| --------------------------------------------------- | ------------------------- |
| numbers (`int`, `float64`, ...)                     | `0`                       |
| `bool`                                              | `false`                   |
| `string`                                            | `""` (empty string)       |
| pointers, slices, maps, channels, functions, errors | `nil`                     |
| structs                                             | every field is its zero   |
| arrays                                              | every element is its zero |

### Try it in Go

```go
package main

import (
	"fmt"
	"time"
)

type Item struct {
	SKU        string
	Quantity   int
	PriceCents int64
}

func main() {
	var (
		n     int
		ok    bool
		name  string
		price float64
		items []Item
		stock map[string]int
		when  time.Time
		err   error
		item  Item
	)
	fmt.Printf("n=%d ok=%t name=%q price=%v\n", n, ok, name, price)
	fmt.Printf("items=%v len=%d nil=%t\n", items, len(items), items == nil)
	fmt.Printf("stock=%v len=%d nil=%t\n", stock, len(stock), stock == nil)
	fmt.Printf("when.IsZero()=%t err=%v\n", when.IsZero(), err)
	fmt.Printf("item=%+v\n", item)

	items = append(items, Item{SKU: "MUG-01", Quantity: 2, PriceCents: 1299})
	fmt.Println("after append:", len(items))
	fmt.Println("read from nil map:", stock["MUG-01"])
}
```

Output:

```text
n=0 ok=false name="" price=0
items=[] len=0 nil=true
stock=map[] len=0 nil=true
when.IsZero()=true err=<nil>
item={SKU: Quantity:0 PriceCents:0}
after append: 1
read from nil map: 0
```

Look closely at the last two lines. A `nil` slice is fine to `append` to. A `nil` map is fine to _read_ from (you get the zero value). Go's designers made `nil` as harmless as they could. (Writing to a `nil` map still panics; see [Types and data](types-and-data.md).)

!!! info "Under the hood: why zero values are cheap"

    When Go allocates memory, the runtime gives it to you **already filled with zero bytes** (the OS
    gives new pages as zeros too, see [Memory](../fundamentals/memory.md)). The zero value of every
    type is designed to be "all bits zero". So "initialising" a variable costs nothing extra.
    That is the hardware reason. The design reason is the next box.

!!! abstract "Convention: make the zero value useful"

    **Rule:** Design your own types so that `var x T` is ready to use, when you can.
    `sync.Mutex`, `bytes.Buffer` and `strings.Builder` are the classic examples: no constructor needed.

    **Why:** A type that works at its zero value can never be "used before it was set up". There is no
    `NewMutex()` to forget. It also means a struct that _contains_ such a field works at its zero value
    too, for free. Fewer constructors, fewer ways to get it wrong. See
    [Conventions](conventions.md#make-the-zero-value-useful) for the full story.

### Money is an integer

Note `PriceCents int64` above, not `Price float64`. A `float64` can't hold `0.10` exactly (see [Bits, bytes and numbers](../fundamentals/bits-and-bytes.md)). Add up a thousand prices and you are a few cents off. In any system that touches money, store **integer minor units** (cents) and format them only for display.

## Constants and `iota`

Constants are values the compiler knows at build time:

```go
const maxRetries = 3
const defaultTimeout = 5 * time.Second
```

Go has no `enum` keyword. Instead, you combine a **named type** with **`iota`**, a counter that starts at `0` in each `const` block and goes up by one per line.

```go
package main

import "fmt"

// Weekday is a day of the week.
type Weekday int

const (
	Sunday Weekday = iota
	Monday
	Tuesday
)

// ByteSize is a number of bytes.
type ByteSize int64

const (
	_           = iota // skip 0
	KB ByteSize = 1 << (10 * iota)
	MB
	GB
)

const timeout = 5 // untyped constant

func main() {
	fmt.Println(Sunday, Monday, Tuesday)
	fmt.Println(KB, MB, GB)
	var f float64 = timeout
	var d int64 = timeout
	fmt.Println(f, d)
	const huge = 1 << 100
	fmt.Println(huge >> 98)
}
```

Output:

```text
0 1 2
1024 1048576 1073741824
5 5
4
```

Two interesting things happened:

1. Lines without `= ...` **repeat the expression above them** with the next `iota`. So `MB` is `1 << (10 * 2)`.
2. `timeout` is an **untyped constant**. It is not an `int` yet; it is "the number 5". It becomes whatever type it is used as: `float64` in one line, `int64` in the next. Untyped constants are also computed with very high precision by the compiler: `1 << 100` does not fit in any Go integer type, but as a constant it is fine, as long as the final result fits.

!!! warning "Trap: `iota` enums and the zero value"

    With `Sunday = iota`, the zero value of `Weekday` _is_ `Sunday`. So a struct where nobody set the
    day silently says "Sunday". For business states that is dangerous: an order with no status set
    would look like a real, valid status.

    Two fixes are common: start at `iota + 1` and treat `0` as "unknown", or use a **string type**:

    ```go
    // Status is the life-cycle state of an order.
    type Status string

    const (
    	StatusPending   Status = "pending"
    	StatusPaid      Status = "paid"
    	StatusShipped   Status = "shipped"
    	StatusCancelled Status = "cancelled"
    )
    ```

    The zero value `""` is clearly "not set", and the values are readable in logs, JSON and the
    database. This is what the orders project uses.

## Control flow

### `if` with a short statement

```go
if err := validate(o); err != nil {
	return fmt.Errorf("create order: %w", err)
}
```

The part before `;` runs first, and `err` only exists inside the `if` (and its `else`). This keeps names in the smallest possible scope. No parentheses around the condition. Braces are **always** required, even for one line.

!!! abstract "Convention: always braces, `{` on the same line"

    **Rule:** `if x {` on one line. Never `if x return`. Never `{` on its own line.

    **Why:** Apple's famous "goto fail" bug (2014) was a C `if` without braces: a second line looked
    like it belonged to the `if`, but it ran every time, and TLS certificate checks were skipped.
    Go removes that whole bug class. The `{` placement is because Go inserts semicolons
    automatically at the end of lines; a `{` on the next line would end the statement too early.
    One rule, enforced by the compiler, and no team ever argues about it.

### One loop keyword: `for`

Go has no `while` and no `do`. `for` does everything:

```go
for i := 0; i < 10; i++ { }   // classic three-part loop
for i := range 10 { }         // range over an int (Go 1.22): i = 0..9
for n > 0 { }                 // "while"
for { }                       // forever (exit with break or return)
for i, item := range items { } // index and copy of element
for key, value := range m { }  // map: key and value
for i, r := range "héllo" { }  // string: byte index and rune (see encodings)
```

**Why one keyword?** Less to learn, and every loop reads the same way. The designers asked "do we need `while`?" and the answer was "no, `for` with one condition _is_ a while loop".

```go
package main

import "fmt"

func main() {
	var prints []func()
	for i := range 3 {
		prints = append(prints, func() { fmt.Print(i, " ") })
	}
	for _, p := range prints {
		p()
	}
	fmt.Println()

	for i, r := range "héllo" {
		fmt.Print(i, ":", string(r), " ")
	}
	fmt.Println()

	status := "paid"
	switch status {
	case "pending":
		fmt.Println("waiting for payment")
	case "paid", "shipped":
		fmt.Println("money received")
	default:
		fmt.Println("unknown")
	}
}
```

Output:

```text
0 1 2
0:h 1:é 3:l 4:l 5:o
money received
```

Line one: each closure prints a different `i`. Since **Go 1.22, every loop iteration has its own copy of the loop variable**. Before 1.22 this printed `3 3 3`, and that was one of the most common Go bugs in history (especially with goroutines started inside loops). If you read old code or old blog posts with `i := i` inside a loop, that was the workaround. You don't need it anymore.

Line two: ranging over a string gives **runes**, not bytes. The index jumps from `1` to `3` because `é` takes two bytes in UTF-8. See [Encodings](../fundamentals/encodings.md).

### `switch` without fall-through

- No `break` needed: a case ends by itself. (Use `fallthrough` if you really want C behaviour. You almost never do.)
- One case can match many values: `case "paid", "shipped":`.
- A `switch` with no value is a clean `if/else if` chain:

```go
switch {
case qty <= 0:
	return ErrInvalidQuantity
case qty > maxQty:
	return ErrTooMany
}
```

**Why no automatic fall-through?** In C, forgetting `break` is a classic bug. In practice, you almost always want "stop after this case". So Go made the common case the default.

## Functions

```go
func split(totalCents int64, parts int) (each int64, remainder int64) {
	each = totalCents / int64(parts)
	remainder = totalCents % int64(parts)
	return each, remainder
}
```

- Types come **after** names: `parts int`. Read it left to right: "parts is an int". This stays readable even for complex types like `func(ctx context.Context) error`.
- A function can return **multiple values**. This is how Go returns a result _and_ an error: `func Get(id string) (Order, error)`.
- Results can have names (`each`, `remainder`). Use names when they **document** what two values of the same type mean, like here. Don't use them just to save a line.

!!! abstract "Convention: no naked returns in long functions"

    **Rule:** With named results you _can_ write a bare `return`. Only do that in very short functions,
    if at all. Write `return each, remainder`.

    **Why:** In a 40-line function, a bare `return` forces the reader to scroll up and track what every
    named result holds at that point. Explicit is cheaper to read than short.

### Functions are values

You can store a function in a variable, pass it, and return it. A function that uses variables from around it is a **closure**:

```go
func counter() func() int {
	n := 0
	return func() int {
		n++
		return n
	}
}
```

`n` lives on after `counter` returns, because the closure still uses it. (The compiler moves `n` to the heap; see escape analysis in [Memory](../fundamentals/memory.md).) Closures are everywhere in Go: HTTP middleware, `sort.Slice`, functional options, `defer`.

## `defer`: clean up on every path

`defer` schedules a function call to run **when the surrounding function returns**, no matter how it returns: normal `return`, early `return`, or panic.

```go
package main

import "fmt"

func trace(name string) func() {
	fmt.Println("enter", name)
	return func() { fmt.Println("leave", name) }
}

func countdown() {
	defer trace("countdown")()
	for i := range 3 {
		defer fmt.Println("deferred", i)
	}
	fmt.Println("body done")
}

func arg() {
	x := 1
	defer fmt.Println("deferred x =", x)
	x = 2
	fmt.Println("x =", x)
}

func main() {
	countdown()
	arg()
}
```

Output:

```text
enter countdown
body done
deferred 2
deferred 1
deferred 0
leave countdown
x = 2
deferred x = 1
```

Three rules, all visible in the output:

1. Deferred calls run in **reverse order** (last in, first out), like a stack of plates. That is what you want: if you open A then B, you close B then A.
2. The **arguments are evaluated immediately**, at the `defer` line. That is why `x` printed `1`, not `2`.
3. `defer trace("countdown")()` runs `trace(...)` **now** (prints "enter") and defers the function it _returns_. A neat trick, and a good test of rule 2.

!!! abstract "Convention: `defer` the cleanup right after the acquire"

    **Rule:**

    ```go
    mu.Lock()
    defer mu.Unlock()

    resp, err := client.Do(req)
    if err != nil {
    	return err
    }
    defer resp.Body.Close()
    ```

    **Why:** The "open" and the "close" sit next to each other, so a reviewer sees at a glance that
    they match. Every early `return` you add later is automatically safe. Without `defer`, each new
    `return` is a chance to leak a lock, a file or a connection.

### Enterprise detail: errors from `Close`

For a file you **write**, `Close` can fail (for example, the disk is full and the last buffer can't be flushed). `defer f.Close()` throws that error away, and you lose data silently. The pattern below keeps it, using a named result and `errors.Join`:

```go
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// writeReport writes body to path. It reports an error from Close too,
// because for files a failed Close can mean the data never reached the disk.
func writeReport(path string, body []byte) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create report: %w", err)
	}
	defer func() {
		err = errors.Join(err, f.Close())
	}()

	if _, err := f.Write(body); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

func main() {
	dir, err := os.MkdirTemp(".", "out")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	fmt.Println(writeReport(filepath.Join(dir, "report.txt"), []byte("ok\n")))
	fmt.Println(writeReport(filepath.Join(dir, "missing", "report.txt"), []byte("ok\n")))
}
```

Output (your temp folder name will differ):

```text
<nil>
create report: open out2276903977/missing/report.txt: no such file or directory
```

The deferred closure can change `err` because it is a **named result**: it is a real variable that the `return` statement fills in _before_ deferred calls run. This is the one place where named results are clearly the right tool. `errors.Join(nil, nil)` is `nil`, so the happy path stays clean. Errors get a full page later: [Errors](errors.md).

!!! warning "Trap: `defer` in a loop"

    ```go
    for _, path := range paths {
    	f, err := os.Open(path)
    	if err != nil {
    		return err
    	}
    	defer f.Close() // runs when the FUNCTION returns, not the loop iteration
    }
    ```

    With 10,000 paths you hold 10,000 open files and hit "too many open files"
    (see [Processes and threads](../fundamentals/processes-and-threads.md)). Move the loop body into
    its own function, so each `defer` runs at the end of each call.

## Unused things are errors

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	total := 10
	fmt.Println("hi")
}
```

```text
# demo
./main.go:5:2: "os" imported and not used
./main.go:9:2: declared and not used: total
```

Most languages give a warning. Go refuses to build. The full _why_ is in [Conventions](conventions.md#unused-imports-and-variables-are-errors); in short: unused imports slow down builds of huge codebases, and an unused variable is very often a bug (you computed something and forgot to use it). Use `_` when you really want to ignore a value: `_, err := w.Write(p)`.

!!! warning "Trap: `:=` can shadow a variable"

    ```go
    func process() error {
    	var err error
    	if true {
    		n, err := load(true) // NEW err, only inside this block
    		_ = n
    		_ = err
    	}
    	return err // always the outer err: nil
    }
    ```

    Output: `process returned: <nil>`, even though `load` failed. `:=` declares **new** variables if at
    least one name on the left is new _in this scope_. Inside the `if` block, both `n` and `err` are
    new, so the outer `err` is never set. `go vet` does not catch this by default; the `shadow`
    analyzer in [golangci-lint](tooling-and-quality.md) can. The real fix is code shape: return early
    instead of carrying an `err` variable through blocks.

## Putting it together

Here is a function written the way a Go reviewer would want it. Read it slowly and notice each habit from this page:

```go
// TotalCents returns the sum of quantity × price for all items.
// It returns an error if any item has a negative quantity or price.
func TotalCents(items []Item) (int64, error) {
	var total int64
	for i, it := range items {
		if it.Quantity < 0 || it.PriceCents < 0 {
			return 0, fmt.Errorf("item %d (%s): negative quantity or price", i, it.SKU)
		}
		total += int64(it.Quantity) * it.PriceCents
	}
	return total, nil
}
```

- `var total int64`: the zero value is the starting point, on purpose.
- `range` instead of index arithmetic. Short loop names (`i`, `it`) because the scope is three lines.
- Invalid input is rejected **first**, with an early return. The happy path runs down the left edge.
- The error message says _what_ and _where_ (index and SKU), in lower case, with no trailing period.
- Money in `int64` cents. The conversion `int64(it.Quantity)` is explicit: Go **never** converts number types for you, so you always see where a conversion (and a possible overflow) happens.

## Check yourself

??? question "1. What does `var orders []Order` contain, and can you `append` to it?"

    It is a `nil` slice with length 0. Yes, `append` works on a `nil` slice and allocates as needed.
    `len` and `range` also work. Careful with JSON: the classic `encoding/json` (v1) writes a
    `nil` slice as `null`, not `[]`.

??? question "2. Why is a string-based `Status` type often better than an `iota` one for business states?"

    With `iota` starting at 0, the zero value is a real, valid state, so "not set" looks like a
    choice. A string type has `""` as a clear "not set" value, and the values are readable in logs,
    JSON and databases.

??? question "3. What does this print, and why?"

    ```go
    x := 1
    defer fmt.Println(x)
    x = 2
    ```

    It prints `1`. Arguments to a deferred call are evaluated at the `defer` line, not when it runs.
    Use a closure `defer func() { fmt.Println(x) }()` if you want the value at return time.

??? question "4. Why does Go refuse to compile a program with an unused import?"

    Unused imports make builds slower (every imported package must be compiled and linked) and add
    hidden dependencies, which matters a lot in huge codebases. Making it an error keeps every file
    honest about what it really uses. Tools like `goimports` add and remove imports for you.

??? question "5. Where can `defer f.Close()` lose data, and how do you fix it?"

    When writing a file: `Close` may flush buffered data and fail, and `defer f.Close()` ignores the
    error. Use a named `err` result and `defer func() { err = errors.Join(err, f.Close()) }()`,
    or call `Close` explicitly at the end and check it.

## Recap

- Every variable has a **zero value**. Design types so the zero value is useful.
- `:=` for computed values, `var` when the zero value is the point.
- Enums are **named types + constants**. For business states, prefer strings over `iota`.
- `for` is the only loop. Since Go 1.22 each iteration has its own loop variable.
- `switch` cases don't fall through.
- Multiple return values carry results _and_ errors.
- `defer` runs at function exit, in reverse order, with arguments evaluated immediately.
- Unused imports and variables are compile errors. Watch out for `:=` shadowing.

Next: [Types and data](types-and-data.md), where we open up slices and maps and see how they really work.
