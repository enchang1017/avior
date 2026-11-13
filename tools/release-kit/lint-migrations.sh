#!/usr/bin/env bash
#
# Refuses a migration that would hurt production. Step 2 of ADR-0004's pipeline.
#
# Four checks, because they know different things:
#
#   squawk              reads the DDL and knows which statements take which locks
#   transaction mode    mirrors what Avior refuses at submission (FEAT-017), so a file
#                       Avior will refuse is refused at the merge request instead
#   NOT NULL            no tool has it: a column made NOT NULL without a DEFAULT in the
#                       same migration. migrations/ is schema only, so the rows already
#                       there get their value from the DEFAULT or not at all
#   partition revokes   every CREATE TABLE ... PARTITION OF takes back what a blanket
#                       grant handed it (only when PARTITION_REVOKE_ROLES is set)
#
# What is refused blocks the merge request. What staging allows but production wants a
# decision on -- a non-concurrent CREATE INDEX -- is printed as advice for the approver,
# who can see how big the table is.
#
# Usage, from the database project's root:
#
#   <avior>/tools/release-kit/lint-migrations.sh                   what this change adds
#   <avior>/tools/release-kit/lint-migrations.sh migrations/x.sql  one file
set -euo pipefail

KIT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "${PROJECT_DIR:-$PWD}"
# shellcheck source=kit-config.sh
. "$KIT_DIR/kit-config.sh"

# Pinned by digest. squawk publishes no arm64 image, so it runs under emulation on an
# arm64 machine, which costs nothing for a linter reading a few kilobytes of SQL.
SQUAWK_IMAGE="${SQUAWK_IMAGE:-ghcr.io/sbdchd/squawk@sha256:6906f7808f2b9683ebc7ef263527d9ca449b5489368ad77409a879c462a832b8}"

# What this change adds, not what the directory holds. A migration already on the base
# branch has been through this; re-reading it every time would report the same findings
# forever and teach people to ignore them. Untracked files count: the point is to hear
# about a migration before committing it.
#
# A base that resolves to HEAD makes the diff empty for a reason that has nothing to do
# with the change, and an empty diff passes. That is refused rather than reported as
# "nothing added".
if [ "$#" -gt 0 ]; then
    FILES=("$@")
else
    if [ "$(git rev-parse "$LINT_BASE" 2>/dev/null)" = "$(git rev-parse HEAD)" ]; then
        echo "refusing to lint: base $LINT_BASE resolves to HEAD, so the diff is empty by" >&2
        echo "construction and this would pass without reading anything" >&2
        exit 1
    fi
    FILES=()
    while IFS= read -r line; do
        [ -n "$line" ] && FILES+=("$line")
    done < <(
        {
            git diff --name-only --diff-filter=A "${LINT_BASE}...HEAD" -- "$MIGRATIONS_DIR/*.sql" 2>/dev/null || true
            git ls-files --others --exclude-standard -- "$MIGRATIONS_DIR/*.sql" 2>/dev/null || true
        } | sort -u
    )
fi

KEPT=()
for file in ${FILES[@]+"${FILES[@]}"}; do
    exempt=""
    for base in ${LINT_BASELINE[@]+"${LINT_BASELINE[@]}"}; do
        if [ "$file" = "$base" ] || [ "$file" = "$MIGRATIONS_DIR/$base" ]; then exempt=1; fi
    done
    [ -z "$exempt" ] && KEPT+=("$file")
done
FILES=(${KEPT[@]+"${KEPT[@]}"})

strip_comments() {
    # perl rather than sed: `sed 's|--[^\n]*||'` reads the bracket as "not a backslash and
    # not the letter n", so it stops at the first n in the text -- the n of "none", in the
    # one line the transaction-mode rule turns on.
    perl -0777 -pe 's{/\*.*?\*/}{ }gs; s{--[^\n]*}{}g' "$1"
}

failed=0

echo "==> partitions revoke their privileges"
if [ "${#PARTITION_REVOKE_ROLES[@]}" -eq 0 ]; then
    echo "    PARTITION_REVOKE_ROLES is empty in $KIT_CONFIG; not checked"
