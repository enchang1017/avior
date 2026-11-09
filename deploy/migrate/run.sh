#!/bin/sh
# Applies the store migrations, with the password read from a file rather than
# carried in the DSN.
#
# The password is mounted by the Secrets Store CSI driver and never appears in
# the connection string, so a failure that prints the DSN — goose does, and so
# does the driver underneath it — prints a URL with no credential in it.
set -eu

: "${AVIOR_STORE_DSN:?AVIOR_STORE_DSN is not set}"

if [ -n "${AVIOR_STORE_PASSWORD_FILE:-}" ]; then
	if [ ! -r "$AVIOR_STORE_PASSWORD_FILE" ]; then
		echo "AVIOR_STORE_PASSWORD_FILE=$AVIOR_STORE_PASSWORD_FILE is not readable" >&2
		exit 1
	fi
	# Trailing newline stripped: a value pasted into OpenBao usually carries one,
	# and the driver mounts the bytes exactly as they were stored. Left on, the
	# symptom is an authentication failure against a password that looks correct.
	PGPASSWORD="$(tr -d '\r\n' < "$AVIOR_STORE_PASSWORD_FILE")"
	export PGPASSWORD
fi

exec goose -dir /migrations postgres "$AVIOR_STORE_DSN" "$@"
