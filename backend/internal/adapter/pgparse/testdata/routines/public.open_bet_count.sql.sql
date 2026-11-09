CREATE OR REPLACE FUNCTION public.open_bet_count(p_member bigint)
 RETURNS bigint
 LANGUAGE sql
 STABLE
AS $function$SELECT count(*) FROM bet WHERE member_id = p_member AND status = 'open'$function$

