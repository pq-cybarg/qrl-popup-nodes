# Variants

Full matrix at
[docs/variants](https://theqrl.github.io/qrl-popup-nodes/variants.html).
Short version:

| Image tag                  | Shipped overlay                     | Delta vs standard  |
|----------------------------|-------------------------------------|--------------------|
| `qrl-gqrl:standard`        | (base `docker-compose.yaml`)        | — baseline         |
| `qrl-gqrl:moscow-chill`    | `docker-compose.moscow-chill.yaml`  | +SHAKE256 at 0x03, +ML-DSA-87 detached verify at 0x06 |
| `qrl-gqrl:rescue-edition`  | `docker-compose.rescue.yaml`        | moscow-chill plus Rescue-Prime at 0x20, STARK verify at 0x21, ML-DSA-87 verify-with-addr at 0x22 |

## Which to pick

- **standard** — closest to upstream. Benchmark baseline.
- **moscow-chill** — the canonical QRL 2.0 QIP spec. Use this for
  dApps destined for the live chain.
- **rescue-edition** — moscow-chill + zRonin extras (Rescue-Prime +
  STARK + ML-DSA-87 verify-with-addr).

## Precompile addresses

| Addr | standard                | moscow-chill             | rescue-edition           |
|------|-------------------------|--------------------------|--------------------------|
| 0x01 | `depositroot`           | same                     | same                     |
| 0x02 | `sha256hash`            | same                     | same                     |
| 0x03 | `ripemd160hash`         | **`shake256(input, 64)`**| **`shake256(input, 64)`**|
| 0x06 | `bn256AddGasEIP2565`    | **`mchillMLDSA87Verify`**| **`mchillMLDSA87Verify`**|
| 0x20 | —                       | —                        | **`rescuePrime`**        |
| 0x21 | —                       | —                        | **`starkVerifier`**      |
| 0x22 | —                       | —                        | **`mldsa87Verify`-with-addr** |

RIPEMD-160 at 0x03 is replaced by SHAKE-256 in both PQ variants —
intentional, documented spec divergence.

## Building

```bash
./docker/build-images.sh standard rescue-edition moscow-chill
# or one at a time:
./docker/build-images.sh rescue-edition
```

The build driver passes a `VARIANT=` build-arg to
`docker/popup-node/Dockerfile.gqrl` which overlays the chosen variant
tree on top of `qrl-2.0/upstream/go-qrl` before `go build`. Images
are byte-identical between builders when `SOURCE_DATE_EPOCH` is pinned —
see [Reproducibility](Reproducibility.html).

## Adding a variant

```
qrl-2.0/variants/
├── moscow-chill/
├── rescue-edition/
└── <your-new-variant>/      ← cp moscow-chill, edit core/vm/contracts.go
```

Then: `./docker/build-images.sh <your-new-variant>` + a compose
overlay that pins `image: qrl-gqrl:<your-new-variant>` on every gqrl
service.
