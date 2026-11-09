CREATE OR REPLACE FUNCTION public.adv_record(p bigint)
 RETURNS void
 LANGUAGE plpgsql
AS $function$
DECLARE rec member%ROWTYPE;
BEGIN
  SELECT * INTO rec FROM member WHERE id = p;
  UPDATE bet SET stake = rec.balance WHERE member_id = rec.id;
END $function$

