#!/usr/bin/env bash
#
# Generates the migration that takes production from where the history says it is to
# what the declarative files say it should be. Step 1 of ADR-0004's pipeline.
#
#   baseline = migrations/ replayed into an empty database
#   desired  = apply-schema.sh applied to an empty database
#   writes migrations/<timestamp>_<name>.sql, or nothing when there is no difference
#
# No production connection and no staging connection. The migration directory is the
# authoritative record of what production has been told to be, so generation is
# offline, deterministic, and reviewable in the same merge request as the schema change
# that caused it.
#
# WHY THE DESIRED STATE IS A DATABASE AND NOT THE FILES
#
# Pointed at the table files alone, the generator would see a desired state with no
# functions in it and propose dropping every one. Pointed at every folder, it would need
# the dependency order release-kit.conf already spells out. Building the desired state
# with apply-schema.sh settles both: the thing that reads the DDL is PostgreSQL itself.
#
# TABLES ARE DIFFED. EVERYTHING ELSE IS COPIED
#
# Atlas models tables, indexes and constraints; it does not model functions, grants or
# partitions. That is ADR-0004's dividing line arriving from the tool's side: an object
# that holds rows needs a derived path from its current shape to the new one, and an
# object that holds none reaches production as the file itself.
#
# So the second pass copies. A declarative file whose provenance header is absent from
# migrations/ has never been sent to production, and it becomes a migration verbatim.
# One run produces at most one migration per kind.
#
# WHAT THIS DOES NOT DECIDE
#
# A diff cannot tell a rename from a drop-and-add, cannot invent a backfill, and does not
# know that production wants CREATE INDEX CONCURRENTLY. Those come back as lint findings
# on the generated file; the answer goes beside the declarative file -- a backfill is a
# DEFAULT on the column, since Data/ is never copied -- and this is run again. The
# generated file is never edited by hand: an edit severs it from the file it was derived
# from, and atlas.sum stops it.
#
# THE TWO ANSWERS A HUMAN MAY GIVE THE LINTER
#
#   --drop-intended <reason>   squawk refuses DROP COLUMN on sight, and is right to: the
#                              drop somebody meant and the drop a rename left behind are
#                              the same statement. This writes `-- avior:drop-intended
#                              <reason>` into the generated file, and lint-migrations.sh
#                              relaxes ban-drop-column for that file and no other.
#
#   --not-valid <reason>       squawk refuses ADD CONSTRAINT ... CHECK that is not NOT
#                              VALID. Atlas cannot emit that form, so this rewrites what
#                              atlas wrote into ADD ... NOT VALID followed by VALIDATE,
#                              before atlas.sum is computed. Avior applies one file in one
#                              transaction and PostgreSQL holds every lock to COMMIT, so
#                              this does NOT shorten the ACCESS EXCLUSIVE hold -- measured,
#                              the two shapes cost the same. What it buys is a file squawk
#                              accepts without exempting it from every other rule. A reason
#                              passed here must not claim more than that.
#
# Both answers are written by the generator, so atlas.sum covers them and nothing is
# hand-edited, and both travel in the diff an approver reads.
#
# Usage, from the database project's root:
#
#   <avior>/tools/release-kit/generate-migration.sh add_accounts_sort_order
set -euo pipefail

KIT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "${PROJECT_DIR:-$PWD}"
# shellcheck source=kit-config.sh
. "$KIT_DIR/kit-config.sh"
# shellcheck source=throwaway-pg.sh
. "$KIT_DIR/throwaway-pg.sh"

NOT_VALID_REASON=""
DROP_REASON=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        --drop-intended)
            [ "$#" -ge 2 ] || { echo "--drop-intended needs a reason" >&2; exit 64; }
            DROP_REASON="$2"
            shift 2
            ;;
        --drop-intended=*)
            DROP_REASON="${1#--drop-intended=}"
            shift
            ;;
        --not-valid)
            [ "$#" -ge 2 ] || { echo "--not-valid needs a reason" >&2; exit 64; }
            NOT_VALID_REASON="$2"
            shift 2
            ;;
        --not-valid=*)
            NOT_VALID_REASON="${1#--not-valid=}"
            shift
            ;;
        *)
            break
            ;;
    esac
done

if [ "$#" -ne 1 ]; then
    echo "usage: $0 [--drop-intended <reason>] [--not-valid <reason>] <migration-name>" >&2
    exit 64
