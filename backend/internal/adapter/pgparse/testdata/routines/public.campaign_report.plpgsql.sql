CREATE OR REPLACE FUNCTION public.campaign_report(p_campaign integer)
 RETURNS TABLE(member_name text, total numeric)
 LANGUAGE plpgsql
AS $function$
BEGIN
  RETURN QUERY
    SELECT mb.name, sum(g.amount)
    FROM promo.grant_log g JOIN member_bets mb ON mb.member_id = g.member_id
    WHERE g.campaign_id = p_campaign
    GROUP BY mb.name;
END $function$

