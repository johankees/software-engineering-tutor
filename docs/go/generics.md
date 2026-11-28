# Generics and iterators

## Why care?

Your team has a `internal/xslices` package. Inside: `ContainsString`, `ContainsInt`, `ContainsUUID`, `UniqueStrings`, `UniqueInts`, `MapOrdersToIDs`, `MapItemsToSKUs`… forty small functions that do the same thing for different types. Every one has its own tests. Two of them have a bug the others don't.

Another option: someone wrote one `Contains(list []interface{}, v interface{}) bool`. Now every caller converts its slice to `[]interface{}` (one allocation per element), and passing an `int` where a `string` was meant compiles fine and is silently wrong.

Before Go 1.18 (2022), those were your only two options. **Generics** add a third: write the code **once**, keep **full type safety**, and let the compiler fill in the types.

And since Go 1.23, **iterators** let you write `for x := range something` over your own types: a paginated API, a database cursor, a tree. This page covers both, because they are designed to work together.

## The idea: functions with type parameters

A normal function has **value parameters**: "give me a number". A generic function also has **type parameters**: "first tell me _which type_, then give me values of that type".

```go
// Sum adds up any slice of numbers, including named types like Cents.
func Sum[T ~int | ~int64 | ~float64](values []T) T {
	var total T // the zero value of T
	for _, v := range values {
		total += v
	}
	return total
}
```

Read it out loud: _"Sum, for any type T that is an int, int64 or float64 underneath, takes a slice of T and returns a T."_

- `[T ...]` is the **type parameter list**. It comes before the normal parameters.
- `~int | ~int64 | ~float64` is the **constraint**: which types are allowed for `T`.
- Inside, `T` is used like any type. `var total T` gives its zero value.

You call it like a normal function. The compiler **infers** `T` from the arguments:

```go
prices := []Cents{1999, 500, 250}
fmt.Println("total:", Sum(prices)) // T inferred as Cents
```

```text
total: 2749
```

## Constraints are interfaces

A constraint says what you may do with `T`. In Go, constraints are just **interfaces**. Until 1.18, an interface was a list of methods. Now it can also be a list of **types**: a **type set**.

| Constraint                           | Allows                                           | Lets you use…                     |
| ------------------------------------ | ------------------------------------------------ | --------------------------------- |
| `any`                                | every type                                       | assign, pass around, nothing else |
| `comparable`                         | types that support `==` (no slices, maps, funcs) | `==`, `!=`, map keys              |
| `cmp.Ordered`                        | integers, floats, strings (and named versions)   | `<`, `>`, `==`, `+` (for numbers) |
| `~int \| ~int64`                     | those types and every named type built on them   | all operators those types share   |
| `fmt.Stringer`                       | types with a `String() string` method            | `.String()`                       |
| `interface{ ~string; Valid() bool }` | string-based types that have a `Valid` method    | both                              |

### Why the `~` matters

In a real service you don't use raw `int64` for money. You define `type Cents int64` (see [Types and data](types-and-data.md)), so that the compiler stops you from adding cents to a quantity. Without the `~`, a constraint only allows the **exact** types listed:

```go
func Sum[T int | int64](values []T) T { ... }

Sum([]Cents{1999, 500})
```

```text
./main.go:16:17: Cents does not satisfy int | int64 (possibly missing ~ for int64 in int | int64)
```

`~int64` means "**any type whose underlying type is `int64`**". That includes `Cents`, `time.Duration`, and every other named type built on `int64`. The compiler even tells you the fix. Almost always, you want the `~`. `cmp.Ordered` already uses it everywhere.

## Generic types

Types can have type parameters too. A typical enterprise example: one page of results from any list endpoint.

```go
// Page is one page of results from a paginated API.
type Page[T any] struct {
	Items     []T
	NextToken string // empty on the last page
}
```

Now `Page[order.Order]` and `Page[customer.Customer]` are two real, different types, both with full type checking. Methods on generic types use the type parameter from the receiver:

```go
func (p Page[T]) Last() bool { return p.NextToken == "" }
```

