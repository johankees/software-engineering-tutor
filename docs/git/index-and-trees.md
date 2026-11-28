# The index and the three trees

## Why care?

- "`git reset --soft`, `--mixed`, `--hard`... I always look it up, and I'm still not sure what it will do."
- "I staged half a file with `git add -p`, then changed the file again. What will the commit contain?"
- "`git status` takes 6 seconds in our monorepo. Is that just how it is?"

All of these are about the **index**: the file between your working folder and your commits. Most people think of it as "the list of staged files". It is much more than that, and once you know how it works, `reset` becomes predictable.

## The idea: three versions of every file

At any moment, every tracked file can exist in **three places**:

```mermaid
flowchart LR
    W["Working tree<br/>(files on disk)"] -->|"git add"| I["Index<br/>(.git/index)"]
    I -->|"git commit"| H["HEAD<br/>(last commit's tree)"]
    H -->|"git restore --staged<br/>git reset"| I
    I -->|"git restore"| W
```

1. **HEAD**: the tree of the last commit. Read-only history.
2. **The index**: the tree you are building for the **next** commit. It starts as a copy of HEAD.
3. **The working tree**: the files you edit.

The index is a **draft of the next commit**. You change the working tree all the time, and `git add` copies chosen changes into the draft. `git commit` records the index, not the working tree.

`git status` is two comparisons: **HEAD vs index** ("Changes to be committed") and **index vs working tree** ("Changes not staged").

## Under the hood: "staged" means "already in the object database"

This part surprises many people: `git add` does not "mark a file". It **writes a blob** right away, and puts its hash in the index:

```console
$ echo 'hello index' > hello.txt
$ git add hello.txt
$ git ls-files -s hello.txt
100644 eb2fc3ca2f129a710df1a6c0fd5ebfd088a10bfd 0	hello.txt
$ git cat-file -p $(git ls-files -s hello.txt | cut -d" " -f2)
hello index
```

The blob exists before any commit. Now change the file again:

```console
$ echo 'hello worktree' > hello.txt
$ git status --short
MM hello.txt
$ git rev-parse HEAD:hello.txt :hello.txt; git hash-object hello.txt
8d0e41234f24b6da002d962a26c2495ea16a425f
eb2fc3ca2f129a710df1a6c0fd5ebfd088a10bfd
d50adf584568c855201c57390651489b57ae14e1
```

Three different hashes: HEAD, index (`:hello.txt`), working tree. `MM` means "modified in the index, and modified again in the working tree". The commit will contain `hello index`, the **staged** version. `git show :hello.txt` shows it.

### `add -p` makes blobs that never existed on disk

```console
$ printf 'y\nn\n' | git add -p
diff --git a/n.txt b/n.txt
index 0ff3bbb..7ab40bf 100644
--- a/n.txt
+++ b/n.txt
@@ -1,5 +1,5 @@
 1
-2
+two
 3
 4
 5
(1/2) Stage this hunk [y,n,q,a,d,k,K,j,J,g,/,e,p,P,?]? @@ -16,5 +16,5 @@
 16
 17
 18
-19
+nineteen
 20
(2/2) Stage this hunk [y,n,q,a,d,K,J,g,/,e,p,P,?]?
$ git diff --cached
diff --git a/n.txt b/n.txt
index 0ff3bbb..a4164fb 100644
--- a/n.txt
+++ b/n.txt
@@ -1,5 +1,5 @@
 1
-2
+two
 3
 4
 5
```

The answer was yes to the first hunk and no to the second. Git built a new version of the file in memory (`two` but still `19`), wrote it as blob `a4164fb`, and put that in the index:

```console
$ git cat-file -p a4164fb | sed -n "1,3p;19p"
1
two
3
19
```

This version of `n.txt` was **never on your disk**. So: always run the tests on what you commit, not on what you have. `git stash push --keep-index` lets you test only the staged state.

## Under the hood: the index file

`.git/index` is a binary file. It starts with a 12-byte header:

