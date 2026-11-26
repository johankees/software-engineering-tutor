# Distributed systems

What changes when your program is many programs on many machines, connected by a network that can lie.

## Why go deeper?

The moment you have two services, a database and a queue, you have a distributed system. Then:

- A payment times out. Did it happen or not? The retry charges the customer twice.
- An event is published, but the database write it describes was rolled back.
- Two servers disagree about which order came first.
- One slow service makes every other service slow, then all of them fall over.
- The cache says the item is in stock. The database says it is not.

These are not bugs in one service. They are the normal behavior of networks and clocks. This chapter gives you the patterns that real systems use to stay correct anyway.

## The big picture

```mermaid
flowchart LR
    C[Client] -->|request + idempotency key| O[orders]
    O -->|transaction| DB[(orders DB)]
    DB -->|outbox| R[Relay]
    R -->|event| MQ[[Message broker]]
    MQ --> P[payments]
    MQ --> S[shipping]
    P -.->|timeout? retry!| O
```

There are three hard truths:

1. **The network can drop, delay, duplicate or reorder any message.** A timeout tells you nothing about what happened.
2. **Clocks on different machines do not agree.** "Before" and "after" need more than timestamps.
3. **Parts fail on their own.** The system must keep working, or fail safely, while some parts are down.

## Reading order

!!! note "Planned chapter"

    This section is a roadmap. The pages below are written in later sessions and linked here when they land.

| #   | Page                    | You will learn                                                                           |
| --- | ----------------------- | ---------------------------------------------------------------------------------------- |
| 1   | Failure models          | Crashes, partitions, slow nodes, the fallacies of distributed computing, failure domains |
| 2   | Time and ordering       | Clock drift, monotonic clocks, Lamport clocks, vector clocks, hybrid logical clocks      |
| 3   | Consistency models      | Linearizable, sequential, causal, eventual; CAP and PACELC without the myths             |
| 4   | Retries and idempotency | Timeouts, backoff with jitter, idempotency keys, circuit breakers, load shedding         |
| 5   | Messaging               | Queues vs logs, Kafka and NATS, at-least-once delivery, ordering, consumer groups        |
| 6   | Outbox and sagas        | The dual-write problem, transactional outbox, inbox dedupe, sagas and compensation       |
| 7   | Consensus and Raft      | Leader election, log replication, quorums, and why etcd needs an odd number of nodes     |
| 8   | Caching                 | Cache-aside, write-through, TTLs, invalidation, thundering herds, `singleflight`         |

## How each page works

The same shape as the [Fundamentals](../fundamentals/index.md): **Why care?**, **The idea**, **Under the hood**, **Try it**, **Traps** and **Check yourself**. The examples break things on purpose: we kill processes, drop packets and skew clocks around the `orders` service, and watch what each pattern saves.