### Generic methods (new in Go 1.27)

For years, a method could use the type parameters of its receiver, but could not have **its own**. Since Go 1.27 it can:

```go
// Map returns a page with f applied to every item.
func (p Page[T]) Map[U any](f func(T) U) Page[U] {
	out := Page[U]{Items: make([]U, 0, len(p.Items)), NextToken: p.NextToken}
	for _, it := range p.Items {
		out.Items = append(out.Items, f(it))
	}
	return out
}
```

```go
ids := Page[int]{Items: []int{1, 2}, NextToken: "abc"}
labels := ids.Map(func(i int) string { return fmt.Sprintf("ord-%d", i) })
fmt.Printf("%+v\n", labels)
```

```text
{Items:[ord-1 ord-2] NextToken:abc}
```

There is one big limit, and it explains why this feature took so long: **generic methods have nothing to do with interfaces.** An interface can't declare one, and a generic method never satisfies an interface method:

```text
./main.go:20:5: interface method must have no type parameters
```

```text
Page[int] does not implement HasMap (wrong type for method Map)
		have Map[U any](func(int) U) Page[U]
		want Map(func(int) int) Page[int]
```

Why? Go checks at **run time** whether a value satisfies an interface (for example in a type assertion `v.(Mapper)`), by looking up its methods in a table. A generic method is really an infinite family of methods, one per `U`. The compiler can't know at build time which ones some type assertion will need later, so it can't put them in the table. Keeping generic methods out of interfaces avoids that problem completely.

So: a generic method is a nicer way to write a generic function that "belongs" to a type. Before Go 1.27 (and in code that must build with older versions) you write `func MapPage[T, U any](p Page[T], f func(T) U) Page[U]` instead.

!!! info "Generic type aliases (Go 1.24)"

    `type Set[T comparable] = map[T]struct{}` is an **alias**, not a new type. It is useful when you move a type
    between packages and want old code to keep compiling.

## Generic helpers in the standard library

Before you write a generic helper, check these three packages. They replace 90% of the `xslices` packages teams used to write.

- **`slices`**: `Contains`, `Index`, `Sort`, `SortFunc`, `SortStableFunc`, `BinarySearch`, `Max`, `Min`, `Reverse`, `Compact`, `Equal`, `Insert`, `Delete`, `Clone`, `Collect`, `Sorted`, `Chunk`…
- **`maps`**: `Keys`, `Values`, `All`, `Clone`, `Copy`, `Collect`, `DeleteFunc`, `Equal`…
- **`cmp`**: `Compare`, `Less`, `Or`, and the `Ordered` constraint.

Plus the built-in functions `min`, `max` and `clear` (Go 1.21).

### Try it in Go: sorting and grouping orders

```go
package main

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Cents is money in integer cents. Never use float for money.
type Cents int64

// Status is the lifecycle state of an order.
type Status string

type Item struct {
	SKU        string
	Quantity   int
	PriceCents Cents
}

// GroupBy puts each value in a bucket chosen by key.
func GroupBy[K comparable, V any](values []V, key func(V) K) map[K][]V {
	groups := make(map[K][]V)
	for _, v := range values {
		k := key(v)
		groups[k] = append(groups[k], v)
	}
	return groups
}

func main() {
	fmt.Println("max:", max(Status("paid"), Status("cancelled")))
	fmt.Println("cmp.Or:", cmp.Or("", "", "default-region"))

	items := []Item{
		{SKU: "BOOK-1", Quantity: 2, PriceCents: 1999},
		{SKU: "PEN-3", Quantity: 10, PriceCents: 150},
		{SKU: "BOOK-2", Quantity: 1, PriceCents: 2499},
	}
	slices.SortFunc(items, func(a, b Item) int {
		return cmp.Or(
			cmp.Compare(b.PriceCents, a.PriceCents), // price, high to low
			cmp.Compare(a.SKU, b.SKU),               // then SKU
		)
	})
	for _, it := range items {
		fmt.Printf("  %-6s %5d\n", it.SKU, it.PriceCents)
	}

	byKind := GroupBy(items, func(it Item) string {
		kind, _, _ := strings.Cut(it.SKU, "-")
		return kind
	})
	for _, k := range slices.Sorted(maps.Keys(byKind)) {
		fmt.Println(k, len(byKind[k]))
	}
}
```

