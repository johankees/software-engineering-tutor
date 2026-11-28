# CI/CD and delivery

How a commit becomes running code in production: safely, often, and without heroes.

## Why go deeper?

Teams that ship small changes many times a day have fewer outages, not more. But getting there is hard:

- CI takes 40 minutes, so people batch changes, so each deploy is bigger and riskier.
- "It built on my machine" and the CI artifact are different binaries.
- A long-lived feature branch takes a week to merge.
- A bad deploy reaches every user at once, and rollback means "revert and wait for CI".
- Nobody knows what is running in production, or who changed the firewall rule.

This chapter is about the machinery that makes small, safe changes cheap: fast pipelines, reproducible artifacts, gradual rollouts, and infrastructure described in code.

## The big picture

```mermaid
flowchart LR
    C[Commit on main] --> CI[CI: lint, test, build]
    CI --> ART[(Signed image + SBOM)]
    ART --> GIT[Config repo: new image digest]
    GIT -->|GitOps sync| ST[Staging]
    ST -->|checks pass| CAN[Canary 5%]
    CAN -->|SLOs OK| PROD[100%]
    CAN -->|SLOs bad| RB[Automatic rollback]
```

1. **Build once.** The same artifact, identified by its digest, goes to every environment.
2. **Git is the source of truth** for code _and_ for what runs where.
3. **Release gradually.** Let real traffic and [SLOs](../observability/index.md) decide if a change is good.
4. **Separate deploy from release.** Feature flags let you ship code that is turned off.

## Reading order

Each page builds on the one before it. The examples ran on WSL2 (Ubuntu 24.04, kernel 5.15) with Docker 29.1 and Go 1.27.4, plus actionlint 1.7.12, Git Town 24.2.0, kind v0.33.0, kubectl v1.37.3, Flux 2.10.1 and OpenTofu v1.13.10.

| #   | Page                                                  | You will learn                                                                          |
| --- | ----------------------------------------------------- | --------------------------------------------------------------------------------------- |
| 1   | [CI pipelines](ci-pipelines.md)                       | How GitHub Actions runs a job, caching, matrices, fast feedback, keeping CI trustworthy |
| 2   | [Reproducible builds](reproducible-builds.md)         | Pinned tools, lock files, `-trimpath`, build IDs, proving two builds are the same       |
| 3   | [Trunk-based development](trunk-based-development.md) | Small PRs, short-lived branches, stacked changes, merge queues                          |
| 4   | [Feature flags](feature-flags.md)                     | Deploy vs release, flag types, OpenFeature, cleaning up old flags                       |
| 5   | [Deployment strategies](deployment-strategies.md)     | Rolling, blue/green, canary, progressive delivery, database changes during deploys      |
| 6   | [GitOps](gitops.md)                                   | Pull-based deploys with Argo CD or Flux, drift detection, environment promotion         |
| 7   | [Infrastructure as code](infrastructure-as-code.md)   | Terraform/OpenTofu state, plans and drift, modules, and reviewing infra changes safely  |

## How each page works

The same shape as the [Fundamentals](../fundamentals/index.md): **Why care?**, **The idea**, **Under the hood**, **Try it**, **Traps** and **Check yourself**. The running example is this site's own repository and the `orders` service: their CI workflows, pinned tools, and a deploy to a local `kind` cluster. How to make each stage tamper-proof is the topic of [Secure delivery](../secure-delivery/index.md).
