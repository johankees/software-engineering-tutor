# The Go toolchain

## Why care?

A new colleague clones your repository. They run the tests. Everything is red. After two hours you find the reason: they have Go 1.25, you have Go 1.27, and the code uses a function that only exists in 1.27. Meanwhile CI uses a third version, and the binary in production says `version: dev`, so nobody knows which commit is running.

None of this is about the Go _language_. It is about the **toolchain**: the programs that turn your code into a binary. In Go, the toolchain is simple and strict, and when you understand it, these problems disappear.

## The idea: one tool does everything

In many languages you need a collection of tools: a compiler, a build tool (Maven, Gradle, CMake, webpack), a package manager (npm, pip), a test runner, a formatter, a doc generator. Every team picks a different set.

Go ships **one program**, called `go`, that does all of these jobs:

| Command            | What it does                                                   | Similar to              |
| ------------------ | -------------------------------------------------------------- | ----------------------- |
| `go build`         | Compile packages into a binary                                 | `gcc`, `mvn package`    |
| `go run`           | Compile to a temporary binary and run it                       | `python file.py`        |
| `go test`          | Build and run tests, benchmarks, fuzz tests                    | `pytest`, `jest`, JUnit |
| `go mod`, `go get` | Manage dependencies (see [Modules](modules.md))                | `npm`, `pip`, Maven     |
| `go fmt`           | Format code in the one official style                          | `prettier`, `black`     |
| `go vet`           | Find likely bugs the compiler does not catch                   | a basic linter          |
| `go fix`           | Rewrite old code to modern Go                                  | codemods                |
| `go doc`           | Show documentation, in the terminal or as a local web page     | `man`, Javadoc          |
| `go tool`          | Run lower-level tools (`pprof`, `trace`, `cover`) and your own | `npx`                   |
| `go env`           | Show and set the settings the `go` command uses                |                         |
| `go version`       | Show the Go version, or the version a binary was built with    |                         |

!!! abstract "Convention: use the standard tools"

    **Rule:** build with `go build`, test with `go test`, format with `gofmt`. Wrap them in a
    `Makefile` or `mise` task if you like, but do not replace them with another build system.

    **Why:** every Go engineer in the world already knows these commands. A new team member can
    build and test your project in the first five minutes, without reading a wiki. That is a huge
    saving in a company with hundreds of repositories. A custom build system (or Bazel) is only
    worth it in very large, multi-language monorepos.

## Installing Go the team way

