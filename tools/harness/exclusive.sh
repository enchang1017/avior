#!/usr/bin/env bash
# Runs a command while holding a lock shared by every Avior checkout on this
# machine.
#
#   exclusive.sh <lock-name> <command> [args...]
#
# A performance budget measured while another worktree's browser runs is a
# measure of both, so the budgets take this lock and wait their turn.
#
# macOS ships no flock, so the lock is a directory: mkdir is atomic everywhere.
# It holds the owner's PID and stack. A lock whose PID is gone was left by a
# run that died, and is taken over rather than waited on forever.
set -euo pipefail

name="${1:?usage: exclusive.sh <lock-name> <command> [args...]}"
shift
[[ "$#" -gt 0 ]] || { echo "usage: exclusive.sh <lock-name> <command> [args...]" >&2; exit 64; }

dir="${AVIOR_LOCK_DIR:-/tmp}"
lock="$dir/avior-$name.lock"
stack="${AVIOR_STACK:-unknown}"
timeout="${AVIOR_LOCK_TIMEOUT:-1800}"
# An owner file still missing this many seconds after the directory appeared
# means its creator died between the two steps.
unowned_grace=10

release() { rm -rf "$lock"; }

owner_of() { cat "$1/owner" 2>/dev/null || true; }

take_over_stale() {
  local seen="$1" moved="$lock.stale.$$"
  mv "$lock" "$moved" 2>/dev/null || return 0
  if [[ "$(owner_of "$moved")" == "$seen" ]]; then
    rm -rf "$moved"
    return 0
  fi
  # Somebody took the lock between our read and our move. Put theirs back.
  mv "$moved" "$lock" 2>/dev/null || rm -rf "$moved"
}

waited=0
unowned=0
announced=""
while ! mkdir "$lock" 2>/dev/null; do
  holder="$(owner_of "$lock")"
  pid="${holder%% *}"
  if [[ -z "$holder" ]]; then
    unowned=$((unowned + 1))
    if (( unowned > unowned_grace )); then
      echo "exclusive: $lock has had no owner for ${unowned_grace}s; taking it over" >&2
      take_over_stale ""
      unowned=0
    fi
  elif ! kill -0 "$pid" 2>/dev/null; then
    echo "exclusive: $lock was left by pid $pid, which is gone; taking it over" >&2
    take_over_stale "$holder"
    continue
  else
    unowned=0
    if [[ "$announced" != "$holder" ]]; then
      echo "exclusive: waiting for ${holder#* } (pid $pid) to release $name"
      announced="$holder"
    fi
  fi
  if (( waited >= timeout )); then
    echo "FAIL  exclusive: waited ${timeout}s for $lock, held by '${holder:-nobody}'" >&2
    exit 1
  fi
  sleep 1
  waited=$((waited + 1))
done

trap release EXIT
trap 'release; exit 130' INT
trap 'release; exit 143' TERM
echo "$$ $stack" > "$lock/owner"

status=0
"$@" || status=$?
exit "$status"
