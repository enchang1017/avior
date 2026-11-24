#!/usr/bin/env bash
# Runs a command while holding a lock shared by every Avior checkout on this
# machine.
#
#   exclusive.sh <lock-name> <command> [args...]
#
# A performance budget measured while another worktree's browser runs is a
# measure of both, so the budgets take this lock and wait their turn.
#
# The exclusion is a kernel advisory lock, flock(2), on avior-<name>.flock,
# taken through python3 because macOS ships no flock command. The lock belongs
# to this shell's descriptor 9, so the kernel drops it when this shell dies,
# however it dies. That file is never removed: a run that unlinked it would let
# the next one lock a fresh inode while a waiter still holds the old one.
#
# avior-<name>.lock beside it is only a label holding the owner's PID and stack,
# so a waiting run can name who it waits for. A label found by a run that
# already holds the flock was left by a run that died, and is taken over, or
# belongs to a checkout still on the mkdir-only lock, and is waited for.
set -euo pipefail

usage="usage: exclusive.sh <lock-name> <command> [args...]"
name="${1:?$usage}"
shift
[[ "$#" -gt 0 ]] || { echo "$usage" >&2; exit 64; }

dir="${AVIOR_LOCK_DIR:-/tmp}"
lock="$dir/avior-$name.lock"
flockfile="$dir/avior-$name.flock"
stack="${AVIOR_STACK:-unknown}"
timeout="${AVIOR_LOCK_TIMEOUT:-1800}"
mine="$$ $stack"
# A label still missing its owner this many seconds after it appeared means
# its creator died between the two steps.
unowned_grace=10
busy=75

trace() {
  if [[ -n "${AVIOR_LOCK_TRACE:-}" ]]; then
    echo "trace pid=$$ stack=$stack waited=${waited:-0}s: $*" >&2
  fi
}

owner_of() { cat "$1/owner" 2>/dev/null || true; }

labelled=""
release() {
  if [[ -n "$labelled" ]]; then
    trace "release $lock"
    rm -rf "$lock"
  fi
}
trap release EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

try_flock() {
  python3 -I -c '
import errno, fcntl, sys
try:
    fcntl.flock(9, fcntl.LOCK_EX | fcntl.LOCK_NB)
except OSError as err:
    if err.errno in (errno.EWOULDBLOCK, errno.EAGAIN):
        sys.exit(int(sys.argv[1]))
    print("exclusive: flock on %s failed: %s" % (sys.argv[2], err), file=sys.stderr)
    sys.exit(1)
' "$busy" "$flockfile"
}

waited=0
announced=""
announce() {
  local holder="$1"
  if [[ -n "$holder" && "$announced" != "$holder" ]]; then
    echo "exclusive: waiting for ${holder#* } (pid ${holder%% *}) to release $name"
    announced="$holder"
  fi
}

tick() {
  if (( waited >= timeout )); then
    echo "FAIL  exclusive: waited ${timeout}s for $lock, held by '${1:-nobody}'" >&2
    exit 1
  fi
  sleep 1
  waited=$((waited + 1))
}

command -v python3 >/dev/null 2>&1 || {
  echo "FAIL  exclusive: python3 is needed to take the advisory lock on $flockfile" >&2
  exit 69
}
exec 9>>"$flockfile" || {
  echo "FAIL  exclusive: cannot open $flockfile" >&2
  exit 69
}
trace "start lock=$lock flock=$flockfile uname=$(uname -sr) bash=$BASH_VERSION"

while :; do
  status=0
  try_flock || status=$?
  (( status == 0 )) && break
  if (( status != busy )); then
    echo "FAIL  exclusive: cannot take the advisory lock on $flockfile" >&2
    exit 69
  fi
  holder="$(owner_of "$lock")"
  announce "$holder"
  tick "$holder"
done
trace "flock held on $flockfile"

unowned=0
while ! mkdir "$lock" 2>/dev/null; do
  if [[ ! -d "$lock" ]]; then
    echo "FAIL  exclusive: cannot create $lock" >&2
    exit 69
  fi
  holder="$(owner_of "$lock")"
  pid="${holder%% *}"
  if [[ -z "$holder" ]]; then
    unowned=$((unowned + 1))
    if (( unowned > unowned_grace )); then
      echo "exclusive: $lock has had no owner for ${unowned_grace}s; taking it over" >&2
      rm -rf "$lock"
      unowned=0
      continue
    fi
  elif ! reason="$(kill -0 "$pid" 2>&1)"; then
    echo "exclusive: $lock was left by pid $pid (owner '$holder'), which is gone: ${reason:-kill gave no reason}; taking it over" >&2
    rm -rf "$lock"
    continue
  else
    unowned=0
    announce "$holder"
  fi
  tick "$holder"
done
labelled=1
printf '%s\n' "$mine" > "$lock/owner.$$"
mv "$lock/owner.$$" "$lock/owner"
trace "acquired, owner [$(owner_of "$lock")]"

status=0
"$@" 9>&- || status=$?
exit "$status"
