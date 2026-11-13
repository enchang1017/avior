#!/usr/bin/env bash
#
# Loads the database project's release-kit.conf and refuses one that would let a
# script run on a guess.
#
# Sourced by every script in this kit after it has changed into the project
# directory. The kit lives in Avior and the configuration lives in the database
# project, so nothing project-specific is ever written into a script here: table
# order, role names and linter answers are the project's facts, and the kit reads
# them rather than restating them.
#
# Every list may be empty. A project with no partitions or no data files says so
# with an empty array, and every loop in the kit tolerates one -- bash 3.2, which
# macOS still ships, treats "${empty[@]}" under set -u as an error rather than as
# zero iterations, so the kit expands lists as ${list[@]+"${list[@]}"}.

KIT_CONFIG="${KIT_CONFIG:-release-kit.conf}"

if [ ! -f "$KIT_CONFIG" ]; then
    echo "ERROR: no $KIT_CONFIG in $(pwd)." >&2
    echo "       Run the kit from the database project's root, or set PROJECT_DIR to it." >&2
    echo "       tools/release-kit/release-kit.conf.example is the template." >&2
    exit 64
fi

SECURITY_ORDER=()
SECURITY_AFTER_TABLES_ORDER=()
TABLES_ORDER=()
DATA_ORDER=()
PROCEDURES_ORDER=()
PARTITIONS_ORDER=()
APP_ROLES=()
PARTITION_REVOKE_ROLES=()
LINT_BASELINE=()
SQUAWK_ANSWERED=()

# LINT_BASE is the one setting CI decides per pipeline -- the merge request's target, or
# what the branch pointed at before the push -- so a value in the environment wins over
# the project's default rather than being overwritten by it.
_lint_base_from_env="${LINT_BASE:-}"

# shellcheck source=release-kit.conf.example
. "./$KIT_CONFIG"

if [ -n "$_lint_base_from_env" ]; then
    LINT_BASE="$_lint_base_from_env"
fi
unset _lint_base_from_env

: "${DATABASE:?$KIT_CONFIG must set DATABASE, the governed database name Avior knows the target by}"

TABLES_DIR="${TABLES_DIR:-Tables}"
PROCEDURES_DIR="${PROCEDURES_DIR:-Stored Procedures}"
SECURITY_DIR="${SECURITY_DIR:-Security}"
DATA_DIR="${DATA_DIR:-Data}"
PARTITIONS_DIR="${PARTITIONS_DIR:-Partitions}"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-migrations}"
LINT_BASE="${LINT_BASE:-origin/main}"

if [ "${#TABLES_ORDER[@]}" -eq 0 ]; then
    echo "ERROR: $KIT_CONFIG lists no tables in TABLES_ORDER." >&2
    echo "       A project with no tables has nothing for the generator to diff." >&2
    exit 64
fi

# An answer with no reason is the log line a reader takes for a clean run, so it is
# refused here, before any linter is skipped on its account.
for _answer in ${SQUAWK_ANSWERED[@]+"${SQUAWK_ANSWERED[@]}"}; do
    case "$_answer" in
        *"|"*[!\ ]*) ;;
        *)
            echo "ERROR: SQUAWK_ANSWERED entry '$_answer' carries no reason." >&2
            echo "       Write it as 'file-name-pattern|why squawk is wrong about this file'." >&2
            exit 64
            ;;
    esac
done
unset _answer
