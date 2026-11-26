# mise

## Why care?

This repository's `mise.toml` says `go = "latest"`. CI runs the checks and they pass. On the author's laptop, in the same folder:

```text
$ go version
go version go1.27.4 linux/amd64
$ mise exec -- go version
go version go1.26.5 linux/amd64
```

Two Go versions, in the same folder, on the same machine, a second apart. Which one runs your tests depends on _how_ you call it. And CI, which installs fresh, gets a third answer that may differ from both. This page explains why, and how to make it boring and predictable again.

## The idea: a menu per restaurant

Homebrew is your kitchen at home: one version of each tool, for everything. That's fine for `ripgrep`. It's not fine for Go, Python or Node, where project A needs one version and project B another.

**mise** gives every project its own menu. A `mise.toml` in the project says "here we use Go 1.26 and Python 3.13". When you work in that folder, `go` means that version. Outside, it means something else. mise manages three things:

1. **Tools**: install versions side by side under `~/.local/share/mise/installs/`, and put the right one on `PATH`.
2. **Environment variables**: an `[env]` section, per project (like direnv).
3. **Tasks**: named commands with dependencies (like `make`), in the same file.

This repository's file:

```toml
[tools]
python = "3.13"
"pipx:zensical" = "latest"
go = "latest"
golangci-lint = "latest"
actionlint = "latest"
bun = "latest"

[settings]
pipx.uvx = true

[tasks.check]
description = "Run all checks (CI entry point)"
depends = ["fmt:check", "lint", "build", "go:check"]
```

(Tasks shortened; we look at all of them below.)

## Under the hood: three ways to get the right `go`