fi
NAME="$1"

case "$NAME" in
    *[!a-z0-9_]*|"")
        echo "migration name '$NAME' must be lower-case letters, digits and underscores" >&2
        exit 64
        ;;
esac

if [ -n "$DROP_REASON" ]; then
    case "$DROP_REASON" in
        *[!\ ]*) ;;
        *) echo "--drop-intended needs a reason somebody wrote, not blank space" >&2; exit 64 ;;
    esac
fi

if [ -n "$NOT_VALID_REASON" ]; then
    case "$NOT_VALID_REASON" in
        *[!\ ]*) ;;
        *) echo "--not-valid needs a reason somebody wrote, not blank space" >&2; exit 64 ;;
    esac
fi

mkdir -p "$MIGRATIONS_DIR"

# Pinned by digest rather than by tag: the atlas image publishes no immutable version
# tags, and a generator that changes under you turns a regeneration into review churn.
# atlas version v1.3.4.
ATLAS_IMAGE="${ATLAS_IMAGE:-arigaio/atlas@sha256:17c0144fae8f925752afde63f1ddb494663c34240955e29aa2f14d49fbca8f31}"
pg_start
pg_create_roles
pg_psql -d postgres -c "CREATE DATABASE desired" >/dev/null
pg_psql -d postgres -c "CREATE DATABASE atlas_dev" >/dev/null

echo "==> building the desired state with apply-schema.sh"
PGUSER=postgres PGDATABASE=desired PG_CONTAINER="$PG_NAME" "$KIT_DIR/apply-schema.sh" >/dev/null

# --user, or the container writes as root: every file atlas creates in migrations/ lands
# owned by root while the directory stays ours, and preamble() then fails to rewrite the
# file it just produced, leaving a half-written directory with two owners in it.
atlas() {
    docker run --rm --user "$(id -u):$(id -g)" \
        --network "$PG_NETWORK" -v "$PWD:/work" -w /work "$ATLAS_IMAGE" "$@"
}

# atlas.sum is how a hand-edited migration is caught. A directory without one is a
# directory nothing is protecting, so it is created rather than reported.
if [ ! -f "$MIGRATIONS_DIR/atlas.sum" ]; then
    echo "==> hashing the existing migrations"
    atlas migrate hash --dir "file://$MIGRATIONS_DIR"
fi

