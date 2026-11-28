# Software Engineering Tutor

A personal training site about software engineering, built with [Zensical](https://zensical.org).

It covers:

- **Fundamentals**: registers, stack, heap, memory, encodings, and more.
- **Toolbox**: terminal, zsh, tmux, Neovim, mise and dotfiles, under the hood.
- **Git**: how Git works internally (objects, refs, index, packfiles, merging, remotes, recovery).
- **Go**: from basic to expert, with an enterprise focus (project layout, tooling, testing, CLIs, services).
- **Linux**: system calls, file descriptors, filesystems, signals, namespaces, cgroups, systemd, debugging.
- **Networking**: packets, TCP, DNS, TLS, HTTP/1.1 to HTTP/3, load balancers, debugging.
- **Containers**: images and layers, building images, runtimes, networking, security.
- **Kubernetes**: from the API server and reconciliation to storage, scheduling, packaging and operators.
- **Databases**: PostgreSQL storage, indexes, transactions, MVCC, query planning, migrations, Go and SQL, replication.
- **Distributed systems**: failures, time, consistency, retries, messaging, outbox and sagas, Raft, caching.
- **Observability**: logs, metrics, traces, SLOs and alerting, profiling.
- **CI/CD and delivery**: pipelines, reproducible builds, trunk-based development, feature flags, deployments, GitOps, IaC.
- **Security**: threat modeling, cryptography, authentication, authorization, secrets, supply chain, web and API security.
- **Secure delivery**: GitHub repository settings, signed commits and tags, GitHub Actions security, third-party dependencies, artifact signing and attestations, a secure pipeline from commit to deploy.
- **Architecture**: trade-offs, HTTP and gRPC APIs, evolving APIs, boundaries, domain modeling, decision records.
- **Algorithms**: complexity, hashing, trees, heaps, graphs, sorting, probabilistic structures.
- **AI engineering**: how LLMs work, prompting, coding agents, reviewing AI-written code, building with LLMs in Go.

Style: professional, in plain and simple English, with depth: every page explains why, not only how.

## Getting started

Install [mise](https://mise.jdx.dev), then:

```sh
mise install       # install all tools (incl. bun)
mise run install   # install prettier + markdownlint with bun
mise run serve     # live preview at http://localhost:8000
```

## Tasks

Every task runs with `mise run <task>`. The `Makefile` wraps each one, so `make <task>` works too. Run `make help` to list them.

| Task           | What it does                                                           |
| -------------- | ---------------------------------------------------------------------- |
| `install`      | Install lint/format tools with bun                                     |
| `serve`        | Serve the site with live reload                                        |
| `build`        | Build the static site into `site/`                                     |
| `fmt`          | Format Markdown, YAML and Go code                                      |
| `fmt:check`    | Check formatting (`make fmt-check`)                                    |
| `lint`         | Lint Markdown and GitHub workflows (`actionlint`, `zizmor`)            |
| `make:check`   | Check every mise task has a Makefile target (`make make-check`)        |
| `go:check`     | Vet, lint, race-test, `go mod verify`, `govulncheck` (`make go-check`) |
| `secrets`      | Scan the whole Git history for secrets with `gitleaks`                 |
| `pre-commit`   | Scan staged changes for secrets (use as a Git pre-commit hook)         |
| `github:apply` | Apply the GitHub settings and rulesets (`make github-apply`)           |
| `github:check` | Compare GitHub settings with the repository (`make github-check`)      |
| `check`        | Format, lint, build, Go checks and secret scan (CI)                    |
| `pr`           | All checks, then `./commit.sh` if present and executable               |
| `clean`        | Remove build output                                                    |

## Contributing and security

Changes reach `main` only through a pull request: every commit signed, all checks green, squash merge. The repository follows its own [Secure delivery](docs/secure-delivery/index.md) chapter; see `.github/rulesets/`, `scripts/github-settings.sh` and the workflows. Install the secret-scanning hook once with `mise generate git-pre-commit --write --task pre-commit`.

Report security problems privately, as described in [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
