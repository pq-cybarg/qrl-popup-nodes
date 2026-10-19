---
title: Reproducibility proof
nav_order: 9
---

# Reproducibility proof

Task #80 — a two-builder byte-identical reproducibility proof.

## Why

An attacker who swaps a build-time dependency (an apt mirror, a Go
module, a Docker base image) can insert a backdoor whose only
footprint is a change in the OCI layer digests. **Reproducibility is
the backstop.** If two independent builders can start from the same
source commit and produce byte-identical OCI tarballs, the "an
adversary swapped a dep" story stops working — unless they swapped
it on both builders in a way that both still produce identical
output, which is a much harder bar.

## How

`docker/reproducibility-check.sh` drives the proof. Two modes:

### Local-serial

```bash
./docker/reproducibility-check.sh --local-serial
```

Runs `docker/build-images.sh` twice on this host — first with the
default buildx builder, second against a fresh builder named
`repro-b` so caches don't short-circuit the comparison. Diffs the
resulting OCI tarballs by sha256. Catches **stage-local** non-
determinism (timestamp leaks, Go map iteration, random inputs).

### Remote

```bash
./docker/reproducibility-check.sh --remote builder2.lan
```

Rsyncs the repo tree to `builder2.lan`, runs `build-images.sh` over
SSH there, scps the tarballs back, diffs. Catches **host-dependent**
non-determinism: libc differences, apt mirror drift, kernel version,
hostname leak into image metadata. **This is the real test** — the
local-serial variant exists for CI quickstart.

Needs passwordless SSH to the remote and the repo accessible at the
same absolute path on both sides (rsync preserves mtimes so SDE
behaves consistently).

## SOURCE_DATE_EPOCH

Everything hinges on `SOURCE_DATE_EPOCH`. By default the script pins
it to the commit time of `HEAD`. Override if needed:

```bash
SOURCE_DATE_EPOCH=1700000000 ./docker/reproducibility-check.sh ...
```

The build driver passes the value down the Dockerfile chain. Every
stage that emits a timestamp must respect it: `cp -p`, `tar
--mtime="@${SOURCE_DATE_EPOCH}"`, `go build -trimpath -buildvcs=false
-ldflags='-s -w -buildid='`.

## Output

A markdown report at `docker/reproducibility-last-run.md`:

```
# Reproducibility proof — 20260101T000000Z

- Mode: `remote` (remote: `builder2.lan`)
- SOURCE_DATE_EPOCH: `1700000000` (…)
- Images under test: 6

| Image                       | Builder A sha256 | Builder B sha256 | Match |
|-----------------------------|------------------|------------------|-------|
| qrl-gqrl-standard.oci.tar   | `abc…`           | `abc…`           | ✅    |
| qrl-gqrl-rescue-edition.oci.tar | `def…`       | `def…`           | ✅    |
| qrl-gqrl-moscow-chill.oci.tar | `ghi…`         | `ghi…`           | ✅    |
| qrl-popup-beacon-node1.oci.tar | `jkl…`        | `jkl…`           | ✅    |
| qrl-popup-validator.oci.tar | `mno…`           | `mno…`           | ✅    |
| qrl-popup-dashboard.oci.tar | `pqr…`           | `pqr…`           | ✅    |

## Outcome: **PASS ✓** — all images byte-identical.
```

A diverged pair gets a per-layer dump so the first-mismatch digest is
easy to isolate.

## When divergence happens

The report's "Common causes" footer lists the usual suspects:

1. **Timestamp leak**. `grep` every Dockerfile for `date`, `$(date)`,
   `now()`, `time()`. Pin or elide.
2. **Non-deterministic iteration order**. Go pre-1.0 map iteration,
   `glob()` without sort, Python 2 `dict`. Sort or pin.
3. **Base image drift**. All popup Dockerfiles pin base images by
   `@sha256:…`. Any `apt-get upgrade` or un-pinned base re-introduces
   drift and is removed on sight.
4. **Random inputs**. `uuidgen`, `openssl rand`, `/dev/urandom` must
   be seeded by `SOURCE_DATE_EPOCH` or elided entirely. Our images
   use neither.
5. **Go build id**. Needs `-ldflags='-buildid='` (already set
   project-wide).
6. **Host libc / kernel**. Only visible under `--remote` mode; the
   fix is to standardise on a specific host image (we document
   building on Debian bookworm with linux-image-6.1 as the reference).

## CI integration

Add to a GH Actions workflow:

```yaml
- name: Reproducibility proof
  run: ./docker/reproducibility-check.sh --local-serial
  env:
    SOURCE_DATE_EPOCH: ${{ github.event.pull_request.base.sha_timestamp }}
```

A per-PR local-serial pass gives 90% of the coverage at ~5 min cost.
Weekly cron can run the full remote proof against a second runner.
