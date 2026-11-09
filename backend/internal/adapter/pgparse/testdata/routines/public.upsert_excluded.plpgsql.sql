CREATE OR REPLACE FUNCTION public.upsert_excluded(p bigint)
 RETURNS void
 LANGUAGE plpgsql
AS $function$
BEGIN
  INSERT INTO account (id, member_id, currency) VALUES (p, p, 'THB')
  ON CONFLICT (id) DO UPDATE SET currency = EXCLUDED.currency, member_id = account.member_id
  WHERE account.currency <> EXCLUDED.currency;
END $function$

