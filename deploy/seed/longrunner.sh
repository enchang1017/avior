#!/bin/sh
# Local development only. Two sessions that each hold one honest, slow
# statement open for minutes and then start another, so the live panel and the
# long-running count have something real to show. Reconnects after the seed
# drops the database under it.
set -u

run() {
  seconds=$1
  while true; do
    psql "$LONGRUNNER_DSN" -qAt -v ON_ERROR_STOP=1 \
      -c "SELECT pg_sleep($seconds), count(*) FROM public.round r JOIN public.player p USING (player_id)" \
      >/dev/null 2>&1 || sleep 10
    sleep 3
  done
}

run 330 &
run 540 &
wait