Output:

```text
max: paid
cmp.Or: default-region
  BOOK-2  2499
  BOOK-1  1999
  PEN-3    150
BOOK 2
PEN 1
```

Three useful patterns:

1. **`cmp.Or` for multi-key sorting.** It returns the first non-zero value. So "compare by price; if equal, compare by SKU" is one expression. It is also handy for defaults: `cmp.Or(os.Getenv("REGION"), "eu-west-1")`.
2. **`b` before `a` for descending** order. No need for a `Reverse` step.
3. **`slices.Sorted(maps.Keys(m))`** for stable output from a map. Map order is random on purpose (see [Types and data](types-and-data.md)), so anything you print, log or test must be sorted first.

## Under the hood: how Go compiles generics

There are two classic ways to compile generics:

- **Full specialization** (C++, Rust): generate a separate copy of the function for every type. Fastest code, but slow builds and big binaries.
- **Boxing** (Java): one copy for everything, all values behind pointers. Small and fast to compile, but slow at run time (every value is an allocation).

Go picked a middle road called **GC shape stenciling with dictionaries**:

- Types with the same **GC shape** (same size and same pointer layout for the garbage collector) share **one** copy of the code. All pointer types have the same shape, so `Sum[*A]` and `Sum[*B]` share code. `int64` and `Cents` have the same shape, so they share too.
- Each call passes a hidden **dictionary**: a small table with type information and method pointers for the actual `T`.

Why this choice? It fits Go's values: **fast builds first**, good run-time speed second. The consequence for you:

- Generic code over **value types** (`int`, `string`, small structs) is usually as fast as hand-written code.
- Calling a **method** through a constraint on a **pointer** or interface type goes through the dictionary: an indirect call that the compiler can't inline. In a very hot loop, that can be slower than a plain interface call. If it matters, measure (see [Performance](performance.md)).

## Iterators: `range` over your own types

`for i, v := range s` works over slices, maps, strings, channels and (since Go 1.22) integers: `for i := range 10`. Since Go 1.23 it also works over **functions** with a special shape. Those functions are **iterators**.

The `iter` package names the two shapes:

```go
type Seq[V any] func(yield func(V) bool)
type Seq2[K, V any] func(yield func(K, V) bool)
```

The shape is unusual. Here is what it means:

- An iterator is a function that takes another function, `yield`.
- It calls `yield(v)` once for each value.
- `yield` returns `false` when the loop body did `break` (or `return`). Then the iterator **must stop**.

The compiler turns your loop body into the `yield` function. So this:

```go
for v := range seq {
	fmt.Println(v)
}
```

works roughly like this:

```go
seq(func(v int) bool {
	fmt.Println(v)
	return true // false if the body did break
})
```

This is a **push** iterator: the iterator pushes values into your loop. No channel, no goroutine, no allocation in the common case.

### Try it in Go: walk a paginated API

A real problem: the orders API returns 100 orders per page. You want to process **all** of them without writing the "next token" loop every time, and without loading 50,000 orders into memory first.

```go
// FetchFunc loads one page, starting at token.
type FetchFunc[T any] func(ctx context.Context, token string) (Page[T], error)

// All returns every item of every page, fetching pages lazily.
// The loop stops at the first error, which is yielded as the second value.
func All[T any](ctx context.Context, fetch FetchFunc[T]) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		token := ""
		for {
			page, err := fetch(ctx, token)
			if err != nil {
				var zero T
				yield(zero, fmt.Errorf("fetch page %q: %w", token, err))
				return
			}
			for _, item := range page.Items {
				if !yield(item, nil) {
					return // the caller used break: stop fetching
				}
			}
			if page.NextToken == "" {
				return
			}
			token = page.NextToken
		}
	}
}
```

The caller writes a normal loop. Here it runs with a fake API that serves 3 pages of 2 orders, and prints every fetch:

