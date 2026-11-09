CREATE OR REPLACE FUNCTION public.promo_touch()
 RETURNS void
 LANGUAGE plpgsql
 SET search_path TO 'promo', 'public'
AS $function$
BEGIN
  UPDATE campaign SET title = upper(title)
  WHERE EXISTS (SELECT 1 FROM grant_log g WHERE g.campaign_id = campaign.id);
END $function$

