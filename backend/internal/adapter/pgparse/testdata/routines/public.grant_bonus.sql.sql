CREATE OR REPLACE PROCEDURE public.grant_bonus(IN p_campaign integer, IN p_member bigint, IN p_amount numeric)
 LANGUAGE sql
BEGIN ATOMIC
 INSERT INTO promo.grant_log (campaign_id, member_id, amount)
   VALUES (grant_bonus.p_campaign, grant_bonus.p_member, grant_bonus.p_amount);
 UPDATE promo.campaign SET budget = (campaign.budget - grant_bonus.p_amount)
   WHERE (campaign.id = grant_bonus.p_campaign);
END

