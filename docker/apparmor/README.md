# AppArmor profiles for popup-node containers

Four enforce-mode profiles live next to this README:

| profile            | pins service                              |
|--------------------|-------------------------------------------|
| `qrl-gqrl.aa`      | `gqrl-node[1-4]` (execution layer)        |
| `qrl-beacon.aa`    | `beacon-node[1-4]` (consensus)            |
| `qrl-validator.aa` | `validator`                               |
| `qrl-dashboard.aa` | `dashboard` (status-server / Node.js)     |

The `bootstrap-fetcher` is deliberately kept on the default container
profile — it runs for seconds and then exits, so a bespoke profile
would be dead weight.

## Load the profiles

Needs an AppArmor-enabled host (Ubuntu / Debian / SUSE kernels). On
each popup-node host:

```sh
sudo install -m 0644 docker/apparmor/qrl-gqrl.aa      /etc/apparmor.d/qrl-gqrl
sudo install -m 0644 docker/apparmor/qrl-beacon.aa    /etc/apparmor.d/qrl-beacon
sudo install -m 0644 docker/apparmor/qrl-validator.aa /etc/apparmor.d/qrl-validator
sudo install -m 0644 docker/apparmor/qrl-dashboard.aa /etc/apparmor.d/qrl-dashboard

sudo apparmor_parser -r -W /etc/apparmor.d/qrl-gqrl
sudo apparmor_parser -r -W /etc/apparmor.d/qrl-beacon
sudo apparmor_parser -r -W /etc/apparmor.d/qrl-validator
sudo apparmor_parser -r -W /etc/apparmor.d/qrl-dashboard

sudo aa-status | grep '^   qrl-'       # confirm 4 profiles loaded in enforce
```

## Attach via docker compose

Stack the overlay on top of the base + hardening overlays:

```sh
qrlenv run -- docker compose \
  -f docker/docker-compose.yaml \
  -f docker/docker-compose.hardened.yaml \
  -f docker/docker-compose.seccomp.yaml \
  -f docker/docker-compose.apparmor.yaml \
  up -d
```

This appends `security_opt: - apparmor=qrl-<service>` to each popup
service.

## Verify the profile is actually attached

```sh
PID=$(docker inspect -f '{{.State.Pid}}' qrl-popup-gqrl-node1)
sudo cat /proc/$PID/attr/current       # expect: qrl-gqrl (enforce)
```

If `/proc/<pid>/attr/current` reports `unconfined`, the overlay isn't
applied — most commonly because the compose stack was brought up
without the apparmor overlay flag.

## After a kernel upgrade

AppArmor profile hashes are kernel-version-sensitive. After `apt
upgrade linux-image-*`:

```sh
for p in qrl-gqrl qrl-beacon qrl-validator qrl-dashboard; do
    sudo apparmor_parser -r -W /etc/apparmor.d/$p
done
docker compose ... restart            # containers pick up the fresh profile
```

If a profile fails to parse after upgrade, kernel features it uses may
have been renamed; `aa-logprof` walks the delta.

## Deny-list rationale

Each profile denies, in enforce mode:

- `mount` / `remount` / `umount` / `pivot_root` — stops a compromised
  binary from re-mounting `/proc` or `/sys` to break out.
- `capability sys_module sys_rawio sys_admin sys_ptrace sys_boot
  mac_admin mac_override` — pairs with the compose `cap_drop: [ALL]`
  so that even if a cap is somehow restored, the LSM blocks the call.
- `@{PROC}/sys/** w` + `/sys/kernel/** w` — read-only kernel view.
- `/dev/mem`, `/dev/kmem`, `/dev/port`, `/dev/kvm` — no raw memory.
- `network packet` / `netlink raw` / `bluetooth` / `can` — TCP+UDP
  only; blocks a raw-socket sniff from inside the container.
- `ptrace` — stops cross-process memory peek.
- Executables outside `/opt/qrl/bin` and the runtime deps paths — no
  dropping-in a stage-two binary.

Everything not listed in the `allow` section is denied by default
(`flags=(enforce)`).

## ⚠️ qrl-dashboard carries a privilege cliff

Unlike the other three profiles, `qrl-dashboard.aa` allows read-write
access to `/var/run/docker.sock`:

```
/var/run/docker.sock rw,
```

This is required for the dashboard's `/api/compose/{status,reset,
projects,container/*}` and `/api/release/*` endpoints — they speak
HTTP to the docker daemon over that socket.

**docker.sock access is equivalent to root on the host.** A compromise
of the dashboard process (via a 0-day in Node.js, in status-server.js
itself, or in a dependency) lets the attacker `docker exec` into any
other container on the host (gqrl / beacon / validator) as uid 0 and
escape containers entirely.

Mitigations if the dashboard's trust bar is too high for your
deployment:

1. Mount the socket **read-only** by changing `rw` → `r` here. You
   lose `/api/compose/reset`, `/api/compose/container/*`, and the
   release pipeline — but the dashboard still works as a status
   surface.
2. Run the dashboard in a separate compose project with its own
   bridge, so a compromise can't reach the other services via docker
   DNS even with socket access.
3. Front the dashboard with an auth proxy (basic auth over
   `docker-compose.tls.yaml`) so remote reach is gated.
