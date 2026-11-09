CREATE OR REPLACE FUNCTION public.adv_cursor_assign()
 RETURNS void
 LANGUAGE plpgsql
AS $function$
DECLARE
  c CURSOR FOR SELECT name FROM member WHERE closed;
  v numeric;
  rec record;
BEGIN
  v := (SELECT max(stake) FROM bet);
  OPEN c; FETCH c INTO rec; CLOSE c;
  RAISE NOTICE '% %', v, (SELECT count(*) FROM account);
  UPDATE member SET (name, email) = (SELECT 'x', 'y') WHERE id = 1;
END $function$

