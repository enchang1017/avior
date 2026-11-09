 SELECT member_id,
    name,
    stake
   FROM member_bets
  WHERE ((status = 'open'::text) AND (stake > (1000)::numeric));
