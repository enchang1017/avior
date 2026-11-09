CREATE OR REPLACE FUNCTION public.default_currency()
 RETURNS text
 LANGUAGE sql
 IMMUTABLE
AS $function$ SELECT 'THB' $function$

