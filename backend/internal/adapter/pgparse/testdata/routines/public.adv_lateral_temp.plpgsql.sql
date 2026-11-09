CREATE OR REPLACE FUNCTION public.adv_lateral_temp()
 RETURNS void
 LANGUAGE plpgsql
AS $function$
BEGIN
  CREATE TEMP TABLE IF NOT EXISTS tmp_totals AS SELECT member_id, sum(stake) AS total FROM bet GROUP BY member_id;
  INSERT INTO ledger (member_id, amount, kind, booked_on)
  SELECT t.member_id, t.total, 'x', current_date
  FROM tmp_totals t, LATERAL (SELECT 1 FROM member m WHERE m.id = t.member_id) x;
END $function$

