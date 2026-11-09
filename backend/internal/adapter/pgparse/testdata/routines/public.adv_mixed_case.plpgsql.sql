CREATE OR REPLACE FUNCTION public.adv_mixed_case()
 RETURNS void
 LANGUAGE plpgsql
AS $function$
BEGIN
  INSERT INTO "MixedCase" ("Id", "Val") SELECT id, name FROM member;
END $function$

