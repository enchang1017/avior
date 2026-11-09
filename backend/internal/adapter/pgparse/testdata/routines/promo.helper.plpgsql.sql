CREATE OR REPLACE FUNCTION promo.helper()
 RETURNS bigint
 LANGUAGE plpgsql
AS $function$
BEGIN
  RETURN (SELECT count(*) FROM promo.campaign);
END $function$

