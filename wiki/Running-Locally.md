# Running locally

See [docs/getting-started](https://theqrl.github.io/qrl-popup-nodes/getting-started.html)
for the full walkthrough. The 60-second version:

## One stack (standard)

```bash
git clone https://github.com/theQRL/qrl-popup-nodes.git
cd qrl-popup-nodes
./docker/configure.sh
qrlenv run -- docker compose \
    -f docker/docker-compose.yaml \
    -f docker/docker-compose.docker-sock.yaml \
    up -d
open http://127.0.0.1:8080/
```

## Three stacks (all variants)

```bash
# Build the two variant images once
./docker/build-images.sh rescue-edition moscow-chill

# l2-rescue
QRL_DATA_DIR=./data-l2-rescue qrlenv run -- docker compose -p l2-rescue \
    -f docker/docker-compose.yaml \
    -f docker/docker-compose.docker-sock.yaml \
    -f docker/docker-compose.rescue.yaml \
    up -d

# l2-moscow
QRL_DATA_DIR=./data-l2-moscow qrlenv run -- docker compose -p l2-moscow \
    -f docker/docker-compose.yaml \
    -f docker/docker-compose.docker-sock.yaml \
    -f docker/docker-compose.moscow-chill.yaml \
    up -d

# dashboards
open http://127.0.0.1:8080/ http://127.0.0.1:8180/ http://127.0.0.1:8280/
```

## From a signed release

```bash
curl -LO https://releases.example.org/qrl-popup-v1.0.0.tar
curl -LO https://releases.example.org/qrl-popup-v1.0.0.tar.sha256sums
curl -LO https://releases.example.org/qrl-popup-v1.0.0.tar.sha256sums.qrlsig

./docker/verify-images.sh \
    --sums qrl-popup-v1.0.0.tar.sha256sums \
    --sig qrl-popup-v1.0.0.tar.sha256sums.qrlsig \
    --pinned-pubkey keys/release.pub

./docker/load-release.sh qrl-popup-v1.0.0.tar
qrlenv run -- docker compose -f docker/docker-compose.yaml up -d
```

## Checking the chain is alive

```bash
curl -s http://127.0.0.1:3600/qrl/v1/node/syncing | jq .data.head_slot
# climbs by one every ~60 s = chain producing
```

Can't open 127.0.0.1:8080? See
[Troubleshooting](Troubleshooting.html#dashboard-crash-looping).
