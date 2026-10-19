---
title: Variants
nav_order: 5
---

# gqrl variants

The gqrl execution-layer client is **variant-aware**: a single
`qrl-2.0/upstream/go-qrl` source tree compiles into three distinct
images, differing only in the precompile table published at
`core/vm/contracts.go`. Each variant image carries the same genesis
compatibility, the same QRVM semantics, and the same JSON-RPC surface
— the only observable difference is which addresses in the `0x01…0x22`
range respond to a `CALL`.

## Variant catalogue

| Image tag                   | Compose overlay                      | Precompile set                       |
|-----------------------------|--------------------------------------|--------------------------------------|
| `qrl-gqrl:standard`         | (none — the base `docker-compose.yaml` ships standard) | QRL 2.0 baseline  |
| `qrl-gqrl:rescue-edition`   | `docker/docker-compose.rescue.yaml`  | moscow-chill plus `0x20` Rescue-Prime, `0x21` STARK verify, `0x22` ML-DSA-87 verify-with-addr |
| `qrl-gqrl:moscow-chill`     | `docker/docker-compose.moscow-chill.yaml` | DigitalGuards QIP spec: `0x03` SHAKE256(x,64), `0x06` ML-DSA-87 detached verify |

## Precompile table — side by side

Both variants retain the standard first precompile slots:

| Addr | Standard               | moscow-chill            | rescue-edition          |
|------|------------------------|-------------------------|-------------------------|
| 0x01 | `depositroot`          | same                    | same                    |
| 0x02 | `sha256hash`           | same                    | same                    |
| 0x03 | `ripemd160hash`        | **`shake256(input, 64)`** | **`shake256(input, 64)`** |
| 0x04 | `dataCopy`             | same                    | same                    |
| 0x05 | `bigModExp`            | same                    | same                    |
| 0x06 | `bn256AddGasEIP2565`   | **`mchillMLDSA87Verify`** detached | **`mchillMLDSA87Verify`** detached |
| 0x07 | `bn256ScalarMulGasEIP2565` | same                | same                    |
| 0x08 | `bn256PairingGasEIP2565`   | same                | same                    |
| 0x09 | `blake2F`              | same                    | same                    |
| 0x20 | —                      | —                       | **`rescuePrime`** Rp64_256/512 |
| 0x21 | —                      | —                       | **`starkVerifier`** |
| 0x22 | —                      | —                       | **`mldsa87Verify`** with-addr |

The `shake256` precompile at `0x03` **replaces** `ripemd160` in both
moscow-chill and rescue-edition. Contracts calling `0x03` for RIPEMD-160
on those variants will get a SHAKE256(input, 64) response instead —
this is an intentional, documented spec divergence (RIPEMD-160 is a
2003 primitive with no quantum relevance and reclaiming the slot for
SHAKE-256 keeps the precompile table tight).

## When to pick which

- **standard** — closest to upstream for anyone benchmarking the base
  QRL 2.0 chain, writing dApps that compile to the plain QRVM surface,
  or testing migrations off Ethereum where you want the stock pre-M
  precompile shape.
- **moscow-chill** — the canonical "spec with the QIP precompiles"
  variant. If you're developing a dApp that intends to run on the
  live QRL 2.0 chain under the DigitalGuards precompile set, this is
  the one. Smaller precompile delta from standard than rescue-edition.
- **rescue-edition** — same as moscow-chill plus three extra
  precompiles. Useful if you're testing a zRonin-style application
  that needs Rescue-Prime + STARK verify, or an ML-DSA-87 verify
  variant that bundles the address derivation.

The three run side-by-side so you can prototype against two or three
at once and A/B the gas costs or verification results.

## Building a variant from source

```bash
# builds all three (parallel buildx)
./docker/build-images.sh standard rescue-edition moscow-chill

# or one at a time
./docker/build-images.sh rescue-edition
```

The build driver passes a `VARIANT=` build-arg to
`docker/popup-node/Dockerfile.gqrl`, which overlays
`qrl-2.0/variants/<variant>/core/vm/contracts.go` on top of the
`upstream/go-qrl` tree before `go build`. The rest of the Dockerfile
is identical across variants.

Reproducibility: pass `SOURCE_DATE_EPOCH=<unix ts>` to `build-images.sh`
to pin the image mtime. Variant images are expected to be byte-identical
between two builders on the same host when SOURCE_DATE_EPOCH is pinned
— see [security — reproducibility](security.html#reproducible-builds)
for the two-builder diff recipe.

## Precompile call examples

Call ML-DSA-87 verify on moscow-chill (precompile `0x06`):

```javascript
// payload layout: 64-byte signer || 32-byte msg hash || variable-length sig
const payload = Buffer.concat([signerQ64, msgHash, sigBytes]);
const result = await web3.eth.call({
  to: '0x0000...0006',
  data: '0x' + payload.toString('hex'),
});
// result === '0x01' iff signature is valid
```

Call Rescue-Prime hash on rescue-edition (precompile `0x20`):

```javascript
// payload: variable-length input (Rp64 absorbs 64-byte chunks)
const result = await web3.eth.call({
  to: '0x0000...0020',
  data: '0x' + input.toString('hex'),
});
// result is the 32-byte hash digest
```

## Adding a fourth variant

If you want to prototype a new precompile set:

1. `cp -r qrl-2.0/variants/moscow-chill qrl-2.0/variants/<yourname>`
2. Edit `core/vm/contracts.go` to swap in your precompile. Match the
   signature `PrecompiledContract` interface:
   ```go
   type PrecompiledContract interface {
       RequiredGas(input []byte) uint64
       Run(input []byte) ([]byte, error)
   }
   ```
3. Register at a free address in `PrecompiledContractsZond`.
4. Build: `./docker/build-images.sh <yourname>` — produces
   `qrl-gqrl:<yourname>`.
5. Follow the [adding a fourth stack](stacks.html#adding-a-fourth-stack)
   recipe to spin it up alongside the three canonical ones.

## Variant surface in the dashboard

The dashboard's identity chip shows the running variant pulled from
the `qrl_variant` RPC method (gqrl exposes it under its admin
namespace). On a variant mismatch — e.g. you pointed the beacon at
the standard image but the gqrl is running rescue-edition — the chip
turns amber and the Node Control tab shows an explicit "variant
mismatch" warning before the chain starts producing invalid blocks.
