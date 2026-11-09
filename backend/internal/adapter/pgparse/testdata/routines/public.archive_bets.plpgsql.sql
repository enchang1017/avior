CREATE OR REPLACE FUNCTION public.archive_bets()
 RETURNS void
 LANGUAGE plpgsql
AS $function$
BEGIN
  INSERT INTO legacy_log SELECT b.id, b.status FROM bet b WHERE b.status = 'settled';
  DELETE FROM bet USING member m WHERE bet.member_id = m.id AND m.closed;
END $function$