```go
for id, err := range All(ctx, fakeOrders) {
	if err != nil {
		return err
	}
	fmt.Println(id)
	count++
	if count == 3 {
		break
	}
}
```

Output, first for a full loop, then for a loop that breaks after three orders:

```text
all orders:
  (fetching page 0)
ord-1
ord-2
  (fetching page 1)
ord-3
ord-4
  (fetching page 2)
ord-5
ord-6
first three only:
  (fetching page 0)
ord-1
ord-2
  (fetching page 1)
ord-3
```

Look at the second run: after `break`, page 2 is **never fetched**. The iterator is **lazy**: it does only the work the caller needs. This is the main reason iterators exist.

!!! abstract "Convention: `iter.Seq2[T, error]` for iterators that can fail"

    **Rule:** when getting the next value can fail (network, disk, database), yield `(value, error)` pairs and stop
    after the first error.

    **Why:** the caller handles the error inside the loop, right where it uses the value, in the normal Go style.
    The alternative (an `Err()` method you must remember to call after the loop, like `bufio.Scanner` and `sql.Rows`)
    is easy to forget, and forgetting it hides failures.

### Iterators in the standard library

| Function                                                 | Gives you                               |
| -------------------------------------------------------- | --------------------------------------- |
| `slices.All(s)`, `slices.Values(s)`                      | index+value, or only values, of a slice |
| `slices.Backward(s)`                                     | a slice from end to start               |
| `maps.Keys(m)`, `maps.Values(m)`, `maps.All(m)`          | map contents (random order)             |
| `slices.Collect(seq)`, `slices.Sorted(seq)`              | turn an iterator back into a slice      |
| `maps.Collect(seq2)`                                     | turn a `Seq2` into a map                |
| `strings.SplitSeq`, `strings.FieldsSeq`, `strings.Lines` | split without allocating a slice        |
| `slices.Chunk(s, n)`                                     | sub-slices of `n` elements, for batches |

`slices.Chunk` is useful for batch work: inserting 10,000 rows in batches of 500 is `for batch := range slices.Chunk(rows, 500) { ... }`.

!!! tip "Pull iterators"

    Sometimes you need to take values **one at a time** from two iterators at once (for example, to merge two
    sorted streams). `iter.Pull(seq)` turns a push iterator into a `next()` function plus a `stop()` function.
    Always `defer stop()`. You won't need it often; push iterators cover most cases.

## Advanced: self-referencing constraints (Go 1.26)

Sometimes a constraint must talk about the type itself: "a type `A` that has a method `Add(A) A`". Since Go 1.26 a generic type can refer to itself in its own type parameter list:

```go
// Adder is any type that can add a value of its own type.
type Adder[A Adder[A]] interface {
	Add(A) A
}

type Cents int64

func (c Cents) Add(o Cents) Cents { return c + o }

func Total[A Adder[A]](values ...A) A {
	var total A
	for _, v := range values {
		total = total.Add(v)
	}
	return total
}
```

`Total(Cents(100), Cents(250))` prints `350`. You'll see this pattern in libraries for money, vectors or ordered keys. In application code it is rare.

## When to use generics (and when not)

Generics are a tool, not a style. The Go team's advice is clear: **write code, not types**.

!!! abstract "Convention: start concrete; go generic when the code is the same for many types"

    **Rule:** write a function for the one type you have. Make it generic only when you have (or clearly will have)
    the **same logic** for several types, and the logic does not care what the type is.

    **Why:** generic code is harder to read: the reader must keep `T` and its constraint in their head. A concrete
    function is the simplest thing that works. Go values readability over cleverness. Most application code
    (handlers, services, repositories) has nothing generic about it.

Good fits:

- **Containers**: sets, caches, queues, trees, `Page[T]`, `Result[T]`.
- **Algorithms on collections**: sort, filter, group, chunk, dedupe.
- **Plumbing** that moves values without looking at them: iterators, pipelines, retry helpers that return a `T`.

Bad fits:

