 SELECT date_trunc('day'::text, settled_at) AS day,
    sum(stake) AS stake
   FROM bet
  GROUP BY (date_trunc('day'::text, settled_at));
