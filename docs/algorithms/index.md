# Data structures and algorithms

Not interview puzzles: the structures already inside your tools, and how to tell when one is the wrong choice.

## Why go deeper?

You use these structures every day, often without knowing it:

- A loop that was fine with 1,000 orders takes an hour with 1,000,000.
- A `map` lookup is "O(1)", but the profile says it is the hot spot.
- A priority queue would turn a ten-second job into ten milliseconds, and nobody noticed.
- Git, Postgres, Kubernetes and your rate limiter all use the same handful of ideas.

This chapter is about **reasoning**: how cost grows with input size, which structure fits which access pattern, and how real systems ([Git](../git/index.md), [databases](../databases/index.md), the Go runtime) use them. The examples use Go's `slices`, `maps` and `container` packages before writing anything by hand.

## The big picture

| You need to...                     | Reach for              | Used in                                       |
| ---------------------------------- | ---------------------- | --------------------------------------------- |
| Look up by key                     | Hash map               | Go `map`, caches, Git's object lookup         |
| Keep things sorted and search fast | Balanced tree / B-tree | Database indexes, filesystems                 |
| Always get the smallest next item  | Heap                   | Timers, schedulers, Dijkstra, top-k           |
| Model things and their links       | Graph                  | Git commits, service dependencies, k8s owners |
| Answer "seen it?" with little RAM  | Bloom filter           | LSM trees, Git, caches                        |

## Reading order

!!! note "Planned chapter"

    This section is a roadmap. The pages below are written in later sessions and linked here when they land.

| #   | Page                       | You will learn                                                                     |
| --- | -------------------------- | ---------------------------------------------------------------------------------- |
| 1   | Big-O in practice          | Counting work, amortized cost, constants and caches, measuring with benchmarks     |
| 2   | Arrays, slices and hashing | Contiguous memory, hash functions, collisions, how Go's Swiss-table `map` works    |
| 3   | Trees                      | Binary search trees, balanced trees, B-trees, tries and radix trees (HTTP routers) |
| 4   | Heaps and queues           | Binary heaps, `container/heap`, ring buffers, timers and the Go scheduler          |
| 5   | Graphs                     | BFS, DFS, topological sort, shortest paths, cycles; Git and dependency graphs      |
| 6   | Sorting and searching      | `slices.Sort` (pdqsort), stability, binary search, external sort for big data      |
| 7   | Probabilistic structures   | Bloom filters, HyperLogLog, count-min sketch, consistent hashing                   |

## How each page works

The same shape as the [Fundamentals](../fundamentals/index.md): **Why care?**, **The idea**, **Under the hood**, **Try it in Go**, **Traps** and **Check yourself**. Every claim about speed comes with a benchmark you can run.
