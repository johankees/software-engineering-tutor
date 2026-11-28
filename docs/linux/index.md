# Linux and the OS

The kernel your services really run on: files, processes, signals, and the isolation between them.

## Why go deeper?

Almost every server, container and Kubernetes node runs Linux. Most days you never think about it. Then:

- The service dies with `too many open files`, and raising a limit "fixes" it for a week.
- `kill` stops the service, but in-flight requests are lost.
- The disk is full, you delete a 20 GB log file, and the disk is _still_ full.
- A container is "out of memory" while the host has 50 GB free.
- Something is slow, and `top` says the CPU is idle.

[Processes, threads and system calls](../fundamentals/processes-and-threads.md) showed how a program asks the kernel for help. This chapter explains the kernel's side. After it, containers are easy to explain: a container is a normal Linux process with a few extra limits on what it can see and use.

## The big picture

```mermaid
flowchart TB
    subgraph U [User space]
        P1[ordersd] --- P2[postgres]
        P3[shell] --- P4[systemd]
    end
    U -->|system calls| K
    subgraph K [Kernel]
        S[Scheduler] --- VM[Virtual memory]
        VFS[Virtual file system] --- NET[Network stack]
        NS[Namespaces + cgroups]
    end
    K --> H[Hardware: CPU, RAM, disk, NIC]
```

1. **Everything is a file** (almost). Disks, sockets, pipes and `/proc` all hide behind one small API: `open`, `read`, `write`, `close`.
2. **Processes live in a tree.** They start, get signals, exit, and must be reaped by their parent.
3. **Namespaces decide what a process can see. Cgroups decide how much it can use.** Together, they are a container.

## Reading order

Each page builds on the one before it.

| #   | Page                                                     | You will learn                                                                         |
| --- | -------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| 1   | [System calls up close](system-calls.md)                 | The user/kernel boundary, `strace`, vDSO, why some calls are slow, how Go makes them   |
| 2   | [File descriptors](file-descriptors.md)                  | The fd table, pipes, sockets, `ulimit`, `O_CLOEXEC`, the "deleted but still open" trap |
| 3   | [Filesystems](filesystems.md)                            | Inodes, links, the page cache, `fsync`, mounts, overlayfs, `/proc` and `/sys`          |
| 4   | [Signals and process lifetime](signals-and-processes.md) | `fork`/`exec`, exit codes, zombies, PID 1, `SIGTERM` vs `SIGKILL`, graceful shutdown   |
| 5   | [Namespaces and cgroups](namespaces-and-cgroups.md)      | PID, mount, network and user namespaces; cgroup v2 limits; build a "container" by hand |
| 6   | [systemd](systemd.md)                                    | Units, dependencies, socket activation, journald, running a Go service with hardening  |
| 7   | [Debugging a Linux box](debugging.md)                    | `ps`, `ss`, `lsof`, `vmstat`, `iostat`, `perf`, eBPF tools, and the USE method         |

## How each page works

The same shape as the [Fundamentals](../fundamentals/index.md): **Why care?**, **The idea**, **Under the hood**, **Try it**, **Traps** and **Check yourself**. All output comes from real runs on WSL2 (Ubuntu 24.04, kernel 5.15, systemd 255, Go 1.27.4). WSL2 is a real Linux kernel, with one difference we point out where it matters: it mounts cgroups in "hybrid" mode. The examples use the `ordersd` service from the [Go chapter](../go/index.md) as the example process.
