CREATE OR REPLACE FUNCTION public.broken_sql()
 RETURNS integer
 LANGUAGE sql
AS $function$ SELECT 1 $function$

