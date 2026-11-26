# Software Engineering Tutor

A personal training site about software engineering, built with [Zensical](https://zensical.org).

It covers:

- **Fundamentals**: registers, stack, heap, memory, encodings, and more.
- **Go**: from basic to expert, with an enterprise focus (project layout, tooling, testing, CLIs, services).

Style: plain English, friendly like "Head First", deep like "The Hard Parts".

## Getting started

Install [mise](https://mise.jdx.dev), then:

```sh
mise install       # install all tools
mise run serve     # live preview at http://localhost:8000
```

## Tasks

| Task             | What it does                       |
| ---------------- | ---------------------------------- |
| `mise run serve` | Serve the site with live reload    |
| `mise run build` | Build the static site into `site/` |
| `mise run fmt`   | Format Markdown, YAML and Go code  |
| `mise run lint`  | Lint Markdown and GitHub workflows |
| `mise run check` | Format check + lint + build (CI)   |
| `mise run clean` | Remove build output                |

## License

[MIT](LICENSE)
