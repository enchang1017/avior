CREATE OR REPLACE FUNCTION public.select_into_record(p bigint)
 RETURNS text
 LANGUAGE plpgsql
AS $function$
DECLARE v text; rec record;
BEGIN
  SELECT name INTO v FROM member WHERE id = p;
  SELECT * INTO rec FROM bet WHERE id = p;
  RETURN v;
END $function$