The core problem: when you type `go`, the shell searches `PATH` (see [Terminal and shell](terminal-and-shell.md#path-how-a-name-becomes-a-program)). It knows nothing about projects. mise has three ways to influence that search.

```mermaid
flowchart TB
    subgraph A ["1. activate (interactive shells)"]
        A1[prompt or cd] --> A2["hook runs: mise hook-env"] --> A3["PATH rewritten:<br/>.../installs/go/1.26.5/bin first"]
    end
    subgraph B ["2. shims (scripts, IDEs)"]
        B1["~/.local/share/mise/shims/go"] -->|symlink to mise| B2["mise reads config for cwd,<br/>execs the real go"]
    end
    subgraph C ["3. exec / run (explicit)"]
        C1["mise exec -- go test"] --> C2["environment for this one command"]
    end
```

### 1. `mise activate`: a hook before every prompt

`eval "$(mise activate zsh)"` in `.zshrc` prints a small zsh script and runs it. The important lines of that script, from a real run:

```sh
_mise_hook() {
  eval "$(/home/linuxbrew/.linuxbrew/bin/mise hook-env --shell-pid $$ -s zsh "$@")";
}
...
add-zsh-hook precmd _mise_hook_precmd
add-zsh-hook chpwd _mise_hook_chpwd

_mise_hook --force
```

zsh calls `precmd` hooks before each prompt and `chpwd` hooks after each `cd`. Each time, `mise hook-env` reads the config files for the current folder and prints `export PATH=...` and other variables, and the shell `eval`s them. mise remembers the original `PATH` (in `__MISE_ORIG_PATH`), so it can remove old entries when you leave a project. There is also a fast path: if nothing changed, the hook returns early, so the cost per prompt is small.

What it looks like, in a clean zsh (`zsh -f`, no start-up files) in this repository:

```text
$ zsh -fc 'eval "$(mise activate zsh)"; for d in ${path[1,4]}; do print -r -- "${d/#$HOME/~}"; done; go version'
~/.local/share/mise/installs/python/3.13/bin
~/.local/share/mise/installs/pipx-zensical/latest/bin
~/.local/share/mise/installs/go/1.26.5/bin
~/.local/share/mise/installs/golangci-lint/latest/golangci-lint-2.15.0-linux-amd64
go version go1.26.5 linux/amd64
```

The real install folders are put at the **front** of `PATH`. No wrapper, no extra process: `go` is the real binary. mise also sets tool variables; for Go, `mise env` shows `GOROOT` and `GOBIN` pointing into the 1.26.5 install, so `go install` puts binaries next to that Go version.

### 2. Shims: tiny stand-ins

A hook only works in an interactive shell. Your IDE, a cron job or a `#!/bin/sh` script never runs `precmd`. For them, mise has **shims**: one file per tool name in `~/.local/share/mise/shims/`. Look at them:

```text
$ cd ~/.local/share/mise/shims && stat -c '%N' go gofmt
'go' -> '/home/linuxbrew/.linuxbrew/bin/mise'
'gofmt' -> '/home/linuxbrew/.linuxbrew/bin/mise'
```

Every shim is a **symlink to mise itself**. When you run `go`, mise starts, sees `argv[0]` is `go`, reads the config for the current folder, and runs the right real binary. (The same multi-call trick as the `dot` CLI in [tmux](tmux.md#try-it-script-a-session-like-tmux-session-start).) The answer depends on the folder:

```text
$ cd ~ && ~/.local/share/mise/shims/go version
go version go1.27.4 linux/amd64
$ cd ~/.cache/agent/worktrees/software-engineering-tutor-toolbox-and-git && ~/.local/share/mise/shims/go version
go version go1.26.5 linux/amd64
```

In `$HOME` there is no config for Go, so the shim falls through to the next `go` on `PATH` (Homebrew's 1.27.4). In the project it picks 1.26.5. The cost: every call starts mise first, and, as `mise activate --help` warns, "shims do not support every feature of `mise activate`".

### 3. `mise exec` and `mise run`: explicit

`mise exec -- <command>` (short: `mise x`) runs one command with the project's tools and env, without touching your shell. `mise run <task>` does the same for tasks. Nothing to activate, nothing to forget. That's why the help text of `mise activate` itself says:

> Use `mise exec -- command` for scripts and CI that do not need interactive hooks.

| Way          | Works in                    | Cost                | Use for                       |
| ------------ | --------------------------- | ------------------- | ----------------------------- |
| `activate`   | interactive shells          | a hook per prompt   | your terminal                 |
| shims        | everything that uses `PATH` | mise start per call | IDEs, scripts, GUI apps       |
| `exec`/`run` | anywhere                    | explicit command    | CI, Makefiles, one-off checks |

## Diagnose this setup: mise is not active

Back to the mystery. `mise doctor` tells you the state:

```text
$ mise doctor
version: 2026.11.14 linux-x64 (2026-11-24)
activated: no
shims_on_path: no
self_update_available: no
...
config_files:
  ~/.cache/agent/worktrees/software-engineering-tutor-toolbox-and-git/mise.toml
...
toolset:
  aqua:golangci/golangci-lint@2.15.0
  aqua:rhysd/actionlint@1.7.12
  core:bun@1.4.5
  core:go@1.26.5
  core:python@3.13.18
  pipx:zensical@0.1.3
...
1 problem found:

1. mise is not activated, run mise help activate or
   read documentation at https://mise.jdx.dev for activation instructions.
   Alternatively, add the shims directory ~/.local/share/mise/shims to PATH.
   Using the shims directory is preferred for non-interactive setups.
```

`activated: no` and `shims_on_path: no`. mise is installed by Homebrew, but the dotfiles' `.zshrc` never activates it. So in a normal shell, `go` is simply Homebrew's `go`, and the project's `mise.toml` is ignored. Only `mise run` and `mise exec` use it. You can see this in what the shell finds:

```text
$ command -v go python3 bun actionlint
/home/linuxbrew/.linuxbrew/bin/go
/home/linuxbrew/.linuxbrew/bin/python3
/home/linuxbrew/.linuxbrew/bin/bun
/home/linuxbrew/.linuxbrew/bin/actionlint
$ mise which go | sed "s|^$HOME|~|"
~/.local/share/mise/installs/go/1.26.5/bin/go
```

The fix has two parts:

1. In `.zshrc`, after `brew shellenv` (so `mise` is on `PATH`):

   ```sh
   eval "$(mise activate zsh)"
   ```

2. For things that don't run `.zshrc` (editors started from a GUI, scripts), put the shims on `PATH` in `.zshenv` or the login environment: `export PATH="$XDG_DATA_HOME/mise/shims:$PATH"`. Activation in the interactive shell then puts the real folders in front of the shims.

The dotfiles now do part 1. The same check in the project folder, first with the old `.zshrc`, then with the fixed one (`zsh -i -c 'go version; command -v zensical'`):

```text
go version go1.27.4 linux/amd64
zensical: not on PATH
```

```text
go version go1.26.5 linux/amd64
~/.local/share/mise/installs/pipx-zensical/latest/bin/zensical
```

Same folder, same `mise.toml`. Only activation changed, and now the shell uses the project's tools.

### The second half of the mystery: what "latest" means

There's another surprise in the output above. `latest` gave Go **1.26.5**, but:

```text
$ mise latest go
1.27.4
$ mise outdated
go  latest  1.26.5  1.27.4 ~/.cache/agent/worktrees/software-engineering-tutor-toolbox-and-git/mise.toml
```

For `latest`, mise uses the newest version that is **already installed**, until you upgrade (`mise upgrade go`). On a fresh machine, like a CI runner, nothing is installed yet, so CI gets 1.27.4. Your laptop and CI run different Go versions from the same file, and neither is wrong.

The cure is to be explicit: pin a version (`go = "1.26"`) or use a **lock file**. `mise lock` writes `mise.lock` with the exact version, checksums and download URLs per platform. Commit it, and `--locked` (or the `locked` setting) makes installs use only the locked URLs. It's the same idea as `go.sum` and `lazy-lock.json`.

## Config files and trust

mise reads config files from the current folder up to `/`, plus a global one. Closer files win. The common names:

| File                               | Who writes it | Committed?               |
| ---------------------------------- | ------------- | ------------------------ |
| `mise.toml` or `.config/mise.toml` | the project   | yes                      |
| `mise.local.toml`                  | you           | no (personal overrides)  |
| `~/.config/mise/config.toml`       | you           | in your dotfiles, if any |

`mise config ls` shows what is really loaded, lowest precedence first:

```text
$ mise config ls
~/.cache/agent/worktrees/software-engineering-tutor-toolbox-and-git/mise.toml  python, pipx:zensical, go, golangci-lint, actionlint, bun
```

(This setup has no global mise config: `~/.config/mise` does not exist.)

**Trust.** A `mise.toml` can set environment variables, run templates and define tasks. Loading it from a repository you just cloned would be like running its code. So mise asks you to **trust** a config file before it loads the risky parts. From `mise trust --help` in this version:

- A file with only plain `[tools]` versions and simple `[tasks]` does not need trust.
- Commands that run project code anyway (`mise run`, `mise install`, `mise exec`) trust their active config automatically.
- Trusting a file in a repository's main checkout also trusts the same path in its git worktrees.

That last point is why this worktree was trusted without asking:

```text
$ mise trust --show
~/.cache/agent/worktrees/software-engineering-tutor-toolbox-and-git: trusted
```

## Backends: where tools come from

Each tool comes from a **backend**. `mise registry` shows which backend a short name maps to:

```text
$ mise registry go
core:go
$ mise registry golangci-lint
aqua:golangci/golangci-lint asdf:hypnoglow/asdf-golangci-lint
$ mise registry python
core:python
```

| Backend               | How it installs                                            | Example                                |
| --------------------- | ---------------------------------------------------------- | -------------------------------------- |
| `core`                | Built into mise, written for that language                 | `go`, `python`, `bun`                  |
| `aqua`                | The aqua registry: prebuilt binaries, with checksum checks | `golangci-lint`, `actionlint`          |
| `github` / `ubi`      | Download a release asset from a GitHub repository          | `github:owner/tool`                    |
| `go`                  | `go install` a module                                      | `go:golang.org/x/vuln/cmd/govulncheck` |
| `npm`, `cargo`, `gem` | The language's own package manager                         | `npm:prettier`                         |
| `pipx` / `pypi`       | A Python CLI in its own virtual env (here via `uvx`)       | `pipx:zensical`                        |
| `asdf`, `vfox`        | Plugins from those older ecosystems                        | fallback                               |

Note: this repository writes `pipx:zensical` and `pipx.uvx = true`, but in this mise version `mise doctor` lists the backend as `pypi` and reports the setting as `pypi.uvx`. The old name still works. Prefer `aqua` and `core` when you can: they download a prebuilt, verified binary instead of building anything.

## Tasks: this repository as the example

```text
$ mise tasks
build      Build the site into ./site
check      Run all checks (CI entry point)
clean      Remove build output
fmt        Format Markdown, YAML and Go code
fmt:check  Check formatting
go:check   Vet, lint and race-test the Go reference project
install    Install lint and format tools with bun
lint       Lint Markdown and workflows
serve      Serve the site with live reload
$ mise tasks deps check
check
├── go:check
├── build
├── lint
│   └── install
└── fmt:check
    └── install
```

- **`depends`** builds a graph. `check` needs four tasks; two of them need `install`. mise runs `install` **once**, and runs independent tasks **in parallel** (`--jobs`).
- **`run` as an array** runs the commands one after another, and stops at the first failure.
- **`dir`** sets the working folder:

```text
$ mise tasks info go:check
Task: go:check
Description: Vet, lint and race-test the Go reference project
Source: ~/.cache/agent/worktrees/software-engineering-tutor-toolbox-and-git/mise.toml
Directory: examples/orders

Run:
  test -z "$(gofmt -l .)"
  go vet ./...
  golangci-lint run ./...
  go test -race -count=1 ./...
```

Every task runs with the project's tools on `PATH`, activated or not. That's why `mise run check` gives the same answer in your shell and in CI.

Two more task features:

- **File tasks**: executable scripts in a `mise-tasks/` folder become tasks too. Good when a task is longer than one line.
- **`sources` and `outputs`**: "a task with `sources` and `outputs` is skipped while its outputs are up to date; `--force` runs it anyway" (from `mise run --help`). Like `make`, but with globs.

## mise in CI

This repository's whole CI job is three steps:

```yaml
steps:
  - uses: actions/checkout@v5
  - uses: jdx/mise-action@v3
  - run: mise run check
```

`jdx/mise-action` installs mise and the tools from `mise.toml`. Then CI runs **the same task you run locally**. No separate list of setup steps that drifts from the README. The only gap is the one above: without a pin or lock file, "latest" in CI is newer than "latest" on your laptop.

## mise next to Homebrew, fnm, direnv and asdf

| Tool         | Scope                          | Overlap with mise                                        |
| ------------ | ------------------------------ | -------------------------------------------------------- |
| **Homebrew** | System-wide, one version       | Keep it for CLI tools (`rg`, `tmux`) and for mise itself |
| **fnm**      | Node only, per folder          | Full overlap: mise manages Node too                      |
| **direnv**   | Env vars per folder (`.envrc`) | mise's `[env]` covers the common cases                   |
| **asdf**     | Tools per folder, via shims    | mise started as a faster asdf; reads `.tool-versions`    |

This setup activates **fnm** and **direnv** in `.zshrc`, but not mise, even though mise has Node versions installed too (`mise ls` shows `node 24.18.0` and `26.13.1`). Two managers for one tool means two answers to "which `node`?". Pick one per tool. If mise is activated, fnm and most direnv uses can go.

## Traps

- **"latest" is not reproducible.** It means "newest installed" locally and "newest released" on a fresh machine. Pin, or `mise lock`.
- **Installed but not activated.** `mise doctor` says `activated: no`. Everything still _seems_ to work, because Homebrew has a `go` too. You only notice when the versions differ. Run `mise doctor` on every new machine.
- **Hook order.** `mise activate` must run after `mise` is on `PATH` (after `brew shellenv`). Other hooks that also rewrite `PATH` on each prompt (fnm with `--use-on-cd`) fight over the order.
- **Shims and `rehash`.** When you install a tool with a new name, a new shim file appears. zsh may have hashed the old location of that name (or "not found") and needs a `rehash`.
- **Untrusted configs in CI.** `mise run` trusts its config automatically. For other commands, the help text says that without trust "mise prompts, skips the file, or fails with an untrusted-config error when it cannot prompt". In CI there is nobody to answer the prompt.

## Check yourself

??? question "1. Your IDE runs `go test` and gets a different Go version than your terminal. Why, and what are two fixes?"

    The IDE doesn't run `.zshrc`, so `mise activate`'s hook never runs. Put
    `~/.local/share/mise/shims` on the IDE's `PATH`, or configure the IDE to run `mise exec -- go test`.

??? question "2. How does a shim know which version to run?"

    The shim is a symlink to the `mise` binary. mise sees the name it was called with (`argv[0]`),
    reads the config files for the current folder, and execs that tool's real binary.

??? question "3. `go = "latest"`. Your laptop has Go 1.26.5 installed, the newest release is 1.27.4. Which version does each machine use?"

    The laptop uses 1.26.5 (newest installed) until `mise upgrade`. A fresh CI runner installs and uses 1.27.4.
    Pin a version or commit `mise.lock`.

??? question "4. Why does mise ask you to trust a `mise.toml`?"

    It can set environment variables, use templates and define tasks: running it is like running the
    repository's code. Trust is your decision to allow that for this file.

??? question "5. What does `mise run check` do that `make check` with Homebrew tools doesn't?"

    It runs with the exact tool versions from `mise.toml` on `PATH`, installs missing ones, and runs
    the dependency graph in parallel. The same command gives the same tools locally and in CI.

## Recap

- mise manages per-project tools, env vars and tasks from `mise.toml`.
- `activate` rewrites `PATH` in a `precmd`/`chpwd` hook; shims are symlinks to mise that pick the version per call; `exec`/`run` are explicit.
- This setup did **not activate** mise: `mise doctor` showed it. The fix is `eval "$(mise activate zsh)"` in `.zshrc`, plus shims for non-interactive programs.
- `latest` means "newest installed". Pin versions or use `mise lock` for reproducible builds.
- Config files merge from the folder upwards; trust protects you from running a stranger's config.
- Tasks with `depends`, `dir` and `run` arrays make `mise run check` the single entry point, locally and in CI.

Next: [Dotfiles and everyday CLI tools](dotfiles.md), the repository that holds all of this config.