- **When an interface is enough.** If your function only calls methods, take an interface. `func Save(w io.Writer)` is simpler than `func Save[W io.Writer](w W)`, and does the same thing.
- **To avoid writing two similar functions** whose logic is not really the same. Two simple functions beat one clever one.
- **"Generic repository"** layers like `Repository[T]` with `Get`, `List`, `Save` for every table. They look clean at first. Later, the `orders` table needs a join, `customers` needs a soft delete, and the generic layer gets in the way. Write a repository per aggregate.

!!! quote "Go proverb"

    _A little copying is better than a little dependency._

## Traps

!!! warning "Trap 1: `comparable` can still panic"

    ```go
    values := []any{"BOOK-1", 42, []string{"a"}}
    fmt.Println(slices.Index(values, any(42)))
    fmt.Println(slices.Index(values, any([]string{"a"})))
    ```

    ```text
    1
    panic: runtime error: comparing uncomparable type []string
    ```

    `any` satisfies `comparable` (since Go 1.20), because interface values support `==`. But if two interfaces hold
    a slice, the comparison panics at **run time**. Avoid `comparable` over interface types with data you don't control.

!!! warning "Trap 2: ignoring what `yield` returns"

    ```go
    func Broken() iter.Seq[int] {
    	return func(yield func(int) bool) {
    		for i := range 3 {
    			yield(i) // BUG: must stop when yield returns false
    		}
    	}
    }
    ```

    A loop that `break`s on the first value crashes:

    ```text
    0
    panic: runtime error: range function continued iteration after function for loop body returned false
    ```

    Always write `if !yield(v) { return }`. The run-time check exists so this bug can't hide.

!!! warning "Trap 3: printing or testing map keys without sorting"

    `maps.Keys` returns keys in random order. A test that compares `slices.Collect(maps.Keys(m))` to a fixed list
    passes sometimes and fails sometimes. Use `slices.Sorted(maps.Keys(m))`.

!!! warning "Trap 4: needless type parameters"

    `func Process[T Order](o T)` where only one type will ever be used. This adds reading cost and nothing else.
    If you can't name a second type that would use it, remove the type parameter.

## Check yourself

??? question "1. What does the `~` in `~int64` mean, and why do you nearly always want it?"

    "Any type whose underlying type is `int64`." Without it, named types like `Cents` or `time.Duration` are
    rejected, and real code uses named types a lot.

??? question "2. Go 1.27 added generic methods. Why can't they satisfy an interface?"

    Interface satisfaction is checked with method tables, sometimes at run time (type assertions). A generic method
    is an infinite family of methods, and the compiler can't know which instances a future assertion would need.
    So interfaces can't declare generic methods, and generic methods never match interface methods.

??? question "3. Your function only calls `Write` on its argument. Generic `[W io.Writer]` or plain `io.Writer`?"

    Plain `io.Writer`. An interface parameter does the same job, is simpler to read, and doesn't need type parameters.

??? question "4. In an iterator, what does it mean when `yield` returns `false`, and what must you do?"

    The loop body stopped early (`break`, `return`, `goto`, or panic). The iterator must return immediately, and
    should stop any further work (no more page fetches). Calling `yield` again panics.

??? question "5. Why do Go generics use \"GC shape stenciling\" instead of one copy per type?"

    To keep builds fast and binaries small, which Go values highly, while keeping good speed for value types.
    The price is an indirect call through a dictionary in some cases, e.g. methods on pointer type parameters.

## Recap

- Type parameters `[T constraint]` let you write code once with full type safety. Usually `T` is inferred.
- Constraints are interfaces: method sets **and** type sets. `any`, `comparable`, `cmp.Ordered`, and `~T` for named types.
- Generic types like `Page[T]`. Generic methods since Go 1.27, but they never take part in interfaces.
- Check `slices`, `maps`, `cmp` and `min`/`max` before writing your own helpers.
- Iterators are functions `func(yield func(V) bool)`. Use them with `range`. They are lazy. Always respect `yield`'s result.
- For fallible iteration use `iter.Seq2[T, error]`.
- Start concrete. Go generic for containers and collection algorithms, not for business logic.
