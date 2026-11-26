# Go

From your first `go mod init` to a production service, the way real teams write Go.

## Why Go feels so strict

The first week with Go is often a little annoying:

- An unused import? **Compile error.**
- An unused variable? **Compile error.**
- You want to put the `{` on the next line? **Not allowed.**
- You want classes, inheritance, exceptions, a `while` loop? **There are none.**

This is not an accident. Go was designed at Google from 2007 by Robert Griesemer, Rob Pike and Ken Thompson. They had a very specific problem: **huge codebases, thousands of engineers, slow builds, and code that nobody could read anymore.** A big C++ build could take most of an hour. Every team had its own style. Reading someone else's code was a research project.

So they made a language where the main goal is not "how much can I express?" but:

> **Can a new engineer read this code, understand it, and change it safely, two years from now?**

Almost every rule in Go comes from that one question. When a rule looks strange, ask: _"how does this help the next person who reads the code?"_ You will find the answer almost every time. This chapter always gives you that answer, so the rules stop feeling like rules and start feeling like common sense.

## Go's design ideas, in four lines

| Idea                                                | What it means for you                                                                        |
| --------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| **Simplicity is a feature.** Few keywords, one way. | Less to learn, and every codebase looks familiar. You read code much more than you write it. |
| **Explicit beats clever.**                          | Errors are normal return values. No hidden control flow, no magic. You see what happens.     |
| **Composition, not inheritance.**                   | Small types and small interfaces that you plug together, instead of deep class trees.        |
| **The toolchain is part of the language.**          | `gofmt`, `go test`, `go vet`, modules, profiling: one standard set of tools for everyone.    |

!!! quote "Go proverb"

    _Clear is better than clever._ — Rob Pike

## What you will build

The examples in this chapter are not "hello world". From the middle of the chapter on, they come from one realistic project: an **orders** system for a small web shop. It lives in this repository under `examples/orders/` and is built, vetted, linted and tested in CI, like a real team project.

```text
examples/orders/
├── cmd/ordersd/     ← the HTTP service
├── cmd/orderctl/    ← the command-line client
└── internal/        ← domain, storage, HTTP API, client, config
```

## Reading order

The pages build on each other. Part 1 sets up your workshop. Part 2 is the language itself. Part 3 is how Go thinks about failure and quality. Part 4 is running many things at once. Part 5 puts it all together.

| #                        | Page                                                | You will learn                                                                              |
| ------------------------ | --------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| **Part 1: The workshop** |                                                     |                                                                                             |
| 1                        | [The Go toolchain](toolchain.md)                    | Installing Go, the `go` command, the build cache, cross-compiling, reading docs offline     |
| 2                        | [Modules and dependencies](modules.md)              | `go.mod`, `go.sum`, versions, why Go picks the _minimum_ version, workspaces, private repos |
| 3                        | [Project layout](project-layout.md)                 | Packages, `cmd/`, `internal/`, and how to structure CLIs, services, libraries and monorepos |
| 4                        | [Tooling and code quality](tooling-and-quality.md)  | Formatters, `go vet`, `golangci-lint`, `govulncheck`, `go fix`, editor setup, CI            |
| **Part 2: The language** |                                                     |                                                                                             |
| 5                        | [Basics](basics.md)                                 | Variables, zero values, constants, `for`, `switch`, functions, `defer`                      |
| 6                        | [Types and data](types-and-data.md)                 | Slices and maps under the hood, strings, structs, pointers, value vs pointer semantics      |
| 7                        | [Conventions: the Go way](conventions.md)           | Naming, packages, comments and idioms, and the reason behind each one                       |
| 8                        | [Methods and interfaces](methods-and-interfaces.md) | Receivers, method sets, implicit interfaces, the nil-interface trap, embedding              |
| **Part 3: Robust code**  |                                                     |                                                                                             |
| 9                        | [Errors](errors.md)                                 | Errors as values, wrapping, `errors.Is`/`As`, and an error strategy for a whole service     |
| 10                       | [Generics and iterators](generics.md)               | Type parameters, constraints, `slices`/`maps`, range-over-func, and when _not_ to use them  |
| 11                       | [Testing](testing.md)                               | Table tests, fakes, `httptest`, golden files, benchmarks, fuzzing, the race detector        |
| 12                       | [Standard library essentials](stdlib-essentials.md) | `io`, JSON v2, `time`, `slog`, `embed`, and HTTP clients that don't hang                    |
| **Part 4: Concurrency**  |                                                     |                                                                                             |
| 13                       | [Concurrency](concurrency.md)                       | Goroutines, channels, `select`, `sync`, the memory model, patterns and goroutine leaks      |
| 14                       | [Context and cancellation](context.md)              | Deadlines, cancellation trees, request values, and the `ctx` conventions                    |
| 15                       | [Performance](performance.md)                       | Profiling with `pprof`, benchmarks, allocations, PGO and GC tuning                          |
| **Part 5: Production**   |                                                     |                                                                                             |
| 16                       | [Building a CLI](cli.md)                            | A testable `run()` function, flags, config, exit codes, signals, version info               |
| 17                       | [Building a service](services.md)                   | Routing, middleware, timeouts, graceful shutdown, wiring, health checks, containers         |

## How each page works

The same shape as the [Fundamentals](../fundamentals/index.md):

- **Why care?** A real problem first.
- **The idea.** Plain words, often a picture.
- **Under the hood.** The deep part. This is where understanding comes from.
- **Try it in Go.** Code you can run. Output shown in this book comes from real runs with Go 1.27.
- **Convention boxes.** Every Go convention gets a box with the rule _and_ the reason.
- **Traps.** Mistakes real teams make.
- **Check yourself.** Questions with hidden answers. Try first.

A convention box looks like this:

!!! abstract "Convention: example"

    **Rule:** what Go programmers do.

    **Why:** the reason, so you can decide well in cases the rule does not cover.

!!! tip "Set up once, then experiment"

    Install Go with `mise use go@latest` (see [The Go toolchain](toolchain.md)). For any small example:
    make a folder, run `go mod init demo`, paste the code into `main.go`, and run `go run .`.
    Then break it on purpose. Reading the compiler's error messages is a skill too.
