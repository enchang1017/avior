CREATE OR REPLACE FUNCTION public.purge_table(p_table text)
 RETURNS bigint
 LANGUAGE plpgsql
AS $function$
DECLARE n bigint;
BEGIN
  EXECUTE format('DELETE FROM %I WHERE closed', p_table);
  GET DIAGNOSTICS n = ROW_COUNT;
  EXECUTE 'SELECT count(*) FROM member' INTO n;
  RETURN n;
END $function$

