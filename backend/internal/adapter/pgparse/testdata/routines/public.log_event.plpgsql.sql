CREATE OR REPLACE FUNCTION public.log_event(p_kind text, p_ref bigint)
 RETURNS void
 LANGUAGE plpgsql
AS $function$
BEGIN
  INSERT INTO legacy_log (id, msg) VALUES (p_ref, p_kind);
END $function$

