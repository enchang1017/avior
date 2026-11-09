CREATE OR REPLACE FUNCTION public.adv_sql_positional(bigint)
 RETURNS text
 LANGUAGE sql
AS $function$SELECT email FROM member WHERE id = $1$function$

