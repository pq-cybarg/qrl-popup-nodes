#!/usr/bin/env bash
# reproducibility-check.sh — Two-builder byte-identical reproducibility
# proof (task #80).
#
# Builds every popup-node image TWICE from the same source tree, same
# SOURCE_DATE_EPOCH, same compose config — then compares the two
# builds image-by-image, layer-by-layer, by sha256 digest. The first
# mismatch is reported with full context (image, layer index, digests)
# so the operator can trace the non-determinism to a single Dockerfile
# instruction or a timestamp-leaking tool invocation.
#
# Modes:
#   --local-serial     Build twice sequentially on this host. Catches
#                      stage-local non-determinism (timestamp leaks,
#                      map iteration order, random inputs).
#   --remote <host>    Build the second copy over SSH on a remote
#                      host (preferred — catches host-dependent
#                      non-determinism: libc version, apt mirror
#                      drift, kernel version). Needs passwordless SSH
#                      to <host> and the repo at the same abs path.
#
# Output: a markdown report. Exit 0 = byte-identical; exit 1 = diverged;
# exit 2 = couldn't run.

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

MODE="local-serial"
REMOTE=""
OUT_REPORT="docker/reproducibility-last-run.md"
IMAGES="qrl-gqrl:standard qrl-gqrl:rescue-edition qrl-gqrl:moscow-chill qrl-popup-beacon-node1:latest qrl-popup-validator:latest qrl-popup-dashboard:latest"
SDE="${SOURCE_DATE_EPOCH:-}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --local-serial) MODE="local-serial"; shift ;;
    --remote)       MODE="remote"; REMOTE="$2"; shift 2 ;;
    --out)          OUT_REPORT="$2"; shift 2 ;;
    --images)       IMAGES="$2"; shift 2 ;;
    --source-date-epoch) SDE="$2"; shift 2 ;;
    -h|--help) sed -n '2,24p' "$0"; exit 0 ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done

if [[ -z "${SDE}" ]]; then
  # pin to this repo's HEAD commit time so the number is reproducible
  SDE=$(git log -1 --pretty=format:%ct)
  echo "using SOURCE_DATE_EPOCH from HEAD commit: ${SDE}"
fi
export SOURCE_DATE_EPOCH="${SDE}"

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
WORK_A="/tmp/repro-A-${STAMP}"
WORK_B="/tmp/repro-B-${STAMP}"
mkdir -p "${WORK_A}" "${WORK_B}"

# --- builder A: this host ------------------------------------------
echo "=== BUILD A — this host ==="
rm -rf docker/out/*.oci.tar || true
SOURCE_DATE_EPOCH="${SDE}" docker/build-images.sh ${IMAGES}
for img in docker/out/*.oci.tar; do
  cp "${img}" "${WORK_A}/"
done

# --- builder B: either serial here or ssh ---------------------------
echo "=== BUILD B — $([ "${MODE}" = remote ] && echo "${REMOTE} (ssh)" || echo "this host, fresh buildx cache") ==="
if [[ "${MODE}" == "remote" ]]; then
  REMOTE_WORK="/tmp/repro-remote-${STAMP}"
  ssh "${REMOTE}" "mkdir -p ${REMOTE_WORK}"
  # Rsync the repo tree (preserve mtimes for SDE-respecting builds)
  rsync -a --delete \
    --exclude '.git/' --exclude 'docker/out/' --exclude 'docker/data*/' \
    "${REPO_ROOT}/" "${REMOTE}:${REMOTE_WORK}/"
  ssh "${REMOTE}" "cd ${REMOTE_WORK} && SOURCE_DATE_EPOCH=${SDE} docker/build-images.sh ${IMAGES}"
  scp -q "${REMOTE}:${REMOTE_WORK}/docker/out/*.oci.tar" "${WORK_B}/"
