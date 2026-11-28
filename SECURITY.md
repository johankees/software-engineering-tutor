# Security policy

## Reporting a vulnerability

Please do **not** open a public issue for a security problem.

Report it privately with GitHub's
[private vulnerability reporting](https://github.com/johankees/software-engineering-tutor/security/advisories/new).
Include what you found, how to reproduce it, and the impact you expect.

You will get an answer within 7 days. Fixed problems are published as a
GitHub security advisory, with credit to you if you want it.

## Scope

- The site build and deployment: the GitHub Actions workflows, `mise.toml`,
  `mise.lock` and the tools they install.
- The example code under `examples/`.
- Content that teaches an unsafe practice as safe. That is a bug too.

Only the `main` branch is supported. There are no released versions.

## How this repository protects itself

The [Secure delivery](https://johankees.github.io/software-engineering-tutor/secure-delivery/)
chapter explains each control that this repository uses: signed commits,
rulesets, pinned actions and tools, least-privilege workflows, secret
scanning, dependency updates with a cooldown, and OpenSSF Scorecard.
