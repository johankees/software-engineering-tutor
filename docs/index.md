---
title: Software Engineering Tutor
template: home.html
hide:
  - navigation
  - toc
---

## What you will learn

Each chapter starts with how a thing works inside, then shows how to use it well in real projects. Read them in order, or start with the chapter you need today.

<!-- Python-Markdown needs four-space list indents inside the cards grid. -->

<!-- prettier-ignore-start -->

<div class="grid cards" markdown>

- :lucide-cpu:{ .lg } [Fundamentals](fundamentals/index.md)

    ***

    How a computer works. CPU, registers, stack, heap, memory and encodings.

- :lucide-square-terminal:{ .lg } [Toolbox](toolbox/index.md)

    ***

    The tools around the code. Terminal, shell, tmux, Neovim, mise and dotfiles.

- :lucide-git-branch:{ .lg } [Git](git/index.md)

    ***

    How Git works internally. Objects, refs, the index, packfiles, merging and recovery.

- :lucide-code-xml:{ .lg } [Go](go/index.md)

    ***

    From basic to expert, with an enterprise focus. Layout, testing, tooling, CLIs and services.

- :lucide-server:{ .lg } [Linux](linux/index.md)

    ***

    The kernel your services run on. System calls, file descriptors, signals, cgroups, systemd.

- :lucide-network:{ .lg } [Networking](networking/index.md)

    ***

    TCP, DNS, TLS and HTTP, one layer at a time, with real packets.

- :lucide-container:{ .lg } [Containers](containers/index.md)

    ***

    Images as layers, containers as isolated processes, and how to build good images.

- :lucide-ship:{ .lg } [Kubernetes](kubernetes/index.md)

    ***

    Desired state and reconciliation loops, from pods to writing an operator in Go.

- :lucide-database:{ .lg } [Databases](databases/index.md)

    ***

    Pages, B-trees, the WAL, transactions, isolation, migrations, and Go with Postgres.

- :lucide-waypoints:{ .lg } [Distributed systems](distributed-systems/index.md)

    ***

    Failures, clocks, consistency, retries, idempotency, messaging, outbox and Raft.

- :lucide-activity:{ .lg } [Observability](observability/index.md)

    ***

    Logs, metrics, traces and profiles, connected, and alerts based on SLOs.

- :lucide-rocket:{ .lg } [CI/CD and delivery](delivery/index.md)

    ***

    Fast pipelines, reproducible builds, feature flags, canaries, GitOps and infrastructure as code.

- :lucide-shield-check:{ .lg } [Security](security/index.md)

    ***

    Threat models, cryptography, authentication and authorization, secrets and the supply chain.

- :lucide-package-check:{ .lg } [Secure delivery](secure-delivery/index.md)

    ***

    From signed commit to verified deploy. Rulesets, workflow security, dependencies, Sigstore and SLSA.

- :lucide-drafting-compass:{ .lg } [Architecture](architecture/index.md)

    ***

    Trade-offs, API design, gRPC, boundaries, domain modeling and decision records.

- :lucide-binary:{ .lg } [Algorithms](algorithms/index.md)

    ***

    The data structures inside your tools, and how to reason about cost.

- :lucide-sparkles:{ .lg } [AI engineering](ai-engineering/index.md)

    ***

    How LLMs and coding agents work, and how to use them without lowering the bar.

</div>

<!-- prettier-ignore-end -->

## How this site is written

<div class="grid tx-principles" markdown>

<div markdown>

### Plain English

Short sentences. Simple words. No jargon without an explanation.

</div>

<div markdown>

### Professional and precise

Diagrams where they help, real examples, no filler.

</div>

<div markdown>

### Deep

Every page explains _why_ something works, not only how to use it.

</div>

<div markdown>

### Real

Examples target the current Go release, and command output comes from real runs.

</div>

</div>
