CREATE OR REPLACE FUNCTION public.adv_order_alias()
 RETURNS TABLE(x numeric)
 LANGUAGE plpgsql
AS $function$
BEGIN
  RETURN QUERY SELECT m.balance AS stake FROM member m JOIN bet b ON b.member_id = m.id ORDER BY stake;
END $function$