```console
$ xxd .git/index | head -8
00000000: 4449 5243 0000 0002 0000 0002 6b07 f630  DIRC........k..0
00000010: 0121 7b35 6b07 f630 0121 7b35 0000 0820  .!{5k..0.!{5...
00000020: 000b 605d 0000 81a4 0000 03e8 0000 03e8  ..`]............
00000030: 0000 000d 06ab 7d0f 9a35 a7d1 0707 1149  ......}..5.....I
00000040: 6d6c a1cb 892a 258f 000b 636d 642f 6d61  ml...*%...cmd/ma
00000050: 696e 2e67 6f00 0000 0000 0000 6b07 f630  in.go.......k..0
00000060: 0121 7b35 6b07 f630 0121 7b35 0000 0820  .!{5k..0.!{5...
00000070: 000b 605a 0000 81a4 0000 03e8 0000 03e8  ..`Z............
```

Decode it:

- `44 49 52 43` = `DIRC`, "dircache", the old name of the index.
- `0000 0002` = format version 2.
- `0000 0002` = two entries.

Then the first entry, for `cmd/main.go`:

| Bytes                 | Field                               | Value                                 |
| --------------------- | ----------------------------------- | ------------------------------------- |
| `6b07f630 01217b35`   | ctime (seconds, nanoseconds)        | when the file's metadata last changed |
| `6b07f630 01217b35`   | mtime                               | when the content last changed         |
| `00000820`            | device                              |                                       |
| `000b605d`            | inode                               |                                       |
| `000081a4`            | mode                                | `0o100644`                            |
| `000003e8 000003e8`   | uid, gid                            | 1000, 1000                            |
| `0000000d`            | file size                           | 13 bytes = `package main\n`           |
| `06ab7d0f...892a258f` | blob hash (20 bytes)                | the `package main` blob               |
| `000b`                | flags: name length + **stage** bits | 11 characters, stage 0                |
| `cmd/main.go`         | path, then zero padding             |                                       |

`git ls-files --debug` prints the same data in a readable form:

```console
$ git ls-files --debug hello.txt
hello.txt
  ctime: 1795683888:18971445
  mtime: 1795683888:18971445
  dev: 2080	ino: 745562
  uid: 1000	gid: 1000
  size: 10	flags: 0
```

Notice two things. First, the index is **flat**: one entry per file, with the full path. There are no directory entries. Second, every entry has the **stat data** of the file on disk. That is what makes it fast.

After the entries come optional **extensions**, each with a 4-letter signature. The important ones: `TREE` (cached tree hashes, so `write-tree` does not rehash unchanged directories), `REUC` (resolve-undo, to redo a conflict you resolved), `UNTR` (the untracked cache), `FSMN` (fsmonitor state) and `link` (split index). At the end is a checksum of the whole file.

### Why `git status` is fast: the stat cache

To know whether `hello.txt` changed, Git could read and hash every file. In a repo with 100,000 files, that is slow. Instead, Git calls `lstat()` on each file and compares the result with the stat data in the index: mtime, ctime, size, inode, uid, gid, mode. If they all match, Git **assumes the file is unchanged** without reading it. Only files whose stat data changed get hashed.

So a `git status` costs about one `lstat()` per tracked file, plus reading the directories to find **untracked** files. For huge repos, both of those still add up. Two features cut them down:

- **`core.untrackedCache`** stores directory mtimes in the index (`UNTR`). If a directory's mtime did not change, nothing was added or removed in it, so Git skips reading it. Test whether your file system supports it with `git update-index --test-untracked-cache`. On this machine:

  ```console
  $ git update-index --test-untracked-cache
  Testing mtime in '/tmp/.../many' ...... OK
  ```

- **`core.fsmonitor=true`** starts a background daemon (`git fsmonitor--daemon`) that listens to the OS's file change events. Then `git status` asks the daemon "what changed since last time?" and only checks those paths. The `core.fsmonitor` docs name Windows and macOS. The 2.56 Linux build used for this book also has it (`feature: fsmonitor--daemon` in `git version --build-options`) and uses inotify:

  ```console
  $ git fsmonitor--daemon start
  $ git fsmonitor--daemon status
  fsmonitor-daemon is watching '/tmp/.../idx'
  ```

  It refuses network-mounted repos by default. On Linux, inotify has a per-user limit on watches (the man page says it is typically 8192), which a very big repo can hit.

  (Long temporary paths in this output are shortened to `/tmp/...`.)

!!! tip "One switch for big repos"

    `git config feature.manyFiles true` turns on a group of settings for repos with many files.
    In 2.56 they are `index.skipHash=true` (no checksum on index writes), `index.version=4` (path
    prefix compression) and `core.untrackedCache=true`.

### Racy git

The stat check has a gap. Consider this case:

1. You change `foo` and run `git add foo`. The index records `foo`'s mtime.
2. Within the same second (or the same tick of your file system's clock), you change `foo` again, **keeping its size**.

Now the stat data still matches. Git would think `foo` is unchanged. This is called **racy git**. Git's fix: when the index is written, Git remembers the index file's own mtime. Any entry whose mtime is the **same or newer** than the index file is "racily clean", and Git compares its **content**, not only stat data. When the index is written again, racily clean entries get their recorded size set to zero, so they can never match by accident later. You never see this; it is why Git is both fast and correct.

## Under the hood: reset is "move three things"

Now `reset` is simple to explain. It does up to three steps, in order, and the mode says where to **stop**:

| Step | What it does                              | `--soft` | `--mixed` (default) | `--hard` |
| ---- | ----------------------------------------- | -------- | ------------------- | -------- |
| 1    | Move the branch that `HEAD` points to     | yes      | yes                 | yes      |
| 2    | Make the **index** match the new commit   |          | yes                 | yes      |
| 3    | Make the **working tree** match the index |          |                     | yes      |

Here it is on a repo with commits `one` and `two`:

```console
$ git log --oneline
d2b54c7 two
dd2ea27 one
$ git reset --soft HEAD~1 && git status --short && git log --oneline
M  hello.txt
dd2ea27 one
```

`--soft`: the branch moved back, but the index still has `two`'s content. So the change shows as **staged**. This is "uncommit, keep everything staged", useful for squashing the last few commits into one.

```console
$ git reset --mixed HEAD~1 && git status --short
Unstaged changes after reset:
M	hello.txt
 M hello.txt
```

`--mixed`: the index was reset too. The change is still in your file, but **unstaged**.

```console
$ git reset --hard HEAD~1 && git status --short && cat hello.txt
HEAD is now at dd2ea27 one
hello git
```

`--hard`: everything matches `one`. The commit `two` is not deleted; the branch no longer points to it. Git saved the old position:

```console
$ cat .git/ORIG_HEAD
d2b54c7bb96257bc1e044e97b217a4565201426e
$ git reset --hard ORIG_HEAD
HEAD is now at d2b54c7 two
```

!!! warning "The only part of `reset --hard` that loses data"

    Committed work is safe: the reflog and `ORIG_HEAD` have it. **Uncommitted changes in the
    working tree are not.** Step 3 overwrites them, and they were never in the object database.
    (Changes you had _staged_ were written as blobs, so they can be found as dangling objects.
    See [Recovery](recovery-and-history.md).) Run `git stash` first if you are unsure.

### `restore` and `switch`: the clear versions

`git checkout` does too many things. Since Git 2.23, two commands split it up:

- **`git switch <branch>`** moves `HEAD` to another branch, and updates index and working tree. It refuses if that would overwrite local changes.
- **`git restore`** copies file content between the three trees, without moving `HEAD`:
  - `git restore file`: index → working tree (throw away unstaged changes).
  - `git restore --staged file`: HEAD → index (unstage, keep the file).
  - `git restore --source=HEAD~3 file`: any commit → working tree.
  - `git restore --staged --worktree file`: HEAD → both.

`reset <commit> -- file` with a path never moves a branch. It only copies the file from the commit into the index, which is the same as `restore --staged --source=<commit> file`.

## Try it

```sh
echo one > f.txt && git add f.txt && git commit -m one
echo two > f.txt && git add f.txt
echo three > f.txt
git ls-files -s f.txt            # the index's blob
git cat-file -p :f.txt           # "two"
git diff                         # index vs working tree
git diff --cached                # HEAD vs index
git commit -m two && git show HEAD:f.txt   # which one was committed?
```

Then try `git reset --soft HEAD~1`, `--mixed`, `--hard`, and look at `git status` and `git ls-files -s` after each one.

## Traps

- **Testing the working tree, committing the index.** After `add -p`, the commit is a state you never ran. Use `git stash push --keep-index`, test, then `git stash pop`.
- **What `git commit -a` includes.** It stages all **tracked** modified files. New files are not included.
- **`reset --hard` with uncommitted work.** The only part of Git that can really lose data. Stash or commit first.
- **Editing files with tools that keep the mtime.** Some copy or extract tools preserve the old mtime and size. Git may then miss the change until the stat data differs. `touch` the file or `git add` it explicitly.
- **Turning on `untrackedCache` on a file system with broken directory mtimes** (some network mounts). Run `--test-untracked-cache` first.

## Check yourself

??? question "1. You run `git add main.go`, then delete the file from disk before committing. Can you get the content back?"

    Yes. `git add` wrote a blob into the object database and put its hash in the index. Run
    `git restore main.go` (index to working tree), or `git show :main.go`.

??? question "2. What is the difference between `git reset --soft HEAD~2` and `git reset --mixed HEAD~2`?"

    Both move the branch back two commits. `--soft` leaves the index as it was, so the changes from
    those two commits show as staged. `--mixed` also resets the index, so the changes show as
    unstaged. In both cases, the files on disk do not change.

??? question "3. Why does `git status` not read the content of every file?"

    The index stores stat data (mtime, ctime, size, inode, ...) for every entry. Git calls `lstat()`
    and only reads and hashes a file if its stat data changed, or if it is "racily clean".

??? question "4. `git status` shows `MM file.go`. What does each M mean, and what will `git commit` record?"

    First column: HEAD vs index (modified, staged). Second column: index vs working tree (modified
    again, not staged). The commit records the staged (index) version only.

??? question "5. What is in the index during a merge conflict that is never there otherwise?"

    Entries with stage 1, 2 and 3 for the conflicted path: base, ours and theirs. Normally every
    entry has stage 0. See [Merging](merging.md).

## Recap

- Three trees: **HEAD** (last commit), **index** (next commit), **working tree** (your files).
- `git add` writes a blob immediately. "Staged" means "the blob is already in the object database".
- `add -p` can stage a version of a file that never existed on disk.
- `.git/index` is a flat, sorted list: stat data + mode + hash + path per file, plus extensions like `TREE` and `UNTR`.
- The stat cache makes `status` fast. Racy-git checks keep it correct. Untracked cache and fsmonitor make it fast in huge repos.
- `reset` moves the branch (`--soft`), then the index (`--mixed`), then the working tree (`--hard`).
- `switch` moves `HEAD`. `restore` copies file content. Use them instead of `checkout`.

Next: how all those objects are stored efficiently. [Storage: packs and big repos](storage.md).
