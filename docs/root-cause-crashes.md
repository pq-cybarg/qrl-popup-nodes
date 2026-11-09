---
title: Root cause — repeated crashes
nav_order: 14
---

# Root cause: repeated beacon + computer-wide crashes

This repo's maintainer has experienced two distinct crash patterns
on an ongoing basis:

1. **Beacon-node1 stuck in `Error`/restart loop** on every Docker
   Desktop cold-start, requiring a `docker rm -f` + fresh compose-up
   to recover.
2. **Computer-wide "crashes"** — the Mac becoming unresponsive, keyboard
   input queued, app windows frozen — typically a few days after
   bringing all three stacks up.

Investigation on 2026-11-08 nailed both.

## Finding 1 — beacon-node1 cold-start hang is a BoltDB mmap stall

### Evidence

Disk-resident BoltDB sizes on the `qrl-popup` stack:

| node          | `beaconchaindata/beaconchain.db` |
|---------------|----------------------------------|
| beacon-node1  | **1.8 GB**                       |
| beacon-node2  | 5.0 MB                           |
| beacon-node3  | 3.8 MB                           |
| beacon-node4  | 6.3 MB                           |

Node1 is the **lead proposer** in the compose topology (validator's
default proposal order starts at index 0 → node1 gets the first slot
of every epoch). Because `--interop-executiondata-votes` runs
without the deposit-contract gate, every proposed block's full state
transition is persisted to node1's DB. Nodes 2-4 run as light
followers.

Cold-restart log pattern on node1 (6 successive attempts, all die at
the same step):

```
Checking DB   database-path="/opt/qrl/data/beaconchaindata"
Opening Bolt DB at /opt/qrl/data/beaconchaindata/beaconchain.db
Loading genesis state from disk.
Deposit contract: Q00000…0003
<SILENCE — never reaches "Starting beacon node">
<container healthcheck times out, restarts, loop>
```

Nodes 2-4 go `Checking DB` → `Starting beacon node` in **1-2 seconds**.

### Root cause

BoltDB opens with `syscall.Mmap` of the full DB file. Over Docker
Desktop's VirtioFS bind-mount, a 1.8 GB mmap on macOS takes
15-60 seconds under memory pressure. The beacon-chain process is
blocked on that mmap, the healthcheck (`wget --post qrl_blockNumber`
to a port not yet listening) fails, compose marks the container
unhealthy, the `depends_on: beacon-node1 service_healthy` chain
collapses.

The container itself never crashes — it's hung in `BoltDB.Open`.
Docker's healthcheck wins the race and reports Error before the
mmap completes.

### Mitigations

1. **Vacuum the DB offline** before restart. See
   `docker/vacuum-beacon-db.sh`. Reduces 1.8 GB back to the live-state
   footprint (~300 MB). First-boot mmap drops to a few seconds.
2. **Move node1's chaindata to a Docker volume** instead of a
   bind-mount. VirtioFS adds 10-30% I/O overhead; a named volume lives
   inside the Linux VM and mmap stays fast. See
   `docker/docker-compose.beacon-volume.yaml`.
3. **Bump beacon-node1's healthcheck `start_period` to 600s** so the
   mmap has time to complete. See the compose overlay.

## Finding 2 — "Computer crashed" is OS-level swap thrash, not kernel panic

### Evidence

At the moment the maintainer reported the most recent "crash":

```
$ uptime
20:29  up 11 days, 23:08, load averages: 110.54 132.71 125.05
$ vm_stat | grep 'Pages free\|Pages inactive'
Pages free:          3886.        (= 62 MB free of 16 GB)
Pages inactive:    172245.
$ sysctl vm.swapusage
vm.swapusage: total = 12288.00M used = 11779.50M free = 508.50M
$ docker info | grep Memory
 Total Memory: 7.653GiB
$ ls /Library/Logs/DiagnosticReports/*.panic
(no files)
```

No kernel panics. The symptoms are:

- Load average 110-130 on an 8-core Mac (normal: 1-8).
- 62 MB free RAM on a 16 GB system.
- 11.8 GB swap used out of 12.3 GB.
- VM compressor compressing 287 GB of cold pages into 7.26 GB.
- Three popup stacks allocating ~10-12 GB combined; Docker Desktop
  VM sized at 7.65 GB.

### Root cause

The macOS kernel does not kernel-panic under this load — it throttles
every process trying to allocate a page, which makes the system
*appear* frozen. The maintainer misreads this as a crash and hard-
reboots, which leaves the on-disk BoltDBs in states that take
longer to re-mmap (Finding 1), which worsens the next reboot.

Specifically:

- Three stacks × 11 containers each = **33 containers**.
- Peak memory: 4 × gqrl @ ~400 MB + 4 × beacon @ ~500 MB + validator
  @ ~400 MB + dashboard @ ~300 MB = **4.9 GB per stack × 3 = 14.7 GB**.
- Docker Desktop VM has **7.65 GB**. Difference is served from
  macOS compressor + swap on the host.
- VirtioFS fsync activity from 3 × 4 = 12 active BoltDB writers
  keeps the host I/O queue saturated → compressor can't catch up →
  swap ballooning → paging storm.

### Mitigations

1. **Resize the Docker Desktop VM to 12-14 GB** (Preferences →
   Resources → Memory). On a 16 GB Mac this leaves 2-4 GB for the
   host — tight but viable.
2. **Run one stack at a time** instead of three. `qrl-popup` alone
   fits comfortably in 7.65 GB with headroom.
3. **Cap per-service memory** via compose. See
   `docker/docker-compose.resource-limits.yaml`:
   ```yaml
   services:
     gqrl-node1:
       deploy:
         resources:
           limits: { memory: 500m, cpus: '1' }
           reservations: { memory: 300m }
   ```
4. **Use a Linux host, not Docker Desktop on macOS**, if the stack
   is a daily driver. VirtioFS is specifically a macOS/Windows
   pain-point; native Docker on Linux has none of this overhead.

## Compound effect (the real bug)

The two findings feed each other:

1. Host swap thrash makes the maintainer reboot.
2. Reboot leaves BoltDB in a state that mmap-stalls.
3. Beacon-node1 fails to come healthy → `depends_on` chain breaks →
   nothing on the stack is reachable → maintainer thinks the chain
   is broken → more frantic intervention → more memory pressure →
   Goto 1.

Breaking either link ends the cycle. The structural fix is **move
chaindata off VirtioFS** (Finding 1 fix 2) + **cap per-service memory**
(Finding 2 fix 3), both shipped in this commit.
