#!/usr/bin/env bash
# Enforces a per-package statement coverage floor.
#
# A repository-wide average lets a well-tested package hide an untested one, so
# the floor applies to every package that contains code.
#
# Three kinds of package are exempt, each for a stated reason:
#
#   internal/gen, internal/store/gen   Generated. Correctness comes from the
#                                      generator and the no-drift gate.
#   cmd/                               Process wiring, exercised by the stack
#                                      coming up at all.
#   internal/api, internal/store,      Transport translation, storage adapters,
#   internal/feature/*,                and application orchestration. Only a
#   internal/pgtarget
#                                      third-party boundary earns a dedicated
#                                      test; these are covered through the
#                                      acceptance suite against the real
#                                      databases. Their business rules live in
#                                      the domain packages, which are NOT exempt.
#                                      internal/pgtarget is connection pooling
#                                      and the reading of what PostgreSQL said
#                                      when it refused: both need a server to
#                                      mean anything. See docs/adr/0002.
#
# Adding an exemption is a change to the test strategy, not a convenience.
set -euo pipefail

profile="${1:?usage: coverage-floor.sh <profile> <floor>}"
floor="${2:?usage: coverage-floor.sh <profile> <floor>}"

if [[ ! -s "$profile" ]]; then
  echo "FAIL  coverage profile $profile is missing or empty"
  exit 1
fi

exempt_pattern='/internal/gen$|/internal/store/gen$|/internal/store$|/internal/api$|/internal/pgtarget$|/internal/feature/|/cmd/'

failed=0
while read -r pkg pct; do
  if [[ "$pkg" =~ $exempt_pattern ]]; then
    printf '  skip  %-58s %6s%%  (covered by the acceptance suite)\n' "$pkg" "$pct"
    continue
  fi
  if awk -v a="$pct" -v b="$floor" 'BEGIN { exit !(a + 0 < b + 0) }'; then
    printf '  FAIL  %-58s %6s%%  < %s%%\n' "$pkg" "$pct" "$floor"
    failed=1
  else
    printf '  ok    %-58s %6s%%\n' "$pkg" "$pct"
  fi
done < <(
  go tool cover -func="$profile" \
    | grep -v '^total:' \
    | awk '{
        pkg = $1
        sub(/\/[^\/]*\.go:[0-9]+:$/, "", pkg)
        gsub("%", "", $NF)
        sum[pkg] += $NF
        cnt[pkg] += 1
      }
      END { for (p in sum) printf "%s %.1f\n", p, sum[p] / cnt[p] }' \
    | sort
)

if [[ "$failed" -ne 0 ]]; then
  echo
  echo "FAIL  at least one package is below the ${floor}% coverage floor"
  exit 1
fi
echo "PASS  every package meets the ${floor}% coverage floor"
