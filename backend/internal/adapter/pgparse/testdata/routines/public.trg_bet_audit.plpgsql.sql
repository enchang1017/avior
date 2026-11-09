CREATE OR REPLACE FUNCTION public.trg_bet_audit()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
  IF NEW.status IS DISTINCT FROM OLD.status THEN
    INSERT INTO bet_audit (bet_id, old_status, new_status) VALUES (NEW.id, OLD.status, NEW.status);
  END IF;
  RETURN NEW;
END $function$

