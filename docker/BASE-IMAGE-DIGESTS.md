# Popup-node base-image digest pinning

Every `FROM` in `docker/popup-node/Dockerfile.*` names a base image by
`tag@sha256:...`, not by tag alone. This closes a supply-chain hole
that mutable tags leave open: an attacker who compromises Docker Hub,
`golang`, `alpine`, or `node` publisher credentials can silently
substitute a malicious image behind the same tag, and the next
`docker pull` swallows it whole. Digest pins refuse the swap.

## Currently pinned tags

| Tag | Used by | Refresh cadence |
| --- | --- | --- |
| `golang:1.25-alpine` | Dockerfile.gqrl, Dockerfile.beacon, Dockerfile.validator (builder stage) | monthly, or on Go CVE |
| `alpine:3.20` | Dockerfile.gqrl, Dockerfile.beacon, Dockerfile.validator (runtime stage) | monthly, or on Alpine security bulletin |
| `node:22-alpine` | Dockerfile.status-server (runtime stage) | monthly, or on Node CVE |

The exact `sha256:` values are inline in each Dockerfile — grep for
`sha256:` under `docker/popup-node/` to see current state.

## Refresh procedure

```bash
./docker/refresh-base-digests.sh   # regenerates every pin from `docker pull`
git diff -- docker/popup-node/     # eyeball the digest changes
./configure.sh --force --regen-genesis
qrlenv run -- docker compose up -d  # verify 4-node convergence still works
docker compose down -v
git commit -am "docker: refresh base-image digest pins (YYYY-MM-DD)"
```

The refresh script is idempotent — running it again with no upstream
changes is a no-op.

## Why not `latest` or floating major?

The whole point of a digest pin is to defeat *silent* upstream changes.
`latest` and floating majors (`golang:1`, `alpine:3`) reintroduce the
same attack. Even a signed publisher can be compromised for a single
tag push — the pin gives us a fixed target we can reason about.

## What this pin does *not* protect against

- **Malicious code in a version we pinned before it was known-bad.** If
  the digest we pinned was already backdoored, pinning it perpetuates
  the compromise. Mitigation is cross-referencing against known-good
  digests published upstream (Alpine, Docker, Node maintainers each
  publish their manifest digests on their release pages) — a phase 3
  concern (reproducible builds, cosign verification of the base image
  itself).
- **A digest we pull from a compromised registry proxy / cache.** If a
  corporate registry proxy is compromised, the digest returned may be
  attacker-controlled. Verify against upstream once, then pin.
