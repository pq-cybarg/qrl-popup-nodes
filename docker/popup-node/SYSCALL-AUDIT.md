# Popup-node syscall audit

Basis for the `seccomp-qrl.json` starter profile. Captured by running
`strace -f -c -p 6` inside a `--pid=container:<name> --cap-add=SYS_PTRACE
--security-opt seccomp=unconfined` alpine sidecar attached to a
running 4-node compose bring-up. Each trace ran 60s during steady-state
block production (leader past genesis, followers peered).

## Captured (2026-09-01, arm64)

### gqrl-node1 (23 unique syscalls, 60s window)

```
77.24  futex          30580  (1228 errors)
10.42  nanosleep      15922
 8.09  epoll_pwait    14645
 1.68  write           5061
 1.58  read           11928  (6780 errors)
 0.18  tgkill           359
 0.17  rt_sigreturn     359  (53 errors)
 0.15  getpid           360
 0.13  sched_yield     1009
 0.08  openat            20
 0.08  madvise          106
 0.05  pread64           60
 0.04  getdents64        42
 0.02  epoll_ctl         18
 0.02  accept4           11  (2 errors)
 0.02  close             29
 0.01  fdatasync          3
 0.01  setsockopt        45
 0.00  sched_getaffinity 60
 0.00  getsockname        9
 0.00  statfs             2
 0.00  getrandom          8
 0.00  restart_syscall    1
```

### beacon-node1 (24 unique syscalls, 60s window)

```
81.81  futex          33787  (1443 errors)
 7.84  nanosleep      19252
 6.45  epoll_pwait    17251  (16 errors)
 1.65  write           6512
 1.02  read           10417  (4724 errors)
 0.26  sendto           691
 0.19  getpid           533
 0.18  recvfrom        1029  (178 errors)
 0.17  rt_sigreturn     534  (30 errors)
 0.10  sched_yield      977
 0.10  tgkill           533
 0.06  pwrite64          20
 0.04  madvise          132
 0.03  socket           169
 0.03  getsockname      169
 0.02  pread64           60
 0.02  close            169
 0.02  fdatasync          8
 0.01  sched_getaffinity 60
 0.00  bind             169
 0.00  getrandom         13
 0.00  fcntl              8
 0.00  mmap               2
 0.00  restart_syscall    1
```

### Union (30 syscalls)

`accept4, bind, close, epoll_ctl, epoll_pwait, fcntl, fdatasync, futex,
getdents64, getpid, getrandom, getsockname, madvise, mmap, nanosleep,
openat, pread64, pwrite64, read, recvfrom, restart_syscall,
rt_sigreturn, sched_getaffinity, sched_yield, sendto, setsockopt,
socket, statfs, tgkill, write`

## What steady-state does NOT include

The 60s window covered running (block production + P2P + engine API)
but MISSED:

- **Startup.** `execve`, dynamic linker (musl static-linked on Alpine,
  so limited), `mmap`/`mprotect` for library loading, Go runtime
  bootstrap (`clone`, `rt_sigaction`, `sigaltstack`, `set_tid_address`,
  `set_robust_list`, `prctl PR_SET_NAME`, `arch_prctl`,
  `sched_setaffinity`, `epoll_create1`).
- **Rare I/O paths.** `unlink`/`rename` on DB rotation, `sendmmsg`
  batching, `truncate` on log rotation, `fallocate` on chaindata
  allocation.
- **Error/retry paths.** `getpeername` on failed connect, `shutdown`,
  `getsockopt`, socket family variants.
- **Interactive attach.** `getsid`, `setsid`, `signalfd`, `eventfd2`
  if debug console is opened.

So `seccomp-qrl.json` is intentionally wider than this list —
Docker-default-derived allowlist (~200 syscalls) that covers startup
+ steady-state + common error paths. The steady-state data validates
that our allowlist doesn't accidentally exclude a syscall the app
actually uses.

## Re-capturing

```bash
# On a running compose stack:
docker run --rm --pid=container:qrl-gqrl-node1 \
    --cap-add=SYS_PTRACE --security-opt seccomp=unconfined \
    alpine sh -c 'apk add --no-cache strace >/dev/null 2>&1 && \
                  timeout 60 strace -f -c -p 6 2>&1 | tail -80'
```

PID 6 is the actual binary (tini is PID 1). `-f` follows Go's thread
pool, `-c` gives the summary. Longer windows catch more edge cases.

## Reproducing on arm64 vs x86_64

The absolute syscalls differ slightly between architectures — arm64
uses `newfstatat` instead of `stat`/`stat64`, `clock_gettime64` on 32-bit
compat layers, etc. Docker seccomp profiles are per-arch (`SCMP_ARCH_X86_64`
+ `SCMP_ARCH_AARCH64`), and the moby default lists both. `seccomp-qrl.json`
handles both arches by including union of both allowlists.
