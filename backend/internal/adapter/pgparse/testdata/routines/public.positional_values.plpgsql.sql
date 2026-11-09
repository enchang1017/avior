CREATE OR REPLACE FUNCTION public.positional_values()
 RETURNS void
 LANGUAGE plpgsql
AS $function$
BEGIN
  INSERT INTO legacy_log VALUES (1, 'x');
  INSERT INTO legacy_log SELECT * FROM legacy_log_2024;
END $function$

