# TPM2 attestation

Full recipe at
[docs/tpm-attestation](https://theqrl.github.io/qrl-popup-nodes/tpm-attestation.html).
This is a per-host provisioning recipe — not a one-command script.

## Six steps

1. Provision an AK (attestation key) from the TPM EK.
2. Capture the known-good PCR values after a trusted boot.
3. Install the `popup-tpm-quote.service` systemd unit — generates a
   fresh quote on every boot with a per-boot nonce.
4. Expose the quote + nonce + known-good PCRs to the dashboard
   container via a bind-mount overlay (`docker-compose.tpm.yaml`).
5. Dashboard **STARTUP INTEGRITY** panel reports the three checks:
   PCR match, quote signature valid, nonce freshness.
6. (Optional) Push the quote to an external verifier for remote
   attestation.

## Why not seal secrets to PCRs

Deliberate design choice — the popup chain data isn't secret;
attestation is for proving what booted, not protecting what runs.
Key protection is handled by `qrlenv` (Argon2id + AES-256-GCM) which
is strictly stronger than a TPM PCR seal.

## Prerequisites

- TPM 2.0 chip (`ls /dev/tpm0`)
- `tpm2-tools` package
- Kernel with `CONFIG_IMA` + `CONFIG_TRUSTED_KEYS`
- `/etc/subuid` and `/etc/subgid` provisioned if also running rootless

## Troubleshooting

- `TPM2_RC_AUTH_FAIL` → AK auth value stale after TPM wipe. Re-provision.
- `nonce mismatch` → someone else generating quotes against the same
  AK. Rotate and audit.
- PCR mismatch after `apt-get upgrade` → expected! Re-capture
  `known-good.pcrs` after confirming the upgrade.
- No `/dev/tpm0` → check BIOS → Security → TPM State → Enabled, PCR
  Bank → SHA-256.
