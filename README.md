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

| Task               | What it does                       |
| ------------------ | ---------------------------------- |
| `mise run install` | Install lint/format tools with bun |
| `mise run serve`   | Serve the site with live reload    |
| `mise run build`   | Build the static site into `site/` |
| `mise run fmt`     | Format Markdown, YAML and Go code  |
| `mise run lint`    | Lint Markdown and GitHub workflows |
| `mise run check`   | Format check + lint + build (CI)   |
| `mise run clean`   | Remove build output                |

## License

[MIT](LICENSE)
