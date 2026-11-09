CREATE OR REPLACE FUNCTION public.adv_const_exec()
 RETURNS void
 LANGUAGE plpgsql
AS $function$
DECLARE r record;
BEGIN
  EXECUTE 'UPDATE member SET closed = true';
  FOR r IN EXECUTE 'SELECT id FROM bet' LOOP
    NULL;
  END LOOP;
END $function$

