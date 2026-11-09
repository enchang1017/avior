CREATE OR REPLACE FUNCTION public.bet_defaults()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
  NEW.status := coalesce(NEW.status, 'open');
  RETURN NEW;
END $function$

