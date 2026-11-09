CREATE OR REPLACE FUNCTION public.upsert_account(p_id bigint, p_member bigint, p_cur text)
 RETURNS void
 LANGUAGE plpgsql
AS $function$
BEGIN
  INSERT INTO account (id, member_id, currency) VALUES (p_id, p_member, p_cur)
  ON CONFLICT (id) DO UPDATE SET currency = EXCLUDED.currency;
END $function$

