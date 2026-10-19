# seccomp soak-test (task #84)

A tightened seccomp allowlist can appear to work for weeks and
then block a syscall that only fires at epoch N, at the first ancient
prune, or on a release-pipeline tarball load — surfacing as `EPERM`
or `Operation not permitted` from inside the container and usually
misdiagnosed as a logic bug.

This soak-test runs each known slow-firing exercise in a loop for N
hours, scans every service log + the host audit stream for seccomp
block signatures, and fails fast with the exact exercise + container
+ syscall that regressed.

## Running it

```bash
# default: 4h soak against the qrl-popup project
./docker/soak/run-seccomp-soak.sh

# longer soak against a different stack
./docker/soak/run-seccomp-soak.sh --duration 28800 --project l2-rescue

# unlock destructive exercises (reset + ancient prune)
QRL_SOAK_ALLOW_DESTRUCTIVE=1 ./docker/soak/run-seccomp-soak.sh --duration 7200
```

Expected: PASS and `docker/soak/last-run.md` lists every syscall the
run hit — compare that set against `docker/seccomp-profile.json` to
spot allowlist entries that are no longer load-bearing.

A FAIL names the exercise and the container; copy the surfaced
syscall name/number into the profile's `names` list under the
matching action, rebuild the dashboard image, and re-run.

## What each exercise covers

| # | Exercise | Code paths exercised | Known risky syscalls |
|---|---|---|---|
| 01 | `chain-snapshot` | gqrl ancient reads, beacon SSZ dump, tar+gzip stream | `madvise`, `renameat2`, `copy_file_range`, `sync_file_range` |
| 02 | `release-load` | OCI tarball verify, ML-DSA-87 signature check | `getrandom`, `mmap` (PROT_EXEC), `pkey_mprotect` |
| 03 | `full-reset` (destructive) | mass `unlink`+`rmdir`, container stop/create under docker.sock | `rseq`, `membarrier`, `faccessat2` |
| 04 | `token-index-rebuild` | leveldb compaction + sequential block scan | `fallocate`, `pwritev2`, `sync_file_range` |
| 05 | `pq-key-regen` | ML-DSA-87 keygen, Argon2id KDF | `getrandom`, `clone3`, `rseq`, `set_robust_list` |
| 06 | `epoch-boundary-wait` | SSZ shuffle + flag flip + finality | `futex_waitv`, `getcpu` |
| 07 | `ancient-prune` (destructive) | freezer file hole-punch + manifest rename | `fallocate(PUNCH_HOLE)`, `renameat2` |

Exercise 04 currently piggybacks on the dashboard's in-process 60-s
indexer poll via `/api/explorer/tokens` and `/api/explorer/richlist`.
A dedicated `POST /api/explorer/rebuild-tokenindex` would force a
full rebuild every iteration and tighten coverage — the exercise
calls that out at the top so a future endpoint drops in cleanly.

Exercise 07 depends on `admin_prune` being exposed by gqrl. The
compose default does not include `admin` in `--http.api`, so this
exercise SKIPs rather than fails when the method is absent. To
exercise it, add `admin` to the gqrl service's `--http.api` list in
`docker-compose.yaml` (local operator opt-in only; **never** on a
public chain).

## Interpreting a failure

A seccomp block manifests three ways:

1. `audit: type=1326` on the host (Linux, auditd/journald). Carries the
   syscall number, the executable name, and the comm. **The number
   maps directly to a `names:` entry you add to the profile.**
2. `EPERM` from inside the container — a `strace -k -p <pid>` on the
   process reproduces the exact syscall. (Our fallback mode shows
   the EPERM string + container name + log line.)
3. Silent — the container recovers but a feature degraded. The
   exercise's own expected-output check catches this.

After adding a syscall to the allowlist, bump the profile version
suffix in the file's leading comment, rebuild the dashboard image
(`docker compose build --no-cache dashboard`), and re-run the soak.

## When to run

- After any edit to `docker/seccomp-profile.json`.
- Before cutting a release (as part of `docker/release.sh`).
- When a new long-running code path lands (new exercise required —
  add a numbered script in `exercises/` and register it in
  `run-seccomp-soak.sh`'s `EXERCISES` array).
- When a user reports a bug that smells like "worked yesterday,
  fails under docker today."
