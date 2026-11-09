CREATE OR REPLACE FUNCTION public.select_into_table()
 RETURNS void
 LANGUAGE sql
AS $function$ SELECT id, stake INTO TABLE copied_bet FROM bet $function$