# The rewriter --not-valid asks for. perl and not sed, because the clause it has to find
# is one of several separated by top-level commas -- atlas writes ADD CONSTRAINT and ADD
# COLUMN in ONE ALTER -- and a comma inside the CHECK's parentheses, inside a quoted
# string or inside a dollar-quoted one is not a separator.
#
# The plain clauses are emitted FIRST and the constraint after them: a CHECK may name a
# column the same ALTER adds. The VALIDATEs come last.
#
# It refuses per constraint, not per run: after rewriting, it reads its own output back
# and exits 5 if any validating ADD CONSTRAINT ... CHECK survived -- a statement spread
# over two lines, or spelled ALTER TABLE ONLY.
#
#   0  rewrote at least one constraint in this file
#   3  this file carries no such constraint -- not an error, one run writes several files
#   4  a statement it will not touch (spread over lines, unterminated string)
#   5  something validating survived the rewrite
#
# CHECK constraints only. FOREIGN KEY and UNIQUE are left alone and reported as exit 3.
not_valid_filter() {
    perl -e 'use strict; use warnings;
my @out; my $rewrote = 0; my $lineno = 0;
while (my $line = <STDIN>) {
    $lineno++;
    chomp $line;
    unless ($line =~ /^ALTER TABLE\s+(\S+)\s+(.*)$/) { push @out, $line; next; }
    my ($table, $rest) = ($1, $2);
    unless ($rest =~ s/;$//) {
        print STDERR "REFUSED: line $lineno: an ALTER TABLE statement does not end on its own line.\n";
        print STDERR "         $line\n";
        exit 4;
    }
    my @clauses; my $buf = ""; my $depth = 0; my $i = 0; my $n = length($rest);
    while ($i < $n) {
        my $c = substr($rest, $i, 1);
        if ($c eq "'\''") {
            my $j = $i + 1;
            while ($j < $n) {
                if (substr($rest, $j, 1) eq "'\''") {
                    if ($j + 1 < $n && substr($rest, $j + 1, 1) eq "'\''") { $j += 2; next; }
                    last;
                }
                $j++;
            }
            if ($j >= $n) {
                print STDERR "REFUSED: line $lineno: unterminated quoted string.\n";
                print STDERR "         $line\n";
                exit 4;
            }
            $buf .= substr($rest, $i, $j - $i + 1); $i = $j + 1; next;
        }
        if ($c eq '\''$'\'' && substr($rest, $i) =~ /^(\$(?:[A-Za-z_][A-Za-z_0-9]*)?\$)/) {
            my $tag = $1;
            my $k = index($rest, $tag, $i + length($tag));
            if ($k < 0) {
                print STDERR "REFUSED: line $lineno: unterminated dollar-quoted string $tag.\n";
                print STDERR "         $line\n";
                exit 4;
            }
            my $end = $k + length($tag);
            $buf .= substr($rest, $i, $end - $i); $i = $end; next;
        }
        if    ($c eq '\''('\'') { $depth++ }
        elsif ($c eq '\'')'\'') { $depth-- }
        elsif ($c eq '\'','\'' && $depth == 0) { push @clauses, $buf; $buf = ""; $i++; next; }
        $buf .= $c; $i++;
    }
    push @clauses, $buf;
    s/^\s+|\s+$//g for @clauses;
    my (@plain, @checks);
    for my $cl (@clauses) {
        # Already NOT VALID: leave it alone, so running this twice is not two rewrites.
        if ($cl =~ /^ADD\s+CONSTRAINT\s+("[^"]+"|\S+)\s+CHECK\s*\(/i
            && $cl !~ /\bNOT\s+VALID\s*$/i) { push @checks, [$1, $cl]; }
        else { push @plain, $cl; }
    }
    unless (@checks) { push @out, $line; next; }
    push @out, "ALTER TABLE $table " . join(", ", @plain) . ";" if @plain;
    push @out, "ALTER TABLE $table $_->[1] NOT VALID;" for @checks;
    push @out, "ALTER TABLE $table VALIDATE CONSTRAINT $_->[0];" for @checks;
    $rewrote += scalar @checks;
}
# PER-CHECK, not per-run: anything still adding a validating CHECK was not reached.
my $ln = 0;
for my $l (@out) {
    $ln++;
    next if $l =~ /^\s*--/;
    next unless $l =~ /\bADD\s+CONSTRAINT\b.*\bCHECK\s*\(/i;
    next if $l =~ /\bNOT\s+VALID\b/i;
    print STDERR "REFUSED: a validating ADD CONSTRAINT ... CHECK was left unrewritten.\n";
    print STDERR "         $l\n";
    exit 5;
}
exit 3 unless $rewrote;
print "$_\n" for @out;
'
}

# Counts the FILES this flag rewrote something in. Whether every constraint inside a
# file was reached is the filter's own exit 5.
NOT_VALID_REWROTE=0

# Every generated migration starts with these, as a house rule rather than a judgement.
# ALTER TABLE takes ACCESS EXCLUSIVE, and a blocked ACCESS EXCLUSIVE queues every later
# reader behind it, so one slow ALTER stops SELECT as well. Failing fast leaves a release
# to be retried; forming a queue takes the database down.
preamble() {
    local file="$1" body drops="" notvalid=""
    body="$(cat "$file")"

    if [ -n "$NOT_VALID_REASON" ]; then
        local rewritten status
        set +e
        rewritten="$(printf '%s\n' "$body" | not_valid_filter)"
        status=$?
        set -e
        case "$status" in
            0) body="$rewritten"; notvalid=1; NOT_VALID_REWROTE=$((NOT_VALID_REWROTE + 1)) ;;
            3) : ;;
            *) echo "generate-migration.sh: --not-valid refused to rewrite $file" >&2
               echo "       (the filter printed which statement, above)" >&2
               exit 65 ;;
        esac
    fi

    # Decided BEFORE the block below: `{ ... } > "$file"` truncates the file when the
    # redirection is set up, so a grep inside the block would read an empty file.
    #
    # Written only where the run asked for it AND the file drops something. A
    # declaration on a file that drops nothing would sit there until somebody copied it
    # onto one that does.
    if [ -n "$DROP_REASON" ] && printf '%s' "$body" | grep -qiE '\bdrop[[:space:]]+column\b'; then
        drops=1
    fi

    {
        echo "SET lock_timeout = '5s';"
        echo "SET statement_timeout = '30min';"
        if [ -n "$drops" ]; then
            echo
            echo "-- avior:drop-intended $DROP_REASON"
        fi
        if [ -n "$notvalid" ]; then
            echo
            echo "-- avior:not-valid $NOT_VALID_REASON"
        fi
        echo
        echo "$body"
    } > "$file"
}

