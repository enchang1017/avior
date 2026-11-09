CREATE OR REPLACE FUNCTION public.every_execute(p text)
 RETURNS SETOF bigint
 LANGUAGE plpgsql
AS $function$
DECLARE r record; c refcursor;
BEGIN
  EXECUTE 'DELETE FROM ' || p;
  FOR r IN EXECUTE 'SELECT id FROM bet' LOOP
    NULL;
  END LOOP;
  RETURN QUERY EXECUTE format('SELECT id FROM %I', p);
  OPEN c FOR EXECUTE 'SELECT id FROM member';
  CLOSE c;
END $function$

