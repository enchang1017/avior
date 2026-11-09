CREATE OR REPLACE FUNCTION public.trg_old_new()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
  IF OLD.stake <> NEW.stake THEN
    NEW.payout := NEW.stake * 2;
  END IF;
  RETURN NEW;
END $function$

