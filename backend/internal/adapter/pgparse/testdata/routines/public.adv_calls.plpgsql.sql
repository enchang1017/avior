CREATE OR REPLACE PROCEDURE public.adv_calls()
 LANGUAGE plpgsql
AS $procedure$
BEGIN
  PERFORM promo.helper();
  CALL close_member(1);
END $procedure$

