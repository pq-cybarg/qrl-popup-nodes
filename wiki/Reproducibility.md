# Reproducibility proof

Full write-up at
[docs/reproducibility](https://theqrl.github.io/qrl-popup-nodes/reproducibility.html).

## The two-liner

```bash
# local-serial — fast, catches stage-local non-determinism
./docker/reproducibility-check.sh --local-serial

# remote — real test, catches host-dependent non-determinism
./docker/reproducibility-check.sh --remote builder2.lan
```

Output: `docker/reproducibility-last-run.md`. Exit 0 = byte-identical.
Exit 1 = diverged (report lists per-layer digests + common causes).

## SOURCE_DATE_EPOCH

Pinned to the commit time of `HEAD` by default. Everything in the
Dockerfile chain respects it — `tar --mtime=@…`, `go build -trimpath
-buildvcs=false -ldflags='-s -w -buildid='`, no `apt-get upgrade`.

## CI usage

```yaml
- run: ./docker/reproducibility-check.sh --local-serial
  env:
    SOURCE_DATE_EPOCH: ${{ github.event.pull_request.base.sha_timestamp }}
```

Local-serial on every PR, remote on a weekly cron.