else
    partition_revokes() {
        local file="$1" body name role missing=()
        body="$(strip_comments "$file")"
        for name in $(printf '%s' "$body" |
                grep -oiE 'create[[:space:]]+table[[:space:]]+(if[[:space:]]+not[[:space:]]+exists[[:space:]]+)?[a-z0-9_.]+[[:space:]]+partition[[:space:]]+of' |
                awk '{print $(NF-2)}'); do
            for role in "${PARTITION_REVOKE_ROLES[@]}"; do
                printf '%s' "$body" | grep -qiE "revoke[[:space:]]+all[[:space:]]+on[[:space:]]+(table[[:space:]]+)?$name[[:space:]]+from[[:space:]]+$role" ||
                    missing+=("$name from $role")
            done
        done
        if [ "${#missing[@]}" -gt 0 ]; then
            echo >&2
            echo "FAIL  $file creates partitions that do not take their privileges back:" >&2
            printf '      %s\n' "${missing[@]}" >&2
            echo "      Add REVOKE ALL ON <partition> FROM <role>; beside each partition." >&2
            return 1
        fi
    }
    # The source files every run, whatever the diff: verify-migrations.sh cannot see a
    # missing REVOKE, because on both of its lanes the grant runs before any partition
    # exists and the two lanes agree on a partition with no privileges either way.
    for file in "$PARTITIONS_DIR"/*.sql ${FILES[@]+"${FILES[@]}"}; do
        [ -e "$file" ] || continue
        partition_revokes "$file" || failed=1
    done
    [ "$failed" -eq 0 ] && echo "    every CREATE ... PARTITION OF has its REVOKE"
fi

if [ "${#FILES[@]}" -eq 0 ]; then
    if [ "$failed" -ne 0 ]; then
        echo "FAIL  these findings block the merge request" >&2
        exit 1
    fi
    echo "PASS  this change adds no migration that runs against a live database"
    exit 0
fi

declares_txmode_none() {
    grep -qiE '^[[:space:]]*--[[:space:]]*atlas:txmode[[:space:]]+none[[:space:]]*$' "$1"
}

# Written into the file by generate-migration.sh --drop-intended, so atlas.sum covers it:
# change the statement and the declaration does not survive with it.
declares_drop_intended() {
    grep -qiE '^[[:space:]]*--[[:space:]]*avior:drop-intended[[:space:]]+[^[:space:]]' "$1"
}

# A file squawk is answered about, by name, from release-kit.conf. squawk checks NOTHING
# on a matching file -- there is no per-rule-per-file switch -- which is why the answer
# is about one file rather than a rule switched off for all of them.
squawk_answer() {
    local name answer pattern
    name="$(basename "$1")"
    for answer in ${SQUAWK_ANSWERED[@]+"${SQUAWK_ANSWERED[@]}"}; do
        pattern="${answer%%|*}"
        # shellcheck disable=SC2254  # the pattern is a glob on purpose
        case "$name" in
            $pattern) printf '%s\n' "${answer#*|}"; return 0 ;;
        esac
    done
    return 1
}

PLAIN=()
DECLARED=()
DROPPERS=()

echo "==> squawk"
for file in "${FILES[@]}"; do
    if reason="$(squawk_answer "$file")"; then
        # Said out loud, because a silent skip reads exactly like a clean run.
        echo "    $file is ANSWERED in $KIT_CONFIG, not checked -- squawk skipped it entirely."
        echo "      $reason"
        continue
    fi
    if declares_drop_intended "$file"; then DROPPERS+=("$file"); continue; fi
    if declares_txmode_none "$file"; then DECLARED+=("$file"); else PLAIN+=("$file"); fi
done

# require-concurrent-index-creation is excluded as a decision, not a preference. Avior
# refuses CONCURRENTLY unless a file declares itself non-transactional, which squawk
# knows nothing about; left in, the two rules meet in the middle and no index can ship.
# ADR-0004 makes a plain CREATE INDEX something the approver decides, printed as advice
# below.
#
# require-enum-value-ordering is excluded because verify-migrations.sh compares every
# enum's values and order between the two lanes, which is where the order matters.
SQUAWK_EXCLUDE=prefer-robust-stmts,prefer-text-field,prefer-bigint-over-int,require-concurrent-index-creation,require-enum-value-ordering
squawk() {
    docker run --rm --platform linux/amd64 -v "$PWD:/work" -w /work "$SQUAWK_IMAGE" "$@"
}
if [ "${#PLAIN[@]}" -gt 0 ]; then
    squawk --exclude "$SQUAWK_EXCLUDE" "${PLAIN[@]}" || failed=1
fi
# A declared non-transactional file cannot carry the SET timeout preamble: PostgreSQL
# runs a multi-statement simple query in an implicit transaction, and CONCURRENTLY inside
# that fails. Avior sets the timeouts on the session instead, so those two rules are off
# for these files only.
if [ "${#DECLARED[@]}" -gt 0 ]; then
    squawk --exclude "$SQUAWK_EXCLUDE,require-lock-timeout,require-statement-timeout" \
        "${DECLARED[@]}" || failed=1
fi
# ban-drop-column relaxed for the declaring files and no others, so a second migration in
# the same change that drops a column without saying so is refused as it would have been.
if [ "${#DROPPERS[@]}" -gt 0 ]; then
    for file in "${DROPPERS[@]}"; do
        echo "    $file drops a column ON PURPOSE:" \
            "$(sed -nE 's/^[[:space:]]*--[[:space:]]*avior:drop-intended[[:space:]]+//Ip' "$file" | head -1)"
    done
    squawk --exclude "$SQUAWK_EXCLUDE,ban-drop-column" "${DROPPERS[@]}" || failed=1
fi

# The same shape as Avior's own detector: comments stripped first, so a commented-out
# CONCURRENTLY does not refuse a file and a real one cannot hide behind a comment marker.
echo "==> transaction mode"
for file in "${FILES[@]}"; do
    body="$(strip_comments "$file")"
    declared=""
    declares_txmode_none "$file" && declared=1

    concurrent=""
    printf '%s' "$body" | grep -qiE '\b(create[[:space:]]+(unique[[:space:]]+)?index[[:space:]]+concurrently|drop[[:space:]]+index[[:space:]]+concurrently|reindex[^;]*concurrently)\b' && concurrent=1

    statements="$(printf '%s' "$body" | tr ';' '\n' | grep -cE '[^[:space:]]' || true)"

    if [ -n "$concurrent" ] && [ -z "$declared" ]; then
        echo >&2
        echo "FAIL  $file runs CONCURRENTLY and does not declare it." >&2
        echo "      Avior applies each file in one transaction and PostgreSQL refuses" >&2
        echo "      CONCURRENTLY inside one. Put '-- atlas:txmode none' on a line of its own" >&2
        echo "      at the top." >&2
        failed=1
    elif [ -n "$declared" ] && [ -z "$concurrent" ]; then
        echo >&2
        echo "FAIL  $file declares -- atlas:txmode none and carries nothing that needs it." >&2
        echo "      The declaration gives up the apply's atomicity; one nobody meant is a" >&2
        echo "      guarantee dropped by accident." >&2
        failed=1
    elif [ -n "$declared" ] && [ "$statements" -ne 1 ]; then
        echo >&2
        echo "FAIL  $file declares -- atlas:txmode none and carries $statements statements." >&2
        echo "      It may carry exactly one, including when the other is only the SET" >&2
        echo "      timeout preamble. Avior sets the timeouts on the session itself." >&2
        failed=1
    fi
done

# Read per ALTER TABLE clause, not per line: atlas writes several changes to one table as
# one ALTER separated by top-level commas. CREATE TABLE is out of scope: a new table has
# no rows.
#
#   ADD [COLUMN] c ... NOT NULL     refused unless the clause carries DEFAULT, or is an
#                                   identity or generated column, which fills the rows
#                                   already there during the ADD
#   ALTER [COLUMN] c SET NOT NULL   refused unless the same file sets a DEFAULT on that
#                                   table and column
#
# SET DEFAULT does not fill a NULL already in the column, so SET NOT NULL can still fail
# on production. What this guarantees is that every row written after the change has a
# value, not that the change applies.
not_null_findings() {
    strip_comments "$1" | perl -0777 -ne '
use strict; use warnings;
my $sql = $_;
sub ident { my $x = shift; $x =~ s/^\s+|\s+$//g; return $x =~ /^"(.*)"$/s ? $1 : lc $x; }
sub split_top {
    my ($text, $sep) = @_;
    my @parts; my $buf = ""; my $depth = 0; my $i = 0; my $n = length $text;
    while ($i < $n) {
        my $c = substr($text, $i, 1);
        if ($c eq "\x27" || $c eq "\"") {
            my $j = index($text, $c, $i + 1);
            $j = $n - 1 if $j < 0;
            $buf .= substr($text, $i, $j - $i + 1); $i = $j + 1; next;
        }
        if ($c eq "\$" && substr($text, $i) =~ /^(\$(?:[A-Za-z_][A-Za-z_0-9]*)?\$)/) {
            my $tag = $1;
            my $k = index($text, $tag, $i + length $tag);
            my $end = $k < 0 ? $n : $k + length $tag;
            $buf .= substr($text, $i, $end - $i); $i = $end; next;
        }
        if    ($c eq "(") { $depth++ }
        elsif ($c eq ")") { $depth-- }
        elsif ($c eq $sep && $depth == 0) { push @parts, $buf; $buf = ""; $i++; next; }
        $buf .= $c; $i++;
    }
    push @parts, $buf;
    return @parts;
}
my $id = qr/(?:"[^"]+"|[A-Za-z_][A-Za-z_0-9\$]*)/;
my (%set_not_null, %set_default, @bad);
for my $stmt (split_top($sql, ";")) {
    next unless $stmt =~ /^\s*ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?(?:($id)\.)?($id)\s+(.*)$/is;
    my $table = (defined $1 ? ident($1) . "." : "") . ident($2);
    my $rest = $3;
    for my $cl (split_top($rest, ",")) {
        $cl =~ s/^\s+|\s+$//g;
        if ($cl =~ /^ADD\s+(?:COLUMN\s+)?(?:IF\s+NOT\s+EXISTS\s+)?($id)\s+(.*)$/is) {
            my ($col, $def) = ($1, $2);
            next if $col =~ /^(?:CONSTRAINT|PRIMARY|UNIQUE|CHECK|FOREIGN|EXCLUDE)$/i;
            next unless $def =~ /\bNOT\s+NULL\b/i;
            next if $def =~ /\bDEFAULT\b/i || $def =~ /\bGENERATED\b/i;
            push @bad, "$table." . ident($col) . "  ADD COLUMN ... NOT NULL with no DEFAULT";
        } elsif ($cl =~ /^ALTER\s+(?:COLUMN\s+)?($id)\s+SET\s+NOT\s+NULL$/is) {
            $set_not_null{"$table." . ident($1)} = 1;
        } elsif ($cl =~ /^ALTER\s+(?:COLUMN\s+)?($id)\s+SET\s+DEFAULT\b/is) {
            $set_default{"$table." . ident($1)} = 1;
        }
    }
}
for my $c (sort keys %set_not_null) {
    push @bad, "$c  SET NOT NULL with no SET DEFAULT on it in this migration" unless $set_default{$c};
}
print "$_\n" for @bad;
'
}

echo "==> NOT NULL rule"
not_null_failed=0
for file in "${FILES[@]}"; do
    findings="$(not_null_findings "$file")"
    [ -z "$findings" ] && continue
    if [ "$not_null_failed" -eq 0 ]; then
        echo >&2
        echo "FAIL  a migration makes a column NOT NULL without a DEFAULT on it:" >&2
    fi
    echo "      $file" >&2
    printf '%s\n' "$findings" | sed 's/^/        /' >&2
    not_null_failed=1
done

if [ "$not_null_failed" -ne 0 ]; then
    echo >&2
    echo "      Existing rows need a value, and nothing under $DATA_DIR/ reaches production. Give" >&2
    echo "      the column a DEFAULT in $TABLES_DIR/ and regenerate, so the same migration carries it." >&2
    failed=1
else
    echo "    every NOT NULL column has a DEFAULT"
fi

# Advice, not a refusal, printed last so it is what is still on screen. Whether a plain
# CREATE INDEX is milliseconds or an outage depends on how many rows production holds,
# and generation is offline by design; the approver in Avior can see the table.
echo "==> advice"
advice=0
for file in "${FILES[@]}"; do
    strip_comments "$file" | grep -qiE '\bcreate[[:space:]]+(unique[[:space:]]+)?index\b' || continue
    strip_comments "$file" | grep -qiE '\bconcurrently\b' && continue
    echo "    $file builds an index without CONCURRENTLY."
    echo "      The build blocks writes to the table until COMMIT and reads the WHOLE table,"
    echo "      even for a partial index. If anything earlier in the file took ACCESS"
    echo "      EXCLUSIVE, reads are blocked too. Right for a small table, an outage for a"
    echo "      large one -- the approver sees the size and decides."
    advice=$((advice + 1))
done
for file in "${FILES[@]}"; do
    strip_comments "$file" | grep -qiE '\bpartition[[:space:]]+of\b' || continue
    echo "    $file creates partitions of a live table."
    echo "      CREATE TABLE ... PARTITION OF takes ACCESS EXCLUSIVE on the parent and on its"
    echo "      DEFAULT partition, scans DEFAULT, and waits for every uncommitted INSERT into"
    echo "      the parent. Send it off-peak, with DEFAULT confirmed empty for the new range."
    advice=$((advice + 1))
done
[ "$advice" -eq 0 ] && echo "    nothing to say"

echo
if [ "$failed" -ne 0 ]; then
    echo "FAIL  these findings block the merge request" >&2
    exit 1
fi
echo "PASS  nothing refused"
