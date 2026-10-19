# SELinux policy for popup-node containers

`qrl_popup.te` carves a `qrl_popup_t` domain, a `qrl_popup_exec_t`
executable label, and a `qrl_popup_data_t` data-dir label. Containers
transition into `qrl_popup_t` on start — out of `unconfined_t` — and
can only touch files carrying the data label.

Policies here work on RHEL / CentOS Stream / Fedora / Rocky /
AlmaLinux. On an Ubuntu host, use the AppArmor variant instead
(see `../apparmor/README.md`).

## Compile and load

```sh
cd docker/selinux
checkmodule -M -m -o qrl_popup.mod qrl_popup.te
semodule_package -o qrl_popup.pp -m qrl_popup.mod
sudo semodule -i qrl_popup.pp
sudo semodule -l | grep qrl_popup       # expect: qrl_popup
```

## Label the data directories

The compose services bind-mount `DATA_ROOT=./data` (or
`./data-l2-rescue`, `./data-l2-moscow`). Re-label each root so the
contents carry `qrl_popup_data_t`:

```sh
DATA_ROOT=$PWD/docker/data
sudo semanage fcontext -a -t qrl_popup_data_t "$DATA_ROOT(/.*)?"
sudo restorecon -R "$DATA_ROOT"
ls -lZ "$DATA_ROOT" | head              # files should show qrl_popup_data_t
```

Repeat for every data root operator-side.

## Attach via docker compose

```sh
qrlenv run -- docker compose \
  -f docker/docker-compose.yaml \
  -f docker/docker-compose.hardened.yaml \
  -f docker/docker-compose.seccomp.yaml \
  -f docker/docker-compose.selinux.yaml \
  up -d
```

The overlay appends `security_opt: - label=type:qrl_popup_t` to every
popup service so docker starts them in that domain instead of
`container_t`.

## Verify

```sh
PID=$(docker inspect -f '{{.State.Pid}}' qrl-popup-gqrl-node1)
sudo cat /proc/$PID/attr/current        # expect: ...:qrl_popup_t:...
sudo ausearch -m AVC -ts recent -c gqrl # expect no AVC denials
```

An AVC denial means the service tried to touch something the policy
doesn't allow — review the denial with `audit2allow -a`, add a
targeted rule to `qrl_popup.te`, recompile, reload.

## Deny categories (encoded in `qrl_popup.te`)

- `neverallow self:capability sys_admin sys_module sys_rawio sys_boot
  sys_ptrace mac_admin mac_override` — mount, module load, raw I/O,
  reboot, debugger, policy override.
- `neverallow self:process ptrace` — cross-process memory peek.
- `neverallow { proc_t sysctl_t sysfs_t kernel_t }:file write` —
  kernel-tuning surface is read-only.
- `neverallow self:system module_load` — kernel module injection.
- `neverallow self:packet_socket *` + `neverallow self:rawip_socket *` —
  no raw / packet sockets.

Allowed:

- TCP+UDP+unix sockets (needs `net_bind_service` capability for ports
  below 1024 — the popup stack uses 3600/4000/8080/9545+, all above).
- Full access to `qrl_popup_data_t` for the data dir.
- Read-only on proc + sysfs for runtime self-inspection.

## After a policy update

```sh
sudo semodule -r qrl_popup
sudo semodule -i qrl_popup.pp
docker compose ... restart
```

If a container refuses to start with `permission denied`, run
`sudo ausearch -m AVC -ts recent` and either add the missing rule to
`qrl_popup.te` (preferred) or widen the data-dir label.
