CREATE OR REPLACE FUNCTION public.all_members()
 RETURNS SETOF member
 LANGUAGE plpgsql
AS $function$
BEGIN
  RETURN QUERY SELECT * FROM member;
END $function$

