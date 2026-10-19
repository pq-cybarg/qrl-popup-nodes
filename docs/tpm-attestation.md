---
title: TPM attestation recipe
nav_order: 10
---

# TPM2 attestation recipe (task #81)

A TPM2-backed boot measurement that proves a popup-node host booted a
known-good kernel + initrd + bootloader. Combines with the
[reproducibility proof](reproducibility.html) for a chain-of-custody
story: the operator who runs this stack ran the exact binaries that
came out of the signed build.

**This doc is a recipe** — not a one-command script, because the
TPM2 toolchain is per-host (TPM device path, PCR allocation, EK cert
chain). The operator runs the steps once per deployment, bakes the
output quote into the dashboard's STARTUP INTEGRITY panel.

## Prerequisites

- Host with a TPM 2.0 chip (`ls /dev/tpm*` returns `/dev/tpm0`).
- `tpm2-tools` (`apt install tpm2-tools` on Debian / Ubuntu).
- Linux kernel with CONFIG_IMA + CONFIG_TRUSTED_KEYS enabled (checked
  via `grep CONFIG_IMA /boot/config-$(uname -r)`).
- An **Attestation Key (AK)** certificate chain. We walk through
  provisioning below.

## Step 1 — provision an AK

The AK is a non-exportable TPM-resident keypair whose public half
signs quotes. Its EK cert chains to the TPM manufacturer. Operators
provision once per host:

```bash
# Create the primary endorsement key (EK)
tpm2_createek -c /etc/popup/ek.ctx -G rsa -u /etc/popup/ek.pub

# Create the AK off the EK
tpm2_createak -C /etc/popup/ek.ctx \
    -c /etc/popup/ak.ctx \
    -u /etc/popup/ak.pub \
    -n /etc/popup/ak.name

# (Optional) attest the AK pubkey to a trusted CA for a cert chain.
# For a local-only deployment, skip and keep ak.pub pinned on the
# dashboard directly.
```

## Step 2 — bake the measured-boot policy

PCRs 0-7 measure firmware; 8-9 measure the kernel + initrd; 11-14
cover grub + bootconfig; 15 is used for IMA policy. Capture the known-
good values after a trusted boot:

```bash
tpm2_pcrread sha256:0,1,2,3,4,5,6,7,8,9,11,12,13,14 > /etc/popup/known-good.pcrs
```

Store `known-good.pcrs` alongside the AK pubkey — the dashboard
compares live quote PCRs against these values on every poll.

## Step 3 — generate a quote on boot

A systemd unit emits a quote at every boot, pinned to a dashboard-
visible nonce:

```ini
# /etc/systemd/system/popup-tpm-quote.service
[Unit]
Description=Emit TPM2 quote for popup-node attestation
After=multi-user.target
Before=docker.service

[Service]
Type=oneshot
ExecStart=/usr/local/bin/popup-tpm-quote
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
```

```bash
# /usr/local/bin/popup-tpm-quote
#!/usr/bin/env bash
set -euo pipefail
NONCE=$(dd if=/dev/urandom bs=32 count=1 status=none | xxd -p | tr -d '\n')
echo "${NONCE}" > /run/popup/tpm-nonce

tpm2_quote \
    -c /etc/popup/ak.ctx \
    -l sha256:0,1,2,3,4,5,6,7,8,9,11,12,13,14 \
    -q "${NONCE}" \
    -m /run/popup/tpm-quote.msg \
    -s /run/popup/tpm-quote.sig \
    -o /run/popup/tpm-quote.pcrs
```

Install + enable:

```bash
install -m 0755 popup-tpm-quote /usr/local/bin/
install -m 0644 popup-tpm-quote.service /etc/systemd/system/
systemctl enable popup-tpm-quote.service
systemctl start popup-tpm-quote.service
```

## Step 4 — expose the quote to the dashboard

The dashboard container needs read-only access to the quote files.
Add an overlay:

```yaml
# docker/docker-compose.tpm.yaml
services:
  dashboard:
    volumes:
      - /run/popup/tpm-nonce:/opt/qrl/tpm/nonce:ro
      - /run/popup/tpm-quote.msg:/opt/qrl/tpm/quote.msg:ro
      - /run/popup/tpm-quote.sig:/opt/qrl/tpm/quote.sig:ro
      - /run/popup/tpm-quote.pcrs:/opt/qrl/tpm/quote.pcrs:ro
      - /etc/popup/ak.pub:/opt/qrl/tpm/ak.pub:ro
      - /etc/popup/known-good.pcrs:/opt/qrl/tpm/known-good.pcrs:ro
    environment:
      QRL_TPM_PATH: /opt/qrl/tpm
```

Bring up with the TPM overlay:

```bash
qrlenv run -- docker compose \
    -f docker/docker-compose.yaml \
    -f docker/docker-compose.hardened.yaml \
    -f docker/docker-compose.tpm.yaml \
    up -d
```

## Step 5 — verify in the dashboard

`status-server.js` reads `QRL_TPM_PATH` and surfaces:

- **PCR match** — green when live PCRs == `known-good.pcrs`, red
  otherwise. Red means the host booted a different kernel / initrd /
  bootloader than the last trusted boot.
- **Quote signature valid** — green when `tpm2_checkquote` succeeds
  against the pinned AK pubkey, red when the AK private half was
  rotated unexpectedly.
- **Nonce freshness** — green when `/run/popup/tpm-nonce` is less
  than 1 h old (confirms the quote was generated at boot, not
  replayed).

Dashboard panel: **STARTUP INTEGRITY** in Node Control, next to
HARDENING.

## Step 6 — remote attestation

For a nation-state-resistant deployment, send the quote to an
independent verifier:

```bash
# at scale, do this via a webhook — one-off, run on the host:
curl -X POST https://attest.example.org/popup \
    -F quote=@/run/popup/tpm-quote.msg \
    -F sig=@/run/popup/tpm-quote.sig \
    -F pcrs=@/run/popup/tpm-quote.pcrs \
    -F nonce="$(cat /run/popup/tpm-nonce)" \
    -F host="$(hostname)"
```

The verifier replays `tpm2_checkquote` against the pinned AK pubkey,
compares PCRs to the baseline, logs the result into a tamper-evident
feed. A failed match from the field is the operator's early warning.

## Why not seal secrets to PCRs

`tpm2_create` can wrap a secret with a PCR policy — the TPM only
unseals if the current PCRs match. **The popup stack intentionally
doesn't do this** — the host's chain data isn't a secret; it's a
public blockchain. The crypto that matters (validator keys,
dashboard-managed wallets) is wrapped by the operator-chosen Argon2id
+ AES-256-GCM in `qrlenv`, which is strictly stronger than a TPM
PCR seal (it's a password-authenticated derivation, not a
boot-state-authenticated one).

TPM here is **attestation only** — proving what booted, not
protecting what runs. The two are complementary.

## Troubleshooting

- `tpm2_quote: TPM2_RC_AUTH_FAIL` — AK auth value stale after a TPM
  wipe. Re-run `tpm2_createak`.
- `tpm2_checkquote: nonce mismatch` — someone else is generating
  quotes against the same AK. Rotate the AK and audit.
- PCR mismatch after a `apt-get upgrade` — expected! Capture a new
  `known-good.pcrs` after confirming the upgrade.
- No `/dev/tpm0` on the host — TPM may be disabled in BIOS. Check
  BIOS → Security → TPM State → Enabled, PCR Bank → SHA-256.
