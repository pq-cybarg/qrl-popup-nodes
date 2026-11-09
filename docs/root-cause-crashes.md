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

1. **Move node1's chaindata to a named Docker volume** instead of a
   bind-mount. VirtioFS adds 10-30% I/O overhead; a named volume
   lives inside the Linux VM and mmap stays fast regardless of DB
   size. See `docker/docker-compose.beacon-volume.yaml`. **This is
   the only mitigation that fully resolves Finding 1** for the
   interop-executiondata-votes workload; the other two are
   workarounds.
2. **Bump beacon-node1's healthcheck `start_period` to 600s** so the
   mmap has time to complete before compose declares the container
   unhealthy. Shipped in the beacon-volume overlay.
3. ~~**Vacuum the DB offline** via `docker/vacuum-beacon-db.sh`.~~
   **Empirically ineffective for this workload** — a 2026-11-09
   vacuum of the live 1.8 GB beacon-node1 DB reclaimed 0% (bbolt
   compact reported `1763127296 -> 1763127296 bytes (gain=1.00x)`).
   The DB is not fragmented; it is genuinely 1.8 GB of live state.
   The script is still shipped and still useful for chains whose
   BoltDB fragments over time (common in long-running eth2 clients),
   but **cannot help the interop-executiondata-votes profile this
   repo uses** because every slot's state transition is persisted
   and never pruned (see Finding 3).

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

## Finding 3 — unbounded cache growth on the lead proposer

### Evidence

`docker stats` snapshot taken 2026-11-09 across all three stacks:

| container                 | uptime  | RSS       |
|---------------------------|---------|-----------|
| qrl-beacon-node1          | 1 min   | 79 MB *(just restarted)* |
| l2-rescue-beacon-node1    | 3 days  | **799 MB** |
| l2-moscow-beacon-node1    | 3 days  | **907 MB** |
| qrl-beacon-node[2-4]      | 8 days  | 51-60 MB  |
| l2-rescue-beacon-node[2-4]| 8 days  | 48-53 MB  |
| l2-moscow-beacon-node[2-4]| 8 days  | 50-52 MB  |
| all gqrl-nodeN            | 8 days  | 15-33 MB  |
| all validators            | mins    | 22-48 MB  |
| all dashboards            | 8 days  | 28-31 MB  |

Only the **lead proposer** on each stack grows. Followers stay flat.
Growth rate: ~12 MB/hour (900 MB / 72 hours).

### Root cause

In qrysm's `--interop-executiondata-votes` mode with a **single
validator**, there is no 2/3 quorum for finalization. State transitions
are persisted to BoltDB every slot (bloating the on-disk DB linearly,
see Finding 1), AND the in-memory `forkchoice` store, attestation pool,
and state cache keep every state the chain has ever seen because the
finalization-triggered prune never fires.

This is a design-level unbounded-growth, not a classical memory leak
in the C/malloc sense — the Go runtime is correctly tracking every
allocation, it's just that nothing is marking them collectible.

### Mitigations

1. **Periodic restart** of the lead proposer (nightly cron, or
   `docker restart qrl-beacon-node1` whenever RSS > 500 MB). The
   state rehydrates from BoltDB; RSS resets to ~80 MB.
2. **Resource-limits overlay already caps RSS** at 800 MB for
   beacon-node1 and 400 MB for followers (`docker-compose.resource-limits.yaml`).
   Under the cap the OOM-killer will restart the container
   automatically — degenerate but bounded.
3. **Structural fix (future)**: qrysm patch to allow
   checkpoint-forced pruning without finalization, or run with
   ≥4 validators distributed across nodes so finalization engages.

## Finding 4 — non-popup containers dwarf the popup stacks

### Evidence

Same `docker stats` snapshot:

| container       | RSS       | % of VM |
|-----------------|-----------|---------|
| quark-valence   | **3.4 GB** | **44%** |
| quark-strong    | 149 MB    | 2%      |
| quark           | 10 MB     | 0.1%    |
| kurtosis-*      | ~50 MB    | 0.7%    |
| **all 3 QRL stacks combined** | **~2.6 GB** | **34%** |

On this host, non-popup Docker containers use **more memory than
all three QRL stacks combined**, and `quark-valence` alone is the
single biggest allocator in the VM.

### Root cause

Operator convenience: separate project (`quarknet`, a Valence-L3
dev environment) was left running in the same Docker VM as the
popup stacks. Neither project is aware of the other's memory
budget; both grow until the VM compressor pegs.

### Mitigation

Operational: **stop non-popup containers before bringing up all
three QRL stacks**, or move either workload to a separate Docker
context. Not a code change.

```bash
docker stop quark-valence quark-strong quark
# -or-
docker context create popup --docker host=unix:///var/run/docker-popup.sock
```

## Compound effect (the real bug)

Four findings feed each other:

1. Finding 4 shrinks headroom (3.4 GB budget quietly claimed by
   quark-valence).
2. Finding 3 grows the lead proposers (~12 MB/hour × 3 stacks).
3. Finding 2 kicks in once combined demand exceeds the 7.65 GB VM:
   swap thrash, host unresponsive.
4. The maintainer hard-reboots.
5. Finding 1 kicks in on reboot: 1.8 GB BoltDB mmap stalls for
   minutes → beacon-node1 fails healthcheck → depends_on chain
   breaks → maintainer thinks the chain is broken → more frantic
   intervention → more memory pressure → Goto 1.

Breaking any link ends the cycle. The structural fixes shipped in
this repo are:

- Finding 1 (BoltDB mmap stall): `docker-compose.beacon-volume.yaml`
  (named volume bypasses VirtioFS).
- Finding 2 (swap thrash): `docker-compose.resource-limits.yaml`
  (per-service memory caps).
- Finding 3 (unbounded cache growth): resource-limits cap turns
  the leak into a bounded restart cycle; periodic manual restart
  for a smooth path.
- Finding 4 (external RAM consumers): operational — stop unrelated
  containers or move them to another Docker context.