You can download Go from [go.dev/dl](https://go.dev/dl), but in a team you want **everyone to use the same version, automatically**. This repository uses [mise](https://mise.jdx.dev), a tool version manager. A `mise.toml` file in the repo pins the tools:

```toml
[tools]
go = "1.27"
golangci-lint = "2.15"
```

When you `cd` into the folder, mise puts the right versions on your `PATH`. CI uses the same file. Now "works on my machine" means "works on every machine".

Check what you have:

```sh
go version
```

```text
go version go1.27.4 linux/amd64
```

The format is `go<major>.<minor>.<patch> <os>/<arch>`. Go releases a new **minor** version (1.26, 1.27, ...) every six months, in February and August. **Patch** versions (1.27.1, 1.27.2, ...) fix bugs and security issues. The Go team supports the **two newest** minor versions with security fixes.

!!! abstract "Convention: stay on a supported Go version"

    **Rule:** upgrade to each new minor version within a few months, and take every patch release.

    **Why:** Go has the **Go 1 compatibility promise**: code written for Go 1.0 in 2012 still
    compiles today. Upgrades are almost always boring, so there is little reason to wait. And old
    versions stop getting security fixes after one year. "We are stuck on Go 1.19" is a security
    finding in an audit, not a stability choice.

## Where Go keeps things

Run `go env` to see every setting. These are the important ones:

```sh
go env GOROOT GOPATH GOMODCACHE GOCACHE GOOS GOARCH GOPROXY GOTOOLCHAIN
```

```text
/home/linuxbrew/.linuxbrew/Cellar/go/1.27.4/libexec
/home/johan/.local/share/go
/home/johan/.cache/go/mod
/home/johan/.cache/go/build
linux
amd64
https://proxy.golang.org,direct
auto
```

| Variable      | Meaning                                                                              |
| ------------- | ------------------------------------------------------------------------------------ |
| `GOROOT`      | Where Go itself is installed: the compiler and the **standard library** source code. |
| `GOPATH`      | Your personal Go folder. Today mostly used for `GOPATH/bin` (installed tools).       |
| `GOMODCACHE`  | Downloaded dependencies. Read-only. Shared by all your projects.                     |
| `GOCACHE`     | The **build cache**: compiled packages and test results.                             |
| `GOOS/GOARCH` | The target operating system and CPU. Change them to cross-compile.                   |
| `GOPROXY`     | Where dependencies are downloaded from (see [Modules](modules.md)).                  |
| `GOTOOLCHAIN` | Which Go version to use, and whether Go may download a newer one (see below).        |

!!! info "A short history: GOPATH"

    Before 2018, _all_ Go code had to live inside one folder, `$GOPATH/src/github.com/...`.
    There were no versions: you always got the latest commit of every dependency. It was simple,
    and it was terrible for companies, because builds were not reproducible. **Go modules**
    (Go 1.11, default since 1.16) fixed this. Today your code can live anywhere. If an old blog
    post tells you to set `GOPATH` or put code in `$GOPATH/src`, the post is out of date.

!!! tip "The standard library is right there"

    `GOROOT/src` contains the full source of the standard library. When you wonder "how does
    `net/http` really handle timeouts?", open `$(go env GOROOT)/src/net/http/server.go` and read it.
    Go's standard library is famously readable, and it is the best example of good Go code you
    will find.

## Packages, modules and the `./...` pattern

Two words you must not mix up:

- A **package** is one folder of `.go` files that all start with the same `package name` line. It is the unit of compilation and of visibility.
- A **module** is a tree of packages with a `go.mod` file at the top. It is the unit of versioning and of release.

Most `go` commands take **package patterns**:

| Pattern                          | Means                                      |
| -------------------------------- | ------------------------------------------ |
| `.`                              | The package in the current folder          |
| `./internal/order`               | One package, by folder                     |
| `./...`                          | This folder and **every package below it** |
| `github.com/acme/orders/cmd/...` | Every package under that import path       |
| `std`                            | The whole standard library                 |

`go test ./...` and `go vet ./...` are the two commands you will type most in your career.

## Building: one static binary

```sh
go build -o bin/ordersd ./cmd/ordersd
```

The result is **one file**. No runtime to install, no `node_modules`, no JVM, no shared libraries (by default). You copy that file to a server or into an empty container image, and it runs.

Why did Go choose this? Google deploys to huge fleets of machines. With one self-contained binary, a deployment is "copy a file". There is no "the server has the wrong version of libssl" problem. This is also why Go is so popular for containers and CLI tools.

!!! info "cgo and `CGO_ENABLED`"

    Go can call C code with a feature called **cgo**. If cgo is used, the binary links to the C
    library of the system (libc) and is no longer fully static. Some standard packages (`net`,
    `os/user`) use cgo when it is enabled, to use the system DNS resolver.

    For services in containers, teams usually build with `CGO_ENABLED=0`. Then Go uses its own
    pure-Go implementations, and the binary runs on any Linux, even in a `scratch` or distroless
    image. Cross-compiling is also much easier without cgo.

### Under the hood: why Go compiles so fast

Compile speed was one of the original design goals, because slow builds kill productivity in big codebases. Several language rules exist **only** to make builds fast:

1. **Unused imports are a compile error.** Every import costs build time. If the compiler allowed unused imports, big codebases would collect thousands of them.
2. **Import cycles are forbidden.** Packages form a tree without loops (a directed acyclic graph), so they can be compiled in order, in parallel, each one once.
3. **No header files.** When package `b` is compiled, the compiler writes a summary of its exported API into the compiled output. Package `a`, which imports `b`, reads only that summary, not all of `b`'s dependencies. In C++, one `#include` can pull in thousands of lines, again and again.
4. **A simple grammar.** Go can be parsed without knowing types, so the parser is fast.

On top of that, the **build cache** remembers every compiled package. The key is a hash of the source, the compiler version and the flags. Look at the difference on a tiny program:

```sh
time go build -o /dev/null .   # cache already warm
go clean -cache                # empty the cache
time go build -o /dev/null .   # cold: must compile the standard library parts too
```

```text
go build -o /dev/null .  0.16s user 0.11s system 120% cpu 0.221 total
go build -o /dev/null .  13.48s user 2.06s system 372% cpu 4.165 total
```

0.2 seconds warm, 4 seconds cold. In CI, **cache `GOCACHE` and `GOMODCACHE` between runs**. It is the single biggest CI speed-up for Go projects.

`go test` uses the same cache for **test results**. If nothing changed, it prints `(cached)` instead of running the tests again. To force a rerun, use `go test -count=1 ./...`.

!!! warning "You almost never need `go clean -cache`"

    The cache key covers source, flags and compiler version, so a stale result is very unlikely.
    The one known exception: cgo code that links to a system C library you changed. If a build
    looks "stuck on old code", the cause is almost always somewhere else (wrong folder, wrong
    `go.work`, a `replace` line).

### `go run` and `go install`

- `go run .` builds into a temporary folder and runs it. Fine for development. Never use `go run` in production: it needs the source and the toolchain on the server, and it compiles on every start.
- `go install ./cmd/orderctl` builds and puts the binary in `$(go env GOPATH)/bin` (or `GOBIN`). With a version, it installs a tool from anywhere: `go install golang.org/x/tools/cmd/stringer@v0.39.0`. For tools a _project_ needs, use the `tool` directive instead (see [Modules](modules.md#tools-the-tool-directive)).

## Cross-compiling: build for any platform from your laptop

Set `GOOS` and `GOARCH`, and Go compiles for that platform. No extra toolchain needed (as long as cgo is off):

```sh
GOOS=windows GOARCH=arm64 go build -o bin/orders.exe .
GOOS=darwin  GOARCH=arm64 go build -o bin/orders-mac .
file bin/orders.exe bin/orders-mac
```

```text
bin/orders.exe: PE32+ executable (console) Aarch64, for MS Windows, 14 sections
bin/orders-mac: Mach-O 64-bit arm64 executable, flags:<|DYLDLINK|PIE>
```

`go tool dist list` prints all supported targets. With Go 1.27 there are 47. This is how one Linux CI job can release a CLI for Windows, macOS and Linux at the same time.

## Know what is running: version stamping

When something breaks in production, the first question is "which version is running?". Go helps in two ways.

**1. Go stamps build info automatically.** Go records the module versions and the build settings inside the binary. If you build inside a Git repository, it adds the Git commit too. Read it back with `go version -m` (output shortened; this build was outside Git, so there are no `vcs` lines):

```sh
go version -m bin/orders
```

```text
bin/orders: go1.27.4
	path	github.com/acme/orders
	mod	github.com/acme/orders	(devel)
	dep	golang.org/x/sync	v0.25.0	h1:dJnifr0nMV8/SBnBvdo4eSGGnrc3O4FFx6IqvtLcmRs=
	build	-buildmode=exe
	build	-compiler=gc
	build	CGO_ENABLED=1
	build	GOARCH=amd64
	build	GOOS=linux
	build	GOAMD64=v1
```

This is a real security tool: scanners like `govulncheck` read this list to find vulnerable dependencies inside binaries that are already deployed.

Your program can read the same data at run time with `runtime/debug.ReadBuildInfo`.

**2. You inject your release version with `-ldflags -X`.** The linker can set a string variable at build time:

```go
package main

import (
	"fmt"
	"runtime/debug"
)

// version is set at build time with -ldflags "-X main.version=v1.4.0".
var version = "dev"

func main() {
	fmt.Println("version:", version)
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" || s.Key == "vcs.modified" || s.Key == "vcs.time" {
				fmt.Printf("%s=%s\n", s.Key, s.Value)
			}
		}
	}
}
```

```sh
go build -trimpath -ldflags "-s -w -X main.version=v1.4.0" -o hello .
./hello
```

```text
version: v1.4.0
vcs.revision=9dc7aba877f6207180470acf6bcb7822d56066f6
vcs.time=2026-11-26T07:23:39Z
vcs.modified=false
```

`vcs.modified=false` tells you the binary was built from a clean commit, not from someone's laptop with local changes. You will use this in the [CLI](cli.md) (`orderctl version`) and in the [service](services.md) (a `/version` endpoint and a log line at startup).

The flags in that build command:

| Flag               | Effect                                                                                         |
| ------------------ | ---------------------------------------------------------------------------------------------- |
| `-trimpath`        | Removes your local folder paths (`/home/johan/...`) from the binary. Reproducible and private. |
| `-ldflags "-s -w"` | Drops the symbol table and DWARF debug info. Here: 2.4 MB became 1.6 MB.                       |
| `-X main.version=` | Sets the string variable `version` in package `main`.                                          |

!!! warning "`-s -w` is a trade-off"

    Without DWARF info, debuggers like Delve cannot show variables, and some profilers give less
    detail. Panic stack traces still work. Many teams keep debug info for services (size does not
    matter much in a container) and strip it only for CLI downloads.

!!! abstract "Convention: release builds are reproducible and stamped"

    **Rule:** release binaries are built by CI, from a clean commit, with `-trimpath`, a stamped
    version, and (for services) `CGO_ENABLED=0`.

    **Why:** the same commit should always produce the same binary, so you can prove what is
    running. A binary built on a laptop may contain uncommitted code. `-trimpath` also stops
    leaking user names and folder layouts from build machines.

## `GOTOOLCHAIN`: Go manages its own version

Back to the opening problem: a colleague has an older Go. Since Go 1.21, **the `go.mod` file says which Go version the module needs**, and the `go` command acts on it:

```text
module github.com/acme/hello

go 1.26.0

toolchain go1.27.4
```

- The `go` line is the **minimum** Go version needed to build this module. It also selects language features: in a module with `go 1.21`, you cannot use a 1.22 feature like `for i := range 10`.
- The optional `toolchain` line is the **preferred** toolchain for people working _in this module_.

With the default `GOTOOLCHAIN=auto`, when your installed Go is too old, the `go` command **downloads the right version and runs it**, like this:

```sh
GOTOOLCHAIN=go1.26.3 go version
```

```text
go: downloading go1.26.3 (linux/amd64)
go version go1.26.3 linux/amd64
```

The downloaded toolchain is itself a module, verified with checksums like any dependency. With `GOTOOLCHAIN=local`, Go refuses instead:

```text
go: go.mod requires go >= 1.29.0 (running go 1.27.4; GOTOOLCHAIN=local)
```

Many companies set `GOTOOLCHAIN=local` in CI, so builds only use the Go version they installed and audited, and never download a toolchain from the internet in the middle of a build.

!!! abstract "Convention: the `go` line is a promise to your users"

    **Rule:** for an **application**, set the `go` line to the version you build with. For a
    **library**, set it to the _oldest_ Go version you support (often the oldest supported one),
    and do not raise it without a reason.

    **Why:** the `go` line forces everyone who depends on your module to use at least that
    version. A library that needs `go 1.27` forces every user to upgrade. An application has no
    users like that, so it can just use the newest features.

## Reading documentation offline: `go doc`

All Go documentation is written in the source code as comments (see [Conventions](conventions.md)). `go doc` reads it straight from there, so it always matches the version you use:

```sh
go doc strings.CutLast
```

```text
package strings // import "strings"

func CutLast(s, sep string) (before, after string, found bool)
    CutLast slices s around the last instance of sep, returning the text
    before and after sep. The found result reports whether sep appears in s.
    If sep does not appear in s, CutLast returns s, "", false.
```

Useful forms:

- `go doc net/http.Server` or `go doc http Server.Shutdown`: one type or method.
- `go doc -short errors`: a one-line summary of everything in a package.
- `go doc -all ./internal/order`: everything in _your_ package. Great for review: "does our API read well?"
- `go doc -http`: serves your package docs as a website on your machine, like [pkg.go.dev](https://pkg.go.dev).

!!! tip "Trust `go doc` over blog posts"

    Go moves forward every six months. A blog post from 2021 may tell you to use `ioutil.ReadAll`
    (deprecated since 1.16) or `sort.Slice` (today: `slices.SortFunc`). `go doc` shows the
    current API and says when something is deprecated.

## Settings: `go env -w` and why to avoid it in teams

`go env -w GOPRIVATE=github.com/acme/*` writes a setting into a **per-user** config file (`go env GOENV` shows where). It works, but it is invisible: nothing in the repository tells a new colleague or CI that the setting exists.

!!! abstract "Convention: project settings live in the project"

    **Rule:** put settings a project needs (`GOPRIVATE`, `GOFLAGS`, `CGO_ENABLED`) in a checked-in
    file: `mise.toml` `[env]`, a `Makefile`, or the CI config. Use `go env -w` only for personal
    machine settings.

    **Why:** a build should not depend on hidden state on one laptop. If it is not in Git, it does
    not exist for your teammates.

## Traps

- **Running `go build` in the wrong folder.** `go build` with no arguments builds the package in the current folder. In a repo with `cmd/ordersd`, run `go build ./cmd/ordersd` or `go build ./...` (which checks everything compiles but writes no binaries for multiple packages).
- **`go install` vs. `go get`.** `go get` changes your `go.mod` (adds a dependency). `go install pkg@version` installs a binary and does **not** touch `go.mod`. Using `go get` to install a tool pollutes your dependencies.
- **Forgetting `CGO_ENABLED=0` in a Docker build,** then running the binary in a `scratch` image: `exec /app: no such file or directory`. The file is there; the missing file is the C dynamic loader it was linked against.
- **A `go.work` file you forgot about** in a parent folder makes builds use local copies of modules. Check with `go env GOWORK`. See [Modules](modules.md#workspaces-gowork).
- **Not caching in CI.** A cold Go build of a large service can take minutes. A warm one takes seconds.

## Check yourself

??? question "1. Name two language rules that exist mainly to make compilation fast."

    Unused imports are compile errors, and import cycles are forbidden. Also: no header files
    (the compiler reads a compact export summary of each imported package), and a grammar that
    can be parsed without type information.

??? question "2. Your colleague has Go 1.25 installed. Your go.mod says `go 1.27.0`. What happens when they run `go test ./...`?"

    With the default `GOTOOLCHAIN=auto`, the `go` command downloads Go 1.27.0 (verified by
    checksum) and runs the tests with it. With `GOTOOLCHAIN=local`, it stops with
    "go.mod requires go >= 1.27.0".

??? question "3. Production logs show a strange bug. How do you find the exact commit of the running binary?"

    `go version -m ./ordersd` (or `debug.ReadBuildInfo()` in a `/version` endpoint) shows
    `vcs.revision` and `vcs.modified`. If you also inject `-X main.version=...` in CI, the release
    tag is there too.

??? question "4. Why do services usually build with `CGO_ENABLED=0`?"

    The binary is then fully static: it does not need the system C library, so it runs in a
    minimal (`scratch` or distroless) image on any Linux, and cross-compiling just works.

??? question "5. Why should a library keep its `go` line low, while an application can use the newest version?"

    The `go` line is a minimum that every dependent module must meet. A high value in a library
    forces all users to upgrade Go. An application is not imported by anyone, so the only users
    are your own team and CI.

## Recap

- One `go` command builds, tests, formats, vets, documents and manages dependencies. Use it; every Go engineer knows it.
- Pin the Go version for the team (`mise.toml`, the `go` and `toolchain` lines). Stay on a supported release.
- Go builds one static binary, very fast, thanks to language rules (no unused imports, no cycles) and the build cache.
- Cross-compile with `GOOS`/`GOARCH`. Stamp releases with `-trimpath` and `-ldflags -X`, and read build info with `go version -m`.
- `go doc` is the source of truth for APIs.

Next: [Modules and dependencies](modules.md), where we look at `go.mod` and `go.sum` in depth.