else
  # local-serial: fresh buildx builder to force re-run (not just cache hit)
  docker buildx rm repro-b 2>/dev/null || true
  docker buildx create --name repro-b --use
  rm -rf docker/out/*.oci.tar || true
  SOURCE_DATE_EPOCH="${SDE}" docker/build-images.sh ${IMAGES}
  for img in docker/out/*.oci.tar; do
    cp "${img}" "${WORK_B}/"
  done
  docker buildx rm repro-b
fi

# --- diff --------------------------------------------------------
echo "=== DIFF ==="
diff_count=0
first_mismatch=""
{
  echo "# Reproducibility proof — ${STAMP}"
  echo
  echo "- Mode: \`${MODE}\`$([ -n "${REMOTE}" ] && echo " (remote: \`${REMOTE}\`)")"
  echo "- SOURCE_DATE_EPOCH: \`${SDE}\` ($(date -u -r "${SDE}" '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || date -u -d @"${SDE}" '+%Y-%m-%dT%H:%M:%SZ'))"
  echo "- Images under test: $(echo "${IMAGES}" | wc -w | tr -d ' ')"
  echo
  echo "| Image | Builder A sha256 | Builder B sha256 | Match |"
  echo "|-------|------------------|------------------|-------|"

  for a in "${WORK_A}"/*.oci.tar; do
    base="$(basename "${a}")"
    b="${WORK_B}/${base}"
    [[ -f "${b}" ]] || { echo "| ${base} | $(shasum -a 256 "${a}" | awk '{print $1}') | **MISSING** | ❌ |"; diff_count=$((diff_count+1)); first_mismatch="${base} missing on builder B"; continue; }
    ha=$(shasum -a 256 "${a}" | awk '{print $1}')
    hb=$(shasum -a 256 "${b}" | awk '{print $1}')
    if [[ "${ha}" == "${hb}" ]]; then
      echo "| ${base} | \`${ha:0:16}…\` | \`${hb:0:16}…\` | ✅ |"
    else
      echo "| ${base} | \`${ha}\` | \`${hb}\` | ❌ |"
      diff_count=$((diff_count+1))
      [[ -z "${first_mismatch}" ]] && first_mismatch="${base}"

      # layer-by-layer dump of the diverged pair
      echo
      echo "## Layer diff — ${base}"
      echo
      echo '```'
      ldir_a="/tmp/repro-layer-A-${STAMP}-${base}"
      ldir_b="/tmp/repro-layer-B-${STAMP}-${base}"
      mkdir -p "${ldir_a}" "${ldir_b}"
      tar -xf "${a}" -C "${ldir_a}"
      tar -xf "${b}" -C "${ldir_b}"
      for layer in "${ldir_a}"/blobs/sha256/*; do
        lname=$(basename "${layer}")
        bl="${ldir_b}/blobs/sha256/${lname}"
        if [[ ! -f "${bl}" ]]; then
          # layer digests differ even though same filename pattern — list the full dirs
          echo "A layers:"; ls -1 "${ldir_a}/blobs/sha256/" | head
          echo "B layers:"; ls -1 "${ldir_b}/blobs/sha256/" | head
          break
        fi
      done
      diff <(ls -1 "${ldir_a}/blobs/sha256/" | sort) \
           <(ls -1 "${ldir_b}/blobs/sha256/" | sort) || true
      echo '```'
      rm -rf "${ldir_a}" "${ldir_b}"
    fi
  done
  echo
  if [[ "${diff_count}" -eq 0 ]]; then
    echo "## Outcome: **PASS ✓** — all images byte-identical."
  else
    echo "## Outcome: **FAIL ✗** — ${diff_count} image(s) diverged. First: \`${first_mismatch}\`."
    echo
    echo "## Common causes of divergence"
    echo
    echo "1. **Timestamp leak** — a tool that doesn't respect"
    echo "   \`SOURCE_DATE_EPOCH\` (checksum \`stat -c %Y\`, custom "
    echo "   \`$(date)\` output). Grep every Dockerfile for \`date\` /"
    echo "   \`\$(date)\` / \`now()\` / \`time()\` and either pin or elide."
    echo "2. **Non-deterministic iteration order** — Go map iteration,"
    echo "   Python dict pre-3.7, globs returning unsorted. Pin or sort."
    echo "3. **Base image drift** — \`apt-get upgrade\` or an un-pinned"
    echo "   base. All Dockerfiles must pin by sha256 digest."
    echo "4. **Random inputs** — \`uuidgen\`, \`openssl rand\`, "
    echo "   \`/dev/urandom\`. These must be seeded by \`SOURCE_DATE_EPOCH\`"
    echo "   or elided entirely."
    echo "5. **Go build id** — needs \`-ldflags='-buildid='\`."
    echo "6. **Host libc / kernel** — only possible under \`--remote\`"
    echo "   mode. Pin to the same base kernel + glibc major."
  fi
} > "${OUT_REPORT}"

echo
echo "report: ${OUT_REPORT}"
[[ "${diff_count}" -eq 0 ]] && exit 0 || exit 1