before="$(ls -1 "$MIGRATIONS_DIR"/*.sql 2>/dev/null | sort || true)"

echo "==> tables: diffing"
atlas migrate diff "$NAME" \
    --dir "file://$MIGRATIONS_DIR" \
    --to "$(pg_dsn desired)" \
    --dev-url "$(pg_dsn atlas_dev)"

# The copied migrations sort after anything atlas just wrote: a function added in the
# same change may reference a column added in the same change.
STAMP="$(date -u +%Y%m%d%H%M%S)"
newest="$(ls -1 "$MIGRATIONS_DIR"/*.sql 2>/dev/null | sed 's|.*/||' | cut -d_ -f1 | sort | tail -1 || true)"
if [ -n "$newest" ] && [ "$newest" \> "$STAMP" ]; then
    STAMP="$newest"
fi

# A declarative file is in the ledger when its provenance header is. Copying a file in
# writes that header, so the check and the write are the same fact. -x matches the WHOLE
# line: a substring match takes a comment that merely mentions a file for its header,
# and the file is then silently never copied.
already_sent() {
    grep -qxF -- "-- $1/$2.sql" "$MIGRATIONS_DIR"/*.sql 2>/dev/null
}

copy_folder() {
    local folder="$1" suffix="$2" offset="$3"; shift 3

    local pending=() name
    for name in "$@"; do
        already_sent "$folder" "$name" || pending+=("$name")
    done
    if [ "${#pending[@]}" -eq 0 ]; then
        return 0
    fi

    local out
    out="$MIGRATIONS_DIR/$((STAMP + offset))_${NAME}_${suffix}.sql"
    : > "$out"
    for name in "${pending[@]}"; do
        printf -- "-- %s/%s.sql\n" "$folder" "$name" >> "$out"
        cat "$folder/$name.sql" >> "$out"
        printf "\n" >> "$out"
    done
    echo "    $out  (${#pending[@]})"
}

echo "==> everything else: copying what has never been sent"
# Data/ is not copied: migrations/ is schema only. Its rows reach local, CI and staging
# through apply-schema.sh, and production none. Partitions/ follows the grants, for the
# reason apply-schema.sh gives.
copy_folder "$PROCEDURES_DIR" "routines" 1 ${PROCEDURES_ORDER[@]+"${PROCEDURES_ORDER[@]}"}
copy_folder "$SECURITY_DIR" "grants" 2 ${SECURITY_AFTER_TABLES_ORDER[@]+"${SECURITY_AFTER_TABLES_ORDER[@]}"}
copy_folder "$PARTITIONS_DIR" "partitions" 3 ${PARTITIONS_ORDER[@]+"${PARTITIONS_ORDER[@]}"}

for file in $(ls -1 "$MIGRATIONS_DIR"/*.sql 2>/dev/null | sort || true); do
    case "$before" in
        *"$file"*) ;;
        *) preamble "$file" ;;
    esac
done

if [ -n "$NOT_VALID_REASON" ] && [ "$NOT_VALID_REWROTE" -eq 0 ]; then
    # Hashed BEFORE failing. A run that exits having written a file and NOT hashed it
    # leaves the directory looking exactly like one somebody edited by hand.
    atlas migrate hash --dir "file://$MIGRATIONS_DIR" >/dev/null 2>&1 || true
    echo "generate-migration.sh: --not-valid was asked for and this run produced no" >&2
    echo "       ADD CONSTRAINT ... CHECK to rewrite. Nothing was changed by the flag." >&2
    echo "       Only CHECK constraints are rewritten; FOREIGN KEY and UNIQUE are not." >&2
    echo "       $MIGRATIONS_DIR/ has been hashed, so what this run wrote is there and is not" >&2
    echo "       mistakable for a hand-edited file. Remove it if it was not wanted." >&2
    exit 65
fi

# Rehashed at the end rather than after each write: atlas.sum is what refuses a
# hand-edited migration.
atlas migrate hash --dir "file://$MIGRATIONS_DIR"

echo
echo "==> $MIGRATIONS_DIR/ now holds"
ls -1 "$MIGRATIONS_DIR"/
