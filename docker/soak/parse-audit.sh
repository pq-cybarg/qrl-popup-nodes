#!/usr/bin/env bash
# parse-audit.sh — collect every syscall number that fired for a
# given compose project, from both the host kernel audit stream and
# the container stderr. Emits one syscall per line, sorted/unique.
#
# Sources (first wins):
#   1. journalctl -k --output=cat (Linux systemd hosts)
#   2. ausearch --raw           (Linux auditd hosts)
#   3. docker logs with EPERM/seccomp patterns (Docker Desktop fallback)
#
# Usage:  parse-audit.sh <compose-project>
set -Eeuo pipefail
PROJECT="${1:-qrl-popup}"

emit_syscall() {
  # match: type=1326 … syscall=NNN …
  awk 'match($0, /syscall=[0-9]+/) {
         s=substr($0, RSTART+8, RLENGTH-8); print s
       }
       match($0, /SECCOMP[^0-9]+([0-9]+)/, m) { print m[1] }'
}

tried_any=0

if command -v journalctl >/dev/null 2>&1 && [[ "$(uname -s)" == "Linux" ]]; then
  tried_any=1
  journalctl -k --since "1 hour ago" --output=cat 2>/dev/null \
    | grep -E 'audit:.*type=1326|SECCOMP' | emit_syscall || true
fi

if command -v ausearch >/dev/null 2>&1; then
  tried_any=1
  ausearch -m SECCOMP --start recent --raw 2>/dev/null \
    | grep -E 'type=1326' | emit_syscall || true
fi

# container-side fallback — a syscall number isn't usually in container
# stderr but EPERM lines ARE, and the exercise script name tells us
# which code path triggered it. We print "EPERM:<container>:<line>".
if [[ "$(uname -s)" != "Linux" ]] || [[ "${tried_any}" -eq 0 ]]; then
  for c in $(docker ps --filter "label=com.docker.compose.project=${PROJECT}" --format '{{.Names}}' 2>/dev/null); do
    docker logs --since 1h "${c}" 2>&1 \
      | grep -oE '(EPERM|Operation not permitted|operation not permitted|seccomp)[^\n]{0,200}' \
      | awk -v c="${c}" '{print "EPERM:" c ":" $0}' || true
  done
fi
